package bgp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// HeaderLength is the standard 19-byte BGP packet header length.
const HeaderLength = 19

// Standard 16-byte BGP synchronization marker (RFC 4271 Section 4.1).
var marker = [16]byte{
	0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
}

// PathAttribute represents a raw or structured BGP path attribute.
type PathAttribute struct {
	Flags uint8
	Type  uint8
	Value []byte
}

// EncodeHeader constructs a standard 19-byte BGP packet with marker and length.
func EncodeHeader(msgType uint8, payload []byte) []byte {
	totalLen := HeaderLength + len(payload)
	buf := make([]byte, totalLen)
	copy(buf[0:16], marker[:])
	binary.BigEndian.PutUint16(buf[16:18], uint16(totalLen))
	buf[18] = msgType
	if len(payload) > 0 {
		copy(buf[19:], payload)
	}
	return buf
}

// ParseHeader validates marker and extracts message type, length, and payload.
func ParseHeader(data []byte) (msgType uint8, length uint16, payload []byte, err error) {
	if len(data) < HeaderLength {
		return 0, 0, nil, ErrInvalidPacketLen
	}
	if !bytes.Equal(data[0:16], marker[:]) {
		return 0, 0, nil, ErrInvalidMarker
	}
	length = binary.BigEndian.Uint16(data[16:18])
	if int(length) < HeaderLength || int(length) > len(data) {
		return 0, 0, nil, ErrInvalidPacketLen
	}
	msgType = data[18]
	payload = data[HeaderLength:length]
	return msgType, length, payload, nil
}

// EncodeOpen builds an RFC 4271 OPEN message payload.
func EncodeOpen(myAS uint16, holdTime uint16, bgpID [4]byte, optParams []byte) []byte {
	buf := make([]byte, 10+len(optParams))
	buf[0] = 4 // BGP Version 4
	binary.BigEndian.PutUint16(buf[1:3], myAS)
	binary.BigEndian.PutUint16(buf[3:5], holdTime)
	copy(buf[5:9], bgpID[:])
	buf[9] = uint8(len(optParams))
	if len(optParams) > 0 {
		copy(buf[10:], optParams)
	}
	return EncodeHeader(TypeOpen, buf)
}

// ParseOpen unpacks an RFC 4271 OPEN message payload.
func ParseOpen(payload []byte) (version uint8, as uint16, holdTime uint16, bgpID [4]byte, optParams []byte, err error) {
	if len(payload) < 10 {
		return 0, 0, 0, [4]byte{}, nil, ErrInvalidPacketLen
	}
	version = payload[0]
	as = binary.BigEndian.Uint16(payload[1:3])
	holdTime = binary.BigEndian.Uint16(payload[3:5])
	copy(bgpID[:], payload[5:9])
	optLen := int(payload[9])
	if len(payload) < 10+optLen {
		return 0, 0, 0, [4]byte{}, nil, ErrInvalidPacketLen
	}
	if optLen > 0 {
		optParams = payload[10 : 10+optLen]
	}
	return version, as, holdTime, bgpID, optParams, nil
}

// EncodeKeepalive generates a standard 19-byte KEEPALIVE message.
func EncodeKeepalive() []byte {
	return EncodeHeader(TypeKeepalive, nil)
}

// EncodeNotification builds an RFC 4271 NOTIFICATION message.
func EncodeNotification(errorCode uint8, errorSubcode uint8, data []byte) []byte {
	payload := make([]byte, 2+len(data))
	payload[0] = errorCode
	payload[1] = errorSubcode
	if len(data) > 0 {
		copy(payload[2:], data)
	}
	return EncodeHeader(TypeNotification, payload)
}

// ParseNotification parses NOTIFICATION error code and subcode.
func ParseNotification(payload []byte) (code uint8, subcode uint8, data []byte, err error) {
	if len(payload) < 2 {
		return 0, 0, nil, ErrInvalidPacketLen
	}
	return payload[0], payload[1], payload[2:], nil
}

