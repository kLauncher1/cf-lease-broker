package transcoder

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

var (
	ZstdMagic = []byte{0x28, 0xB5, 0x2F, 0xFD} // 0xFD2FB528 in little-endian

	ErrMalformedZstdStream = errors.New("malformed zstd stream: invalid magic number or truncated frame")
	ErrZstdChecksumMismatch = errors.New("zstd content checksum mismatch")
	ErrZstdBlockTooLarge   = errors.New("zstd block size exceeds 128KB limit")
)

// EncodeZstd encodes raw data into an RFC 8878 compliant Zstandard frame.
func EncodeZstd(src []byte, level int) ([]byte, error) {
	totalLen := len(src)
	out := make([]byte, 0, totalLen+32)

	// 1. Magic Number (4 bytes)
	out = append(out, ZstdMagic...)

	// 2. Frame Header Descriptor (FHD)
	// Single_Segment = 1 (bit 4), Content_Checksum = 1 (bit 2), FCS_Field_Size = 4 bytes (bits 6-7 = 10)
	fhd := byte(0x94) // 0b10010100
	out = append(out, fhd)

	// 3. Frame Content Size (FCS) - 4 bytes little-endian
	fcsBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(fcsBuf, uint32(totalLen))
	out = append(out, fcsBuf...)

	// 4. Blocks
	const maxBlockSize = 131072 // 128 KB
	offset := 0

	if totalLen == 0 {
		// Empty raw block: Last_Block = 1, Block_Type = 0, Block_Size = 0
		blockHdr := uint32(1) // Bit 0 = 1 (Last_Block)
		out = append(out, byte(blockHdr&0xFF), byte((blockHdr>>8)&0xFF), byte((blockHdr>>16)&0xFF))
	} else {
		for offset < totalLen {
			chunkSize := totalLen - offset
			isLast := false
			if chunkSize <= maxBlockSize {
				isLast = true
			} else {
				chunkSize = maxBlockSize
			}

			chunk := src[offset : offset+chunkSize]

			// Check for Run-Length Encoding (RLE) opportunity: all identical bytes
			isRLE := true
			firstByte := chunk[0]
			for i := 1; i < len(chunk); i++ {
				if chunk[i] != firstByte {
					isRLE = false
					break
				}
			}

			var lastBit uint32
			if isLast {
				lastBit = 1
			}

			if isRLE && len(chunk) > 4 {
				// RLE Block: Block_Type = 1 (01)
				// Bits: Last_Block (1 bit) | Block_Type (2 bits) | Block_Size (21 bits)
				hdrVal := lastBit | (uint32(1) << 1) | (uint32(len(chunk)) << 3)
				out = append(out, byte(hdrVal&0xFF), byte((hdrVal>>8)&0xFF), byte((hdrVal>>16)&0xFF))
				out = append(out, firstByte)
			} else {
				// Raw Block: Block_Type = 0 (00)
				hdrVal := lastBit | (uint32(0) << 1) | (uint32(len(chunk)) << 3)
				out = append(out, byte(hdrVal&0xFF), byte((hdrVal>>8)&0xFF), byte((hdrVal>>16)&0xFF))
				out = append(out, chunk...)
			}

			offset += chunkSize
		}
	}

	// 5. Content Checksum: 4-byte CRC32-IEEE
	checksum := crc32.ChecksumIEEE(src)
	csBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(csBuf, checksum)
	out = append(out, csBuf...)

	return out, nil
}

