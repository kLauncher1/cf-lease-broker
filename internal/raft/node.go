package raft

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// RaftNode represents an active instance of the Raft consensus engine.
type RaftNode struct {
	mu              sync.RWMutex
	config          NodeConfig
	role            NodeRole
	currentTerm     uint64
	votedFor        string
	leaderID        string
	journal         *Journal
	discovery       *PeerDiscovery
	fsm             FSM
	httpClient      *http.Client
	resetElectionCh chan struct{}
	stopCh          chan struct{}
	fsmAppliedCount int64
	startTime       time.Time
}

// NewRaftNode creates and initializes a Raft node with WAL journal, peer discovery, and concrete FSM.
func NewRaftNode(cfg NodeConfig, journal *Journal, discovery *PeerDiscovery, fsm FSM) *RaftNode {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 50 * time.Millisecond
	}
	if cfg.ElectionTimeoutMin <= 0 {
		cfg.ElectionTimeoutMin = 150 * time.Millisecond
	}
	if cfg.ElectionTimeoutMax <= 0 {
		cfg.ElectionTimeoutMax = 300 * time.Millisecond
	}

	rn := &RaftNode{
		config:          cfg,
		role:            RoleFollower,
		currentTerm:     0,
		votedFor:        "",
		leaderID:        "",
		journal:         journal,
		discovery:       discovery,
		fsm:             fsm,
		httpClient:      &http.Client{Timeout: 150 * time.Millisecond},
		resetElectionCh: make(chan struct{}, 1),
		stopCh:          make(chan struct{}),
		startTime:       time.Now().UTC(),
	}

	return rn
}

// Start boots the background consensus election timers, heartbeats, and FSM commit application loops.
func (rn *RaftNode) Start() {
	go rn.runElectionLoop()
	go rn.runApplyLoop()
}

// Stop gracefully shuts down the consensus engine and releases resources.
func (rn *RaftNode) Stop() {
	select {
	case <-rn.stopCh:
		return
	default:
		close(rn.stopCh)
	}
	_ = rn.journal.Close()
}

func (rn *RaftNode) runElectionLoop() {
	for {
		timeout := rn.randomElectionTimeout()
		timer := time.NewTimer(timeout)

		select {
		case <-rn.stopCh:
			timer.Stop()
			return
		case <-rn.resetElectionCh:
			timer.Stop()
			continue
		case <-timer.C:
			rn.mu.Lock()
			if rn.role != RoleLeader {
				rn.startElectionLocked()
			}
			rn.mu.Unlock()
		}
	}
}

func (rn *RaftNode) randomElectionTimeout() time.Duration {
	min := rn.config.ElectionTimeoutMin
	max := rn.config.ElectionTimeoutMax
	diff := max - min
	if diff <= 0 {
		return min
	}

	var n uint32
	_ = binary.Read(rand.Reader, binary.LittleEndian, &n)
	randDuration := time.Duration(n % uint32(diff))
	return min + randDuration
}

func (rn *RaftNode) resetElectionTimer() {
	select {
	case rn.resetElectionCh <- struct{}{}:
	default:
	}
}

func (rn *RaftNode) startElectionLocked() {
	rn.role = RoleCandidate
	rn.currentTerm++
	rn.votedFor = rn.config.NodeID
	rn.leaderID = ""

	term := rn.currentTerm
	candidateID := rn.config.NodeID
	lastLogIndex := rn.journal.LastIndex()
	lastLogTerm := rn.journal.LastTerm()

	peers := rn.discovery.DiscoverPeers()
	totalNodes := len(peers) + 1
	neededVotes := (totalNodes / 2) + 1

	var votesReceived int32 = 1 // Self vote

	// If single node cluster, become leader immediately
	if neededVotes == 1 {
		rn.becomeLeaderLocked()
		return
	}

	args := RequestVoteArgs{
		Term:         term,
		CandidateID:  candidateID,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}

	go func() {
		var wg sync.WaitGroup
		for peerID, peerAddr := range peers {
			wg.Add(1)
			go func(pID, pAddr string) {
				defer wg.Done()
				start := time.Now()
				reply, err := rn.sendRequestVote(pAddr, args)
				latency := time.Since(start)
				rn.discovery.RecordHeartbeat(pID, latency, err == nil)

				if err != nil {
					return
				}

				rn.mu.Lock()
				defer rn.mu.Unlock()

				if reply.Term > rn.currentTerm {
					rn.currentTerm = reply.Term
					rn.role = RoleFollower
					rn.votedFor = ""
					rn.leaderID = ""
					rn.resetElectionTimer()
					return
				}

				if rn.role == RoleCandidate && reply.Term == rn.currentTerm && reply.VoteGranted {
					got := atomic.AddInt32(&votesReceived, 1)
					if int(got) >= neededVotes {
						rn.becomeLeaderLocked()
					}
				}
			}(peerID, peerAddr)
		}
		wg.Wait()
	}()
}