// EncodeCIDR converts a CIDR string (e.g. "198.51.100.0/24") to BGP prefix wire format.
func EncodeCIDR(cidr string) ([]byte, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, ErrInvalidCIDR
	}
	ones, _ := ipnet.Mask.Size()
	ipBytes := ipnet.IP.To4()
	if ipBytes == nil {
		ipBytes = ipnet.IP.To16()
	}
	byteLen := (ones + 7) / 8
	out := make([]byte, 1+byteLen)
	out[0] = uint8(ones)
	if byteLen > 0 && len(ipBytes) >= byteLen {
		copy(out[1:], ipBytes[:byteLen])
	}
	return out, nil
}

// DecodeCIDR decodes a BGP prefix wire format into CIDR string.
func DecodeCIDR(data []byte) (cidr string, bytesRead int, err error) {
	if len(data) < 1 {
		return "", 0, ErrInvalidPacketLen
	}
	prefixLen := int(data[0])
	byteLen := (prefixLen + 7) / 8
	if len(data) < 1+byteLen {
		return "", 0, ErrInvalidPacketLen
	}
	ipBytes := make([]byte, 4)
	if byteLen <= 4 {
		copy(ipBytes, data[1:1+byteLen])
		ip := net.IP(ipBytes)
		cidr = fmt.Sprintf("%s/%d", ip.String(), prefixLen)
	} else {
		// IPv6 fallback
		ip6Bytes := make([]byte, 16)
		copy(ip6Bytes, data[1:1+byteLen])
		ip := net.IP(ip6Bytes)
		cidr = fmt.Sprintf("%s/%d", ip.String(), prefixLen)
	}
	return cidr, 1 + byteLen, nil
}

// EncodePathAttribute serializes a BGP Path Attribute according to flags, type and value.
func EncodePathAttribute(attrType uint8, flags uint8, value []byte) []byte {
	valLen := len(value)
	var headerLen int
	var buf []byte
	if valLen > 255 {
		flags |= 0x10 // Extended length flag
		headerLen = 4
		buf = make([]byte, headerLen+valLen)
		buf[0] = flags
		buf[1] = attrType
		binary.BigEndian.PutUint16(buf[2:4], uint16(valLen))
		copy(buf[4:], value)
	} else {
		headerLen = 3
		buf = make([]byte, headerLen+valLen)
		buf[0] = flags
		buf[1] = attrType
		buf[2] = uint8(valLen)
		copy(buf[3:], value)
	}
	return buf
}

// EncodeOriginAttribute builds an ORIGIN attribute (Type 1, Transitive).
func EncodeOriginAttribute(origin uint8) []byte {
	return EncodePathAttribute(AttrOrigin, 0x40, []byte{origin})
}

// EncodeASPathAttribute builds an AS_PATH attribute (Type 2, Transitive).
func EncodeASPathAttribute(asns []uint32) []byte {
	segLen := len(asns)
	if segLen == 0 {
		return EncodePathAttribute(AttrASPath, 0x40, nil)
	}
	// AS_SEQUENCE segment with 2-byte or 4-byte ASNs; here standard 4-byte ASNs
	seg := make([]byte, 2+segLen*4)
	seg[0] = ASSequence
	seg[1] = uint8(segLen)
	for i, as := range asns {
		binary.BigEndian.PutUint32(seg[2+i*4:6+i*4], as)
	}
	return EncodePathAttribute(AttrASPath, 0x40, seg)
}

// EncodeNextHopAttribute builds a NEXT_HOP attribute (Type 3, Transitive).
func EncodeNextHopAttribute(nextHopIP string) []byte {
	ip := net.ParseIP(nextHopIP).To4()
	if ip == nil {
		ip = net.IPv4zero.To4()
	}
	return EncodePathAttribute(AttrNextHop, 0x40, ip)
}

// EncodeMEDAttribute builds a MULTI_EXIT_DISC attribute (Type 4, Optional Non-Transitive).
func EncodeMEDAttribute(med uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, med)
	return EncodePathAttribute(AttrMultiExitDisc, 0x80, buf)
}

// EncodeLocalPrefAttribute builds a LOCAL_PREF attribute (Type 5, Transitive).
func EncodeLocalPrefAttribute(localPref uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, localPref)
	return EncodePathAttribute(AttrLocalPref, 0x40, buf)
}

