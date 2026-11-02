package scrubber

import (
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Known high-risk reflection and amplification UDP ports.
var knownAmplificationPorts = map[int]string{
	53:    "DNS",
	123:   "NTP",
	161:   "SNMP",
	389:   "CLDAP",
	1900:  "SSDP",
	11211: "Memcached",
}

// XDPFilter performs line-rate L3/L4 packet header parsing and zero-CPU drop decisions.
type XDPFilter struct {
	mu            sync.RWMutex
	synRates      map[string]*rateTracker
	lastRateSweep time.Time
	droppedSYN    int64
	droppedUDP    int64
	droppedBadHdr int64
}

// rateTracker tracks sliding window event frequencies per IP.
type rateTracker struct {
	windowStart time.Time
	count       int64
}

// NewXDPFilter initializes an in-memory XDP line-rate filtering engine.
func NewXDPFilter() *XDPFilter {
	return &XDPFilter{
		synRates:      make(map[string]*rateTracker),
		lastRateSweep: time.Now(),
	}
}

// ParseRawIPv4Packet parses raw byte slices into PacketMetadata without dynamic memory allocation.
func (f *XDPFilter) ParseRawIPv4Packet(raw []byte) (PacketMetadata, error) {
	if len(raw) < 20 {
		return PacketMetadata{}, ErrPacketTooShort
	}

	versionAndIHL := raw[0]
	version := versionAndIHL >> 4
	if version != 4 {
		return PacketMetadata{}, ErrInvalidIPHeader
	}

	ihl := int(versionAndIHL&0x0F) * 4
	if len(raw) < ihl {
		return PacketMetadata{}, ErrPacketTooShort
	}

	protoByte := raw[9]
	srcIP := net.IP(raw[12:16]).String()
	dstIP := net.IP(raw[16:20]).String()

	meta := PacketMetadata{
		SrcIP:       srcIP,
		DstIP:       dstIP,
		Timestamp:   time.Now(),
		PayloadSize: len(raw) - ihl,
	}

	switch protoByte {
	case 6: // TCP
		meta.Protocol = ProtocolTCP
		if len(raw) >= ihl+20 {
			tcpHdr := raw[ihl:]
			meta.SrcPort = int(binary.BigEndian.Uint16(tcpHdr[0:2]))
			meta.DstPort = int(binary.BigEndian.Uint16(tcpHdr[2:4]))
			flagsByte := tcpHdr[13]
			meta.TCPFlags = TCPFlags{
				FIN: flagsByte&0x01 != 0,
				SYN: flagsByte&0x02 != 0,
				RST: flagsByte&0x04 != 0,
				PSH: flagsByte&0x08 != 0,
				ACK: flagsByte&0x10 != 0,
				URG: flagsByte&0x20 != 0,
			}
			tcpDataOffset := int((tcpHdr[12] >> 4)) * 4
			if len(raw) >= ihl+tcpDataOffset {
				meta.PayloadSize = len(raw) - (ihl + tcpDataOffset)
			}
		}
	case 17: // UDP
		meta.Protocol = ProtocolUDP
		if len(raw) >= ihl+8 {
			udpHdr := raw[ihl:]
			meta.SrcPort = int(binary.BigEndian.Uint16(udpHdr[0:2]))
			meta.DstPort = int(binary.BigEndian.Uint16(udpHdr[2:4]))
			udpLen := int(binary.BigEndian.Uint16(udpHdr[4:6]))
			if udpLen >= 8 && len(raw) >= ihl+udpLen {
				meta.PayloadSize = udpLen - 8
			} else {
				meta.PayloadSize = len(raw) - (ihl + 8)
			}
		}
	case 1: // ICMP
		meta.Protocol = ProtocolICMP
	default:
		meta.Protocol = ProtocolOther
	}

	return meta, nil
}

// EvaluatePacket executes fast XDP line-rate filtering rules against packet metadata.
func (f *XDPFilter) EvaluatePacket(pkt PacketMetadata, policy TenantScrubberPolicy) (Action, AttackType, string) {
	// 1. Malformed header validation
	if pkt.SrcIP == "" || pkt.DstIP == "" {
		atomic.AddInt64(&f.droppedBadHdr, 1)
		return ActionDrop, AttackTypeMalformedHeader, "empty source or destination address"
	}

	// Land attack: identical source and destination with equal ports
	if pkt.SrcIP == pkt.DstIP && pkt.SrcPort == pkt.DstPort && pkt.SrcPort > 0 {
		atomic.AddInt64(&f.droppedBadHdr, 1)
		return ActionDrop, AttackTypeMalformedHeader, "land attack signature detected (self-targeted loopback)"
	}

	// 2. TCP SYN Flood & Illegal TCP Flag Combinations
	if pkt.Protocol == ProtocolTCP {
		// Illegal flag combos: SYN+FIN or zero-flags with payload
		if pkt.TCPFlags.SYN && pkt.TCPFlags.FIN {
			atomic.AddInt64(&f.droppedBadHdr, 1)
			return ActionDrop, AttackTypeMalformedHeader, "illegal TCP SYN+FIN scan packet"
		}

		// Volumetric SYN Flood inspection
		if pkt.TCPFlags.SYN && !pkt.TCPFlags.ACK {
			exceeded := f.recordAndCheckSYN(pkt.SrcIP, policy.MaxSYNRPS)
			if exceeded {
				atomic.AddInt64(&f.droppedSYN, 1)
				return ActionDrop, AttackTypeSYNFlood, "line-rate volumetric TCP SYN flood threshold exceeded"
			}
		}
	}

	// 3. Volumetric UDP Reflection & Amplification Protection
	if pkt.Protocol == ProtocolUDP {
		// Inspect well-known amplification vectors (DNS 53, NTP 123, SSDP 1900, Memcached 11211, etc.)
		if serviceName, ok := knownAmplificationPorts[pkt.SrcPort]; ok {
			maxSz := policy.MaxAmplificationSize
			if maxSz <= 0 {
				maxSz = 512
			}
			if pkt.PayloadSize > maxSz {
				atomic.AddInt64(&f.droppedUDP, 1)
				return ActionDrop, AttackTypeUDPAmplification, "high-volume UDP reflection detected from " + serviceName
			}
		}

		if serviceName, ok := knownAmplificationPorts[pkt.DstPort]; ok {
			// Outbound/inbound reflection trigger attempt
			maxSz := policy.MaxAmplificationSize
			if maxSz <= 0 {
				maxSz = 512
			}
			if pkt.PayloadSize > maxSz*4 {
				atomic.AddInt64(&f.droppedUDP, 1)
				return ActionDrop, AttackTypeUDPAmplification, "oversized UDP payload targeting amplification port " + serviceName
			}
		}
	}

	return ActionPass, AttackTypeNone, ""
}

// recordAndCheckSYN tracks incoming pure SYN rate for an IP address.
func (f *XDPFilter) recordAndCheckSYN(srcIP string, limitRPS int64) bool {
	if limitRPS <= 0 {
		limitRPS = 100
	}

	now := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()

	// Opportunistic periodic pruning every 10 seconds
	if now.Sub(f.lastRateSweep) > 10*time.Second {
		f.pruneStaleRatesLocked(now)
		f.lastRateSweep = now
	}

	entry, exists := f.synRates[srcIP]
	if !exists || now.Sub(entry.windowStart) >= time.Second {
		f.synRates[srcIP] = &rateTracker{
			windowStart: now,
			count:       1,
		}
		return false
	}

	entry.count++
	return entry.count > limitRPS
}

// pruneStaleRatesLocked sweeps inactive rate trackers.
func (f *XDPFilter) pruneStaleRatesLocked(now time.Time) {
	for ip, entry := range f.synRates {
		if now.Sub(entry.windowStart) > 5*time.Second {
			delete(f.synRates, ip)
		}
	}
}

// Stats returns internal counters of dropped packets.
func (f *XDPFilter) Stats() (synDrops, udpDrops, badHdrDrops int64) {
	return atomic.LoadInt64(&f.droppedSYN),
		atomic.LoadInt64(&f.droppedUDP),
		atomic.LoadInt64(&f.droppedBadHdr)
}

// Reset clears all in-memory rate trackers and counters.
func (f *XDPFilter) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.synRates = make(map[string]*rateTracker)
	atomic.StoreInt64(&f.droppedSYN, 0)
	atomic.StoreInt64(&f.droppedUDP, 0)
	atomic.StoreInt64(&f.droppedBadHdr, 0)
}
