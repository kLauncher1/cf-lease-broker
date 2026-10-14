package raft

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Journal manages Write-Ahead Log (WAL) transaction entries with monotonic indexing and SHA-256 verification.
type Journal struct {
	mu          sync.RWMutex
	entries     []LogEntry
	commitIndex uint64
	lastApplied uint64
	dataDir     string
	journalFile *os.File
}

// NewJournal initializes a journal, restoring from disk if dataDir is provided.
func NewJournal(dataDir string) (*Journal, error) {
	j := &Journal{
		entries: []LogEntry{
			{
				Term:      0,
				Index:     0,
				Type:      CmdNoop,
				Timestamp: time.Unix(0, 0).UTC(),
				Checksum:  "",
			},
		},
		dataDir: dataDir,
	}

	if dataDir != "" {
		cleanDir := filepath.Clean(dataDir)
		for _, r := range cleanDir {
			if r == 0 {
				return nil, errors.New("invalid raft data directory: contains null byte")
			}
		}
		j.dataDir = cleanDir
		if err := os.MkdirAll(cleanDir, 0750); err != nil {
			return nil, fmt.Errorf("failed to create raft data directory: %w", err)
		}
		filePath := filepath.Join(cleanDir, "raft_journal.wal")
		if err := j.loadFromDisk(filePath); err != nil {
			return nil, fmt.Errorf("failed to load existing journal: %w", err)
		}

		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed to open journal file for append: %w", err)
		}
		j.journalFile = f
	}

	return j, nil
}

// loadFromDisk parses JSON-serialized entries from disk and verifies SHA-256 checksums.
func (j *Journal) loadFromDisk(filePath string) error {
	f, err := os.Open(filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Expand scanner buffer to support large replicated log payloads up to 16 MB
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return fmt.Errorf("corrupt journal line: %w", err)
		}
		expectedChecksum := entry.ComputeChecksum()
		if entry.Checksum != "" && entry.Checksum != expectedChecksum {
			return fmt.Errorf("journal checksum mismatch at index %d", entry.Index)
		}
		j.entries = append(j.entries, entry)
	}

	return scanner.Err()
}

// Append appends a new state entry to the log with a monotonic index and SHA-256 integrity checksum.
func (j *Journal) Append(term uint64, entryType string, data []byte) (LogEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	nextIndex := uint64(len(j.entries))
	entry := LogEntry{
		Term:      term,
		Index:     nextIndex,
		Type:      entryType,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}
	entry.Checksum = entry.ComputeChecksum()

	j.entries = append(j.entries, entry)

	if j.journalFile != nil {
		line, err := json.Marshal(entry)
		if err != nil {
			return LogEntry{}, err
		}
		line = append(line, '\n')
		if _, err := j.journalFile.Write(line); err != nil {
			return LogEntry{}, err
		}
		_ = j.journalFile.Sync()
	}

	return entry, nil
}

// AppendEntries validates previous log consistency, truncates conflicting entries, and appends new items.
func (j *Journal) AppendEntries(prevLogIndex uint64, prevLogTerm uint64, newEntries []LogEntry) (bool, uint64) {
	j.mu.Lock()
	defer j.mu.Unlock()

	lastIdx := uint64(len(j.entries) - 1)
	if prevLogIndex > lastIdx {
		return false, lastIdx
	}

	// Verify term of previous log entry
	if prevLogIndex > 0 && j.entries[prevLogIndex].Term != prevLogTerm {
		return false, lastIdx
	}

	// Truncate conflicts and append
	insertIdx := prevLogIndex + 1
	var truncated bool
	if insertIdx < uint64(len(j.entries)) {
		j.entries = j.entries[:insertIdx]
		truncated = true
	}

	for _, entry := range newEntries {
		if entry.Checksum == "" {
			entry.Checksum = entry.ComputeChecksum()
		}
		j.entries = append(j.entries, entry)
	}

	// If truncated and disk file is active, rewrite full journal to maintain disk sync
	if j.journalFile != nil {
		if truncated {
			_ = j.journalFile.Close()
			filePath := filepath.Join(j.dataDir, "raft_journal.wal")
			f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err == nil {
				j.journalFile = f
				for i := 1; i < len(j.entries); i++ {
					line, _ := json.Marshal(j.entries[i])
					_, _ = j.journalFile.Write(append(line, '\n'))
				}
				_ = j.journalFile.Sync()
			}
		} else if len(newEntries) > 0 {
			for _, entry := range newEntries {
				line, _ := json.Marshal(entry)
				_, _ = j.journalFile.Write(append(line, '\n'))
			}
			_ = j.journalFile.Sync()
		}
	}

	return true, uint64(len(j.entries) - 1)
}

// GetEntry returns a log entry by index.
func (j *Journal) GetEntry(index uint64) (LogEntry, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if index >= uint64(len(j.entries)) {
		return LogEntry{}, errors.New("log entry index out of bounds")
	}
	return j.entries[index], nil
}

// GetEntriesFrom returns all log entries from startIndex onwards.
func (j *Journal) GetEntriesFrom(startIndex uint64) []LogEntry {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if startIndex >= uint64(len(j.entries)) {
		return nil
	}
	out := make([]LogEntry, len(j.entries)-int(startIndex))
	copy(out, j.entries[startIndex:])
	return out
}

// LastIndex returns the highest log index recorded.
func (j *Journal) LastIndex() uint64 {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return uint64(len(j.entries) - 1)
}

// LastTerm returns the term of the latest entry.
func (j *Journal) LastTerm() uint64 {
	j.mu.RLock()
	defer j.mu.RUnlock()
	if len(j.entries) == 0 {
		return 0
	}
	return j.entries[len(j.entries)-1].Term
}

// CommitIndex returns current committed index.
func (j *Journal) CommitIndex() uint64 {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.commitIndex
}

// SetCommitIndex updates committed index.
func (j *Journal) SetCommitIndex(idx uint64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if idx > j.commitIndex {
		j.commitIndex = idx
	}
}

// LastApplied returns index of last entry applied to FSM.
func (j *Journal) LastApplied() uint64 {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.lastApplied
}

// SetLastApplied updates the last applied index.
func (j *Journal) SetLastApplied(idx uint64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if idx > j.lastApplied {
		j.lastApplied = idx
	}
}

// AllEntries returns a snapshot copy of all entries including dummy 0.
func (j *Journal) AllEntries() []LogEntry {
	j.mu.RLock()
	defer j.mu.RUnlock()
	res := make([]LogEntry, len(j.entries))
	copy(res, j.entries)
	return res
}

// Close closes any underlying file handles.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.journalFile != nil {
		err := j.journalFile.Close()
		j.journalFile = nil
		return err
	}
	return nil
}