// EncodeCommunityAttribute encodes RFC 1997 BGP communities (Type 8, Optional Transitive).
func EncodeCommunityAttribute(communities []string) []byte {
	if len(communities) == 0 {
		return nil
	}
	var raw []byte
	for _, comm := range communities {
		var val uint32
		switch strings.ToUpper(comm) {
		case CommunityNoExport:
			val = 0xFFFFFF01
		case CommunityNoAdvertise:
			val = 0xFFFFFF02
		case CommunityNoExportSubConfed:
			val = 0xFFFFFF03
		default:
			parts := strings.Split(comm, ":")
			if len(parts) == 2 {
				as, _ := strconv.ParseUint(parts[0], 10, 16)
				v, _ := strconv.ParseUint(parts[1], 10, 16)
				val = uint32(as)<<16 | uint32(v)
			}
		}
		if val > 0 {
			buf := make([]byte, 4)
			binary.BigEndian.PutUint32(buf, val)
			raw = append(raw, buf...)
		}
	}
	if len(raw) == 0 {
		return nil
	}
	return EncodePathAttribute(AttrCommunities, 0xC0, raw) // Optional (0x80) | Transitive (0x40)
}

// EncodeUpdate builds an RFC 4271 UPDATE message with withdrawn routes, attributes, and NLRIs.
func EncodeUpdate(withdrawnCIDRs []string, attrs [][]byte, nlriCIDRs []string) ([]byte, error) {
	var withdrawnBuf []byte
	for _, cidr := range withdrawnCIDRs {
		enc, err := EncodeCIDR(cidr)
		if err != nil {
			return nil, err
		}
		withdrawnBuf = append(withdrawnBuf, enc...)
	}

	var attrBuf []byte
	for _, a := range attrs {
		attrBuf = append(attrBuf, a...)
	}

	var nlriBuf []byte
	for _, cidr := range nlriCIDRs {
		enc, err := EncodeCIDR(cidr)
		if err != nil {
			return nil, err
		}
		nlriBuf = append(nlriBuf, enc...)
	}

	totalLen := 2 + len(withdrawnBuf) + 2 + len(attrBuf) + len(nlriBuf)
	payload := make([]byte, totalLen)
	offset := 0

	binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(withdrawnBuf)))
	offset += 2
	copy(payload[offset:], withdrawnBuf)
	offset += len(withdrawnBuf)

	binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(attrBuf)))
	offset += 2
	copy(payload[offset:], attrBuf)
	offset += len(attrBuf)

	copy(payload[offset:], nlriBuf)

	return EncodeHeader(TypeUpdate, payload), nil
}

// ParseUpdate decodes an RFC 4271 UPDATE message.
func ParseUpdate(payload []byte) (withdrawn []string, attrs []PathAttribute, nlri []string, err error) {
	if len(payload) < 4 {
		return nil, nil, nil, ErrInvalidPacketLen
	}
	offset := 0

	withdrawnLen := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
	offset += 2
	if len(payload) < offset+withdrawnLen+2 {
		return nil, nil, nil, ErrInvalidPacketLen
	}
	withdrawnEnd := offset + withdrawnLen
	for offset < withdrawnEnd {
		cidr, n, err := DecodeCIDR(payload[offset:withdrawnEnd])
		if err != nil {
			return nil, nil, nil, err
		}
		withdrawn = append(withdrawn, cidr)
		offset += n
	}

	attrLen := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
	offset += 2
	if len(payload) < offset+attrLen {
		return nil, nil, nil, ErrInvalidPacketLen
	}
	attrEnd := offset + attrLen
	for offset < attrEnd {
		if offset+2 > attrEnd {
			break
		}
		flags := payload[offset]
		attrType := payload[offset+1]
		offset += 2
		var length int
		if flags&0x10 != 0 { // Extended length
			if offset+2 > attrEnd {
				break
			}
			length = int(binary.BigEndian.Uint16(payload[offset : offset+2]))
			offset += 2
		} else {
			if offset+1 > attrEnd {
				break
			}
			length = int(payload[offset])
			offset += 1
		}
		if offset+length > attrEnd {
			break
		}
		val := make([]byte, length)
		copy(val, payload[offset:offset+length])
		offset += length
		attrs = append(attrs, PathAttribute{
			Flags: flags,
			Type:  attrType,
			Value: val,
		})
	}

	for offset < len(payload) {
		cidr, n, err := DecodeCIDR(payload[offset:])
		if err != nil {
			break
		}
		nlri = append(nlri, cidr)
		offset += n
	}

	return withdrawn, attrs, nlri, nil
}
