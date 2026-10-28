package transcoder

import (
	"bytes"
	"errors"
	"io"
)

var (
	ErrMalformedBrotliStream = errors.New("malformed brotli stream: invalid header or truncated meta-block")
	ErrBrotliPayloadTooLarge = errors.New("brotli meta-block exceeds maximum permitted window size")
)

// BitWriter allows bit-level packing into a byte buffer (little-endian bit ordering).
type bitWriter struct {
	buf   []byte
	accum uint32
	bits  uint
}

func newBitWriter(estimatedBytes int) *bitWriter {
	return &bitWriter{
		buf: make([]byte, 0, estimatedBytes),
	}
}

func (bw *bitWriter) writeBits(n uint, val uint32) {
	bw.accum |= (val & ((1 << n) - 1)) << bw.bits
	bw.bits += n
	for bw.bits >= 8 {
		bw.buf = append(bw.buf, byte(bw.accum&0xFF))
		bw.accum >>= 8
		bw.bits -= 8
	}
}

func (bw *bitWriter) flush() []byte {
	if bw.bits > 0 {
		bw.buf = append(bw.buf, byte(bw.accum&0xFF))
		bw.accum = 0
		bw.bits = 0
	}
	return bw.buf
}

// BitReader allows sequential bit reading from a byte slice.
type bitReader struct {
	src   []byte
	pos   int
	accum uint32
	bits  uint
}

func newBitReader(src []byte) *bitReader {
	return &bitReader{src: src}
}

func (br *bitReader) readBit() (uint32, error) {
	for br.bits < 1 {
		if br.pos >= len(br.src) {
			return 0, io.EOF
		}
		br.accum |= uint32(br.src[br.pos]) << br.bits
		br.pos++
		br.bits += 8
	}
	bit := br.accum & 1
	br.accum >>= 1
	br.bits--
	return bit, nil
}

func (br *bitReader) readBits(n uint) (uint32, error) {
	for br.bits < n {
		if br.pos >= len(br.src) {
			return 0, io.EOF
		}
		br.accum |= uint32(br.src[br.pos]) << br.bits
		br.pos++
		br.bits += 8
	}
	val := br.accum & ((1 << n) - 1)
	br.accum >>= n
	br.bits -= n
	return val, nil
}

func (br *bitReader) alignToByte() {
	rem := br.bits % 8
	if rem > 0 {
		br.accum >>= rem
		br.bits -= rem
	}
}

// EncodeBrotli encodes raw payload into RFC 7932 Brotli frame format.
func EncodeBrotli(src []byte, level int) ([]byte, error) {
	if len(src) == 0 {
		// Empty stream: WBITS=16 (bit 0=0), ISLAST=1 (bit 1=1), ISLASTEMPTY=1 (bit 2=1)
		bw := newBitWriter(1)
		bw.writeBits(1, 0) // WBITS = 16
		bw.writeBits(1, 1) // ISLAST = 1
		bw.writeBits(1, 1) // ISLASTEMPTY = 1
		return bw.flush(), nil
	}

	bw := newBitWriter(len(src) + 64)
	// RFC 7932 Section 9.1: Stream Header (WBITS = 16 -> bit 0 = 0)
	bw.writeBits(1, 0)

	const maxChunk = 65536
	offset := 0
	total := len(src)

	for offset < total {
		chunkSize := total - offset
		isLast := false
		if chunkSize <= maxChunk {
			isLast = true
		} else {
			chunkSize = maxChunk
		}

		if isLast {
			bw.writeBits(1, 1) // ISLAST = 1
			bw.writeBits(1, 0) // ISLASTEMPTY = 0
		} else {
			bw.writeBits(1, 0) // ISLAST = 0
		}

		// MNIBBLES: 00 -> 4 nibbles (16 bits)
		bw.writeBits(2, 0)
		// MLEN - 1 (16 bits)
		mlenMinusOne := uint32(chunkSize - 1)
		bw.writeBits(16, mlenMinusOne)
		// ISUNCOMPRESSED = 1
		bw.writeBits(1, 1)

		// Byte alignment
		data := bw.flush()

		// Append literal payload chunk
		data = append(data, src[offset:offset+chunkSize]...)
		bw.buf = data
		offset += chunkSize
	}

	return bw.flush(), nil
}

// DecodeBrotli decodes an RFC 7932 Brotli frame back to uncompressed bytes.
func DecodeBrotli(src []byte) ([]byte, error) {
	if len(src) == 0 {
		return []byte{}, nil
	}

	br := newBitReader(src)

	// Read WBITS bit (0 -> WBITS 16)
	wbit, err := br.readBit()
	if err != nil {
		return nil, ErrMalformedBrotliStream
	}
	if wbit != 0 {
		return nil, ErrMalformedBrotliStream
	}

	var out []byte
	for {
		isLast, err := br.readBit()
		if err != nil {
			return nil, ErrMalformedBrotliStream
		}

		if isLast == 1 {
			isEmpty, err := br.readBit()
			if err != nil {
				return nil, ErrMalformedBrotliStream
			}
			if isEmpty == 1 {
				// Empty final meta-block
				break
			}
		}

		mnibbles, err := br.readBits(2)
		if err != nil {
			return nil, ErrMalformedBrotliStream
		}

		var mlen uint32
		if mnibbles == 0 {
			// 4 nibbles = 16 bits
			val, err := br.readBits(16)
			if err != nil {
				return nil, ErrMalformedBrotliStream
			}
			mlen = val + 1
		} else {
			// Unsupported extended nibble in uncompressed meta-block
			return nil, ErrMalformedBrotliStream
		}

		isUncompressed, err := br.readBit()
		if err != nil || isUncompressed != 1 {
			return nil, ErrMalformedBrotliStream
		}

		// Align to byte boundary
		br.alignToByte()

		// Read mlen uncompressed literal bytes directly
		needed := int(mlen)
		remainingBits := br.bits / 8
		chunk := make([]byte, 0, needed)

		for remainingBits > 0 && needed > 0 {
			b, err := br.readBits(8)
			if err != nil {
				return nil, ErrMalformedBrotliStream
			}
			chunk = append(chunk, byte(b))
			needed--
			remainingBits--
		}

		if needed > 0 {
			if br.pos+needed > len(br.src) {
				return nil, ErrMalformedBrotliStream
			}
			chunk = append(chunk, br.src[br.pos:br.pos+needed]...)
			br.pos += needed
		}

		out = append(out, chunk...)

		if isLast == 1 {
			break
		}
	}

	return out, nil
}

// BrotliWriter streams encoded Brotli frames to an underlying io.Writer.
type BrotliWriter struct {
	w       io.Writer
	level   int
	buf     bytes.Buffer
	written bool
}

// NewBrotliWriter creates a streaming RFC 7932 Brotli frame writer.
func NewBrotliWriter(w io.Writer, level int) *BrotliWriter {
	return &BrotliWriter{
		w:     w,
		level: level,
	}
}

func (bw *BrotliWriter) Write(p []byte) (int, error) {
	return bw.buf.Write(p)
}

func (bw *BrotliWriter) Close() error {
	encoded, err := EncodeBrotli(bw.buf.Bytes(), bw.level)
	if err != nil {
		return err
	}
	_, err = bw.w.Write(encoded)
	return err
}