func (rn *RaftNode) becomeLeaderLocked() {
	if rn.role == RoleLeader {
		return
	}
	rn.role = RoleLeader
	rn.leaderID = rn.config.NodeID

	peers := rn.discovery.DiscoverPeers()
	lastIdx := rn.journal.LastIndex()
	for pID := range peers {
		rn.discovery.UpdateIndices(pID, lastIdx+1, 0)
	}

	go rn.runLeaderHeartbeats()
}

func (rn *RaftNode) runLeaderHeartbeats() {
	ticker := time.NewTicker(rn.config.HeartbeatInterval)
	defer ticker.Stop()

	// Initial heartbeat immediately
	rn.replicateLog()

	for {
		select {
		case <-rn.stopCh:
			return
		case <-ticker.C:
			rn.mu.RLock()
			isLeader := rn.role == RoleLeader
			rn.mu.RUnlock()

			if !isLeader {
				return
			}
			rn.replicateLog()
		}
	}
}

func (rn *RaftNode) replicateLog() {
	rn.mu.RLock()
	if rn.role != RoleLeader {
		rn.mu.RUnlock()
		return
	}
	term := rn.currentTerm
	leaderID := rn.config.NodeID
	commitIndex := rn.journal.CommitIndex()
	peers := rn.discovery.DiscoverPeers()
	peerStatuses := rn.discovery.GetPeerStatusList()
	rn.mu.RUnlock()

	totalNodes := len(peers) + 1
	majority := (totalNodes / 2) + 1

	if len(peers) == 0 {
		rn.mu.Lock()
		rn.checkAndUpdateCommitIndexLocked(1)
		rn.mu.Unlock()
		return
	}

	for _, p := range peerStatuses {
		pAddr, exists := peers[p.ID]
		if !exists {
			continue
		}

		go func(peerID, peerAddr string, nextIdx uint64) {
			prevLogIndex := nextIdx - 1
			prevLogTerm := uint64(0)
			if prevEntry, err := rn.journal.GetEntry(prevLogIndex); err == nil {
				prevLogTerm = prevEntry.Term
			}
			entries := rn.journal.GetEntriesFrom(nextIdx)

			args := AppendEntriesArgs{
				Term:         term,
				LeaderID:     leaderID,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				Entries:      entries,
				LeaderCommit: commitIndex,
			}

			start := time.Now()
			reply, err := rn.sendAppendEntries(peerAddr, args)
			latency := time.Since(start)
			rn.discovery.RecordHeartbeat(peerID, latency, err == nil)

			if err != nil {
				return
			}

			rn.mu.Lock()
			defer rn.mu.Unlock()

			if reply.Term > rn.currentTerm {
				rn.currentTerm = reply.Term
				rn.role = RoleFollower
				rn.votedFor = ""
				rn.leaderID = ""
				rn.resetElectionTimer()
				return
			}

			if rn.role != RoleLeader || reply.Term != rn.currentTerm {
				return
			}

			if reply.Success {
				newNext := reply.MatchIndex + 1
				rn.discovery.UpdateIndices(peerID, newNext, reply.MatchIndex)

				// Calculate commitIndex progression based on quorum match index
				rn.checkAndUpdateCommitIndexLocked(majority)
			} else {
				if nextIdx > 1 {
					rn.discovery.UpdateIndices(peerID, nextIdx-1, 0)
				}
			}
		}(p.ID, pAddr, p.NextIndex)
	}
}

func (rn *RaftNode) checkAndUpdateCommitIndexLocked(majority int) {
	lastIdx := rn.journal.LastIndex()
	for n := lastIdx; n > rn.journal.CommitIndex(); n-- {
		entry, err := rn.journal.GetEntry(n)
		if err != nil || entry.Term != rn.currentTerm {
			continue
		}

		matchCount := 1 // Self
		for _, p := range rn.discovery.GetPeerStatusList() {
			if p.MatchIndex >= n {
				matchCount++
			}
		}

		if matchCount >= majority {
			rn.journal.SetCommitIndex(n)
			break
		}
	}
}

// RequestVote handles incoming vote requests from candidate peers.
func (rn *RaftNode) RequestVote(args RequestVoteArgs) RequestVoteReply {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	if args.Term > rn.currentTerm {
		rn.currentTerm = args.Term
		rn.role = RoleFollower
		rn.votedFor = ""
		rn.leaderID = ""
	}

	reply := RequestVoteReply{
		Term:        rn.currentTerm,
		VoteGranted: false,
	}

	if args.Term < rn.currentTerm {
		return reply
	}

	canVote := rn.votedFor == "" || rn.votedFor == args.CandidateID
	lastLogIndex := rn.journal.LastIndex()
	lastLogTerm := rn.journal.LastTerm()

	isUpToDate := args.LastLogTerm > lastLogTerm ||
		(args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex)

	if canVote && isUpToDate {
		rn.votedFor = args.CandidateID
		rn.resetElectionTimer()
		reply.VoteGranted = true
	}

	return reply
}