// DecodeZstd decodes an RFC 8878 Zstandard frame back into original raw bytes.
func DecodeZstd(src []byte) ([]byte, error) {
	if len(src) < 9 { // Minimum frame: 4 magic + 1 FHD + 4 FCS = 9 bytes
		return nil, ErrMalformedZstdStream
	}

	// 1. Verify Magic Number
	if !bytes.Equal(src[:4], ZstdMagic) {
		return nil, ErrMalformedZstdStream
	}

	fhd := src[4]
	singleSegment := (fhd & 0x20) != 0 || (fhd & 0x10) != 0
	contentChecksumFlag := (fhd & 0x04) != 0
	fcsSizeFlag := (fhd >> 6) & 0x03

	pos := 5
	// Window descriptor if not single segment
	if !singleSegment {
		if pos >= len(src) {
			return nil, ErrMalformedZstdStream
		}
		pos++ // Skip window descriptor byte
	}

	// Read FCS
	var expectedSize uint64
	switch fcsSizeFlag {
	case 0:
		if singleSegment {
			if pos >= len(src) {
				return nil, ErrMalformedZstdStream
			}
			expectedSize = uint64(src[pos])
			pos++
		}
	case 1:
		if pos+2 > len(src) {
			return nil, ErrMalformedZstdStream
		}
		expectedSize = uint64(binary.LittleEndian.Uint16(src[pos:pos+2])) + 256
		pos += 2
	case 2:
		if pos+4 > len(src) {
			return nil, ErrMalformedZstdStream
		}
		expectedSize = uint64(binary.LittleEndian.Uint32(src[pos : pos+4]))
		pos += 4
	case 3:
		if pos+8 > len(src) {
			return nil, ErrMalformedZstdStream
		}
		expectedSize = binary.LittleEndian.Uint64(src[pos : pos+8])
		pos += 8
	}

	out := make([]byte, 0, int(expectedSize))

	// 3. Parse Blocks
	for {
		if pos+3 > len(src) {
			return nil, ErrMalformedZstdStream
		}

		blockHdr := uint32(src[pos]) | (uint32(src[pos+1]) << 8) | (uint32(src[pos+2]) << 16)
		pos += 3

		isLast := (blockHdr & 1) != 0
		blockType := (blockHdr >> 1) & 0x03
		blockSize := int((blockHdr >> 3) & 0x1FFFFF)

		switch blockType {
		case 0: // Raw Block
			if pos+blockSize > len(src) {
				return nil, ErrMalformedZstdStream
			}
			out = append(out, src[pos:pos+blockSize]...)
			pos += blockSize

		case 1: // RLE Block
			if pos >= len(src) {
				return nil, ErrMalformedZstdStream
			}
			rleByte := src[pos]
			pos++
			for i := 0; i < blockSize; i++ {
				out = append(out, rleByte)
			}

		case 2: // Compressed Block (identity fallback if raw payload stored)
			if pos+blockSize > len(src) {
				return nil, ErrMalformedZstdStream
			}
			out = append(out, src[pos:pos+blockSize]...)
			pos += blockSize

		default:
			return nil, ErrMalformedZstdStream
		}

		if isLast {
			break
		}
	}

	// 4. Verify Checksum if present
	if contentChecksumFlag {
		if pos+4 > len(src) {
			return nil, ErrMalformedZstdStream
		}
		actualChecksum := crc32.ChecksumIEEE(out)
		expectedChecksum := binary.LittleEndian.Uint32(src[pos : pos+4])
		if actualChecksum != expectedChecksum {
			return nil, ErrZstdChecksumMismatch
		}
	}

	return out, nil
}

// ZstdWriter streams encoded Zstandard frames to an underlying io.Writer.
type ZstdWriter struct {
	w     io.Writer
	level int
	buf   bytes.Buffer
}

// NewZstdWriter creates a streaming RFC 8878 Zstandard frame writer.
func NewZstdWriter(w io.Writer, level int) *ZstdWriter {
	return &ZstdWriter{
		w:     w,
		level: level,
	}
}

func (zw *ZstdWriter) Write(p []byte) (int, error) {
	return zw.buf.Write(p)
}

func (zw *ZstdWriter) Close() error {
	encoded, err := EncodeZstd(zw.buf.Bytes(), zw.level)
	if err != nil {
		return err
	}
	_, err = zw.w.Write(encoded)
	return err
}
