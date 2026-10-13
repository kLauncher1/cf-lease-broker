package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// DefaultChunkSize specifies the default stripe chunk size (2MB).
const DefaultChunkSize int64 = 2 * 1024 * 1024

// Chunk represents an individual striped block assigned to a Cloudflare tenant.
type Chunk struct {
	Index     int    `json:"index"`
	ID        string `json:"id"`
	Size      int64  `json:"size"`
	Checksum  string `json:"checksum"`
	AccountID string `json:"account_id"`
	Data      []byte `json:"-"`
}

// Chunker splits continuous byte streams into striped blocks across account pools.
type Chunker struct {
	ChunkSize int64
}

// NewChunker creates a new Chunker with the designated chunk boundary.
func NewChunker(chunkSize int64) *Chunker {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	return &Chunker{ChunkSize: chunkSize}
}

// Split divides an input stream into numbered chunks striped across available accounts.
func (c *Chunker) Split(reader io.Reader, accounts []string) ([]Chunk, int64, string, error) {
	if len(accounts) == 0 {
		return nil, 0, "", errors.New("cannot stripe chunks across empty account pool")
	}

	var chunks []Chunk
	var totalSize int64
	overallHasher := sha256.New()
	buf := make([]byte, c.ChunkSize)
	idx := 0

	for {
		n, err := io.ReadFull(reader, buf)
		if n > 0 {
			chunkData := make([]byte, n)
			copy(chunkData, buf[:n])

			chunkHasher := sha256.New()
			chunkHasher.Write(chunkData)
			overallHasher.Write(chunkData)
			chunkHashHex := hex.EncodeToString(chunkHasher.Sum(nil))

			assignedAccount := accounts[idx%len(accounts)]
			chunkID := fmt.Sprintf("chk_%s_%04d", chunkHashHex[:8], idx)

			chunks = append(chunks, Chunk{
				Index:     idx,
				ID:        chunkID,
				Size:      int64(n),
				Checksum:  chunkHashHex,
				AccountID: assignedAccount,
				Data:      chunkData,
			})

			totalSize += int64(n)
			idx++
		}

		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, 0, "", fmt.Errorf("read error during chunking: %w", err)
		}
	}

	if totalSize == 0 {
		// Handle empty file case
		etag := hex.EncodeToString(overallHasher.Sum(nil))
		return []Chunk{}, 0, etag, nil
	}

	etag := hex.EncodeToString(overallHasher.Sum(nil))
	if len(chunks) > 1 {
		etag = fmt.Sprintf("%s-%d", etag[:32], len(chunks))
	}
	return chunks, totalSize, etag, nil
}

// Reassemble combines striped chunks in sequence, verifying integrity checksums.
func (c *Chunker) Reassemble(chunks []Chunk) ([]byte, error) {
	if len(chunks) == 0 {
		return []byte{}, nil
	}

	var totalSize int64
	for _, chk := range chunks {
		totalSize += chk.Size
	}

	buf := make([]byte, 0, totalSize)
	for i, chk := range chunks {
		if chk.Index != i {
			return nil, fmt.Errorf("chunk sequence gap: expected index %d, got %d", i, chk.Index)
		}
		if len(chk.Data) != int(chk.Size) {
			return nil, fmt.Errorf("chunk %s data size mismatch: expected %d, got %d", chk.ID, chk.Size, len(chk.Data))
		}

		hasher := sha256.New()
		hasher.Write(chk.Data)
		sumHex := hex.EncodeToString(hasher.Sum(nil))
		if chk.Checksum != "" && sumHex != chk.Checksum {
			return nil, fmt.Errorf("chunk %s checksum validation failed: expected %s, got %s", chk.ID, chk.Checksum, sumHex)
		}

		buf = append(buf, chk.Data...)
	}

	return buf, nil
}

// ReadRange retrieves a sub-slice of bytes directly across striped chunk boundaries.
func (c *Chunker) ReadRange(chunks []Chunk, start int64, end int64) ([]byte, error) {
	if start < 0 || end < start {
		return nil, errors.New("invalid byte range boundaries")
	}

	var totalSize int64
	for _, chk := range chunks {
		totalSize += chk.Size
	}
	if start >= totalSize {
		return nil, errors.New("start offset exceeds total object size")
	}
	if end >= totalSize {
		end = totalSize - 1
	}

	expectedLen := end - start + 1
	result := make([]byte, 0, expectedLen)

	var currentOffset int64
	for _, chk := range chunks {
		chkStart := currentOffset
		chkEnd := currentOffset + chk.Size - 1

		// Check if this chunk overlaps [start, end]
		if chkEnd >= start && chkStart <= end {
			sliceStart := int64(0)
			if start > chkStart {
				sliceStart = start - chkStart
			}

			sliceEnd := chk.Size - 1
			if end < chkEnd {
				sliceEnd = end - chkStart
			}

			if sliceStart <= sliceEnd && sliceEnd < int64(len(chk.Data)) {
				result = append(result, chk.Data[sliceStart:sliceEnd+1]...)
			}
		}

		currentOffset += chk.Size
	}

	return result, nil
}
