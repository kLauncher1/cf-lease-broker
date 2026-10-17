package steering

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

var (
	ErrPacketTooShort     = errors.New("quic packet too short")
	ErrInvalidFixedBit    = errors.New("quic fixed bit not set")
	ErrTruncatedDCID      = errors.New("quic packet truncated at destination connection id")
	ErrTruncatedSCID      = errors.New("quic packet truncated at source connection id")
	ErrInvalidVarint      = errors.New("quic varint exceeds buffer bounds")
)

// ParseQUICHeader inspects raw UDP packet bytes and extracts RFC 9000 QUIC header fields.
// It executes in sub-microsecond time with minimal heap overhead.
func ParseQUICHeader(payload []byte, defaultShortDCIDLen int) (*QUICPacketHeader, error) {
	if len(payload) < 1 {
		return nil, ErrPacketTooShort
	}

	firstByte := payload[0]
	isLongHeader := (firstByte & 0x80) != 0

	// RFC 9000 Section 17: The fixed bit (0x40) MUST be set to 1 in all QUIC packets.
	// For version negotiation packets (where version == 0), the fixed bit can be arbitrary.
	// We allow graceful parsing if fixed bit is missing, but flag it if version is standard.
	fixedBitSet := (firstByte & 0x40) != 0

	header := &QUICPacketHeader{
		IsLongHeader:  isLongHeader,
		PayloadLength: len(payload),
	}

	if isLongHeader {
		if len(payload) < 7 {
			return nil, ErrPacketTooShort
		}

		version := binary.BigEndian.Uint32(payload[1:5])
		header.Version = version

		if version != 0 && !fixedBitSet {
			return nil, ErrInvalidFixedBit
		}

		// Packet Type for Long Header (bits 4 and 5)
		longTypeBits := (firstByte & 0x30) >> 4
		switch longTypeBits {
		case 0x00:
			header.Type = QUICHeaderInitial
		case 0x01:
			header.Type = QUICHeader0RTT
		case 0x02:
			header.Type = QUICHeaderHandshake
		case 0x03:
			header.Type = QUICHeaderRetry
		default:
			header.Type = QUICHeaderUnknown
		}

		dcidLen := int(payload[5])
		if dcidLen > 20 { // RFC 9000 specifies connection IDs are at most 20 bytes
			dcidLen = 20
		}
		offset := 6
		if len(payload) < offset+dcidLen+1 {
			return nil, ErrTruncatedDCID
		}

		header.DCID = hex.EncodeToString(payload[offset : offset+dcidLen])
		offset += dcidLen

		scidLen := int(payload[offset])
		if scidLen > 20 {
			scidLen = 20
		}
		offset++
		if len(payload) < offset+scidLen {
			return nil, ErrTruncatedSCID
		}

		header.SCID = hex.EncodeToString(payload[offset : offset+scidLen])
		offset += scidLen

		// If Initial packet, extract token length if space permits
		if header.Type == QUICHeaderInitial && offset < len(payload) {
			tokenLen, nBytes, err := readVarint(payload[offset:])
			if err == nil && nBytes > 0 {
				header.TokenLength = int(tokenLen)
			}
		}

		return header, nil
	}

	// Short Header (1-RTT Data)
	if !fixedBitSet {
		return nil, ErrInvalidFixedBit
	}

	header.Type = QUICHeaderShort1RTT
	header.SpinBit = (firstByte & 0x20) != 0
	header.KeyPhase = (firstByte & 0x04) != 0

	pnLen := int(firstByte&0x03) + 1
	header.PacketNumber = uint64(pnLen)

	dcidLen := defaultShortDCIDLen
	if dcidLen <= 0 {
		dcidLen = 8
	}
	if dcidLen > 20 {
		dcidLen = 20
	}

	if len(payload) < 1+dcidLen {
		// If payload is shorter than expected DCID, consume remaining bytes
		dcidLen = len(payload) - 1
		if dcidLen < 0 {
			dcidLen = 0
		}
	}

	if dcidLen > 0 {
		header.DCID = hex.EncodeToString(payload[1 : 1+dcidLen])
	}

	return header, nil
}

// readVarint decodes an RFC 9000 variable-length integer.
func readVarint(b []byte) (val uint64, bytesRead int, err error) {
	if len(b) < 1 {
		return 0, 0, ErrInvalidVarint
	}

	tag := b[0] >> 6
	switch tag {
	case 0: // 1-byte length (6 bits value)
		return uint64(b[0] & 0x3f), 1, nil
	case 1: // 2-byte length (14 bits value)
		if len(b) < 2 {
			return 0, 0, ErrInvalidVarint
		}
		return uint64(binary.BigEndian.Uint16(b[0:2]) & 0x3fff), 2, nil
	case 2: // 4-byte length (30 bits value)
		if len(b) < 4 {
			return 0, 0, ErrInvalidVarint
		}
		return uint64(binary.BigEndian.Uint32(b[0:4]) & 0x3fffffff), 4, nil
	case 3: // 8-byte length (62 bits value)
		if len(b) < 8 {
			return 0, 0, ErrInvalidVarint
		}
		return binary.BigEndian.Uint64(b[0:8]) & 0x3fffffffffffffff, 8, nil
	default:
		return 0, 0, fmt.Errorf("unexpected varint tag %d", tag)
	}
}

// BuildSyntheticQUICPacket creates a valid RFC 9000 QUIC packet buffer for testing and benchmarks.
func BuildSyntheticQUICPacket(headerType QUICHeaderType, dcidHex, scidHex string, payloadBytes int) []byte {
	dcid, _ := hex.DecodeString(dcidHex)
	scid, _ := hex.DecodeString(scidHex)

	if headerType == QUICHeaderShort1RTT {
		buf := make([]byte, 1+len(dcid)+payloadBytes)
		buf[0] = 0x40 | 0x01 // Fixed bit set, 2-byte packet number
		copy(buf[1:], dcid)
		for i := 1 + len(dcid); i < len(buf); i++ {
			buf[i] = byte(i % 256)
		}
		return buf
	}

	// Long Header packet
	var firstByte byte = 0x80 | 0x40 // Long header + Fixed bit
	switch headerType {
	case QUICHeaderInitial:
		firstByte |= 0x00
	case QUICHeader0RTT:
		firstByte |= 0x10
	case QUICHeaderHandshake:
		firstByte |= 0x20
	case QUICHeaderRetry:
		firstByte |= 0x30
	default:
		firstByte |= 0x00
	}

	buf := make([]byte, 6+len(dcid)+1+len(scid)+1+payloadBytes)
	buf[0] = firstByte
	binary.BigEndian.PutUint32(buf[1:5], 0x00000001) // Version 1 (RFC 9000)
	buf[5] = byte(len(dcid))
	offset := 6
	copy(buf[offset:], dcid)
	offset += len(dcid)

	buf[offset] = byte(len(scid))
	offset++
	copy(buf[offset:], scid)
	offset += len(scid)

	// Token length 0 for simple synthetic packet
	buf[offset] = 0x00
	offset++

	for i := offset; i < len(buf); i++ {
		buf[i] = byte(i % 256)
	}
	return buf
}