// AppendEntries handles heartbeat and log replication RPCs from leader.
func (rn *RaftNode) AppendEntries(args AppendEntriesArgs) AppendEntriesReply {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	if args.Term > rn.currentTerm {
		rn.currentTerm = args.Term
		rn.role = RoleFollower
		rn.votedFor = ""
	}

	reply := AppendEntriesReply{
		Term:    rn.currentTerm,
		Success: false,
	}

	if args.Term < rn.currentTerm {
		return reply
	}

	rn.role = RoleFollower
	rn.leaderID = args.LeaderID
	rn.resetElectionTimer()

	success, matchIdx := rn.journal.AppendEntries(args.PrevLogIndex, args.PrevLogTerm, args.Entries)
	reply.Success = success
	reply.MatchIndex = matchIdx

	if success && args.LeaderCommit > rn.journal.CommitIndex() {
		newCommit := args.LeaderCommit
		if matchIdx < newCommit {
			newCommit = matchIdx
		}
		rn.journal.SetCommitIndex(newCommit)
	}

	return reply
}

// Propose submits a new command to the replicated state machine.
func (rn *RaftNode) Propose(cmdType string, data []byte) (uint64, error) {
	rn.mu.Lock()
	if rn.role != RoleLeader {
		leader := rn.leaderID
		rn.mu.Unlock()
		return 0, fmt.Errorf("node is not cluster leader (current leader: %s)", leader)
	}

	entry, err := rn.journal.Append(rn.currentTerm, cmdType, data)
	if err != nil {
		rn.mu.Unlock()
		return 0, err
	}
	rn.mu.Unlock()

	// Trigger replication immediately
	rn.replicateLog()

	// Wait for commit up to timeout
	idx := entry.Index
	deadline := time.Now().Add(1200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rn.journal.CommitIndex() >= idx {
			return idx, nil
		}
		time.Sleep(10 * time.Millisecond)
	}

	if rn.journal.CommitIndex() >= idx {
		return idx, nil
	}
	return idx, fmt.Errorf("proposal timed out waiting for cluster quorum commit (index: %d)", idx)
}

func (rn *RaftNode) runApplyLoop() {
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-rn.stopCh:
			return
		case <-ticker.C:
			commit := rn.journal.CommitIndex()
			lastApplied := rn.journal.LastApplied()

			for idx := lastApplied + 1; idx <= commit; idx++ {
				entry, err := rn.journal.GetEntry(idx)
				if err != nil {
					break
				}
				if rn.fsm != nil {
					_, _ = rn.fsm.Apply(entry)
				}
				rn.journal.SetLastApplied(idx)
				atomic.AddInt64(&rn.fsmAppliedCount, 1)
			}
		}
	}
}

// Status returns the current cluster observability telemetry.
func (rn *RaftNode) Status() ClusterStatus {
	rn.mu.RLock()
	defer rn.mu.RUnlock()

	return ClusterStatus{
		NodeID:          rn.config.NodeID,
		Role:            rn.role,
		CurrentTerm:     rn.currentTerm,
		LeaderID:        rn.leaderID,
		CommitIndex:     rn.journal.CommitIndex(),
		LastApplied:     rn.journal.LastApplied(),
		LogLength:       len(rn.journal.AllEntries()),
		Peers:           rn.discovery.GetPeerStatusList(),
		FSMAppliedCount: atomic.LoadInt64(&rn.fsmAppliedCount),
		UptimeSeconds:   time.Since(rn.startTime).Seconds(),
	}
}

func (rn *RaftNode) sendRequestVote(peerAddr string, args RequestVoteArgs) (RequestVoteReply, error) {
	url := fmt.Sprintf("%s/raft/vote", peerAddr)
	body, err := json.Marshal(args)
	if err != nil {
		return RequestVoteReply{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return RequestVoteReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := rn.httpClient.Do(req)
	if err != nil {
		return RequestVoteReply{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return RequestVoteReply{}, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var reply RequestVoteReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&reply); err != nil {
		return RequestVoteReply{}, err
	}
	return reply, nil
}

func (rn *RaftNode) sendAppendEntries(peerAddr string, args AppendEntriesArgs) (AppendEntriesReply, error) {
	url := fmt.Sprintf("%s/raft/append", peerAddr)
	body, err := json.Marshal(args)
	if err != nil {
		return AppendEntriesReply{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return AppendEntriesReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := rn.httpClient.Do(req)
	if err != nil {
		return AppendEntriesReply{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return AppendEntriesReply{}, errors.New(string(b))
	}

	var reply AppendEntriesReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8*1024*1024)).Decode(&reply); err != nil {
		return AppendEntriesReply{}, err
	}
	return reply, nil
}
