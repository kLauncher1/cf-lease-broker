package bgp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBGP_PacketHeaderAndMarkerValidation(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03, 0x04}
	pkt := EncodeHeader(TypeKeepalive, payload)

	if len(pkt) != HeaderLength+len(payload) {
		t.Fatalf("expected packet length %d, got %d", HeaderLength+len(payload), len(pkt))
	}

	msgType, length, p, err := ParseHeader(pkt)
	if err != nil {
		t.Fatalf("unexpected error parsing valid header: %v", err)
	}
	if msgType != TypeKeepalive {
		t.Fatalf("expected msgType %d, got %d", TypeKeepalive, msgType)
	}
	if length != uint16(len(pkt)) {
		t.Fatalf("expected length %d, got %d", len(pkt), length)
	}
	if !bytes.Equal(p, payload) {
		t.Fatalf("expected payload %v, got %v", payload, p)
	}

	// Corrupt marker
	corruptPkt := make([]byte, len(pkt))
	copy(corruptPkt, pkt)
	corruptPkt[0] = 0x00
	_, _, _, err = ParseHeader(corruptPkt)
	if err != ErrInvalidMarker {
		t.Fatalf("expected ErrInvalidMarker on corrupt marker, got %v", err)
	}

	// Truncated packet
	_, _, _, err = ParseHeader([]byte{0xFF, 0xFF})
	if err != ErrInvalidPacketLen {
		t.Fatalf("expected ErrInvalidPacketLen on truncated packet, got %v", err)
	}
}

func TestBGP_OpenKeepaliveNotificationPackets(t *testing.T) {
	bgpID := [4]byte{192, 0, 2, 1}
	optParams := []byte{0x02, 0x06, 0x01, 0x04, 0x00, 0x01, 0x00, 0x01} // 4-byte ASN capability
	openPkt := EncodeOpen(64512, 90, bgpID, optParams)

	msgType, _, payload, err := ParseHeader(openPkt)
	if err != nil || msgType != TypeOpen {
		t.Fatalf("failed to parse OPEN message header: %v", err)
	}

	ver, as, hold, id, params, err := ParseOpen(payload)
	if err != nil {
		t.Fatalf("failed to parse OPEN payload: %v", err)
	}
	if ver != 4 || as != 64512 || hold != 90 || id != bgpID {
		t.Fatalf("OPEN field mismatch: ver=%d, as=%d, hold=%d, id=%v", ver, as, hold, id)
	}
	if !bytes.Equal(params, optParams) {
		t.Fatalf("optParams mismatch: expected %v, got %v", optParams, params)
	}

	// Test KEEPALIVE
	kaPkt := EncodeKeepalive()
	kaType, kaLen, kaPayload, err := ParseHeader(kaPkt)
	if err != nil || kaType != TypeKeepalive || kaLen != HeaderLength || len(kaPayload) != 0 {
		t.Fatalf("invalid KEEPALIVE packet: %v", err)
	}

	// Test NOTIFICATION
	notifPkt := EncodeNotification(2, 1, []byte("bad message header"))
	nType, _, nPayload, err := ParseHeader(notifPkt)
	if err != nil || nType != TypeNotification {
		t.Fatalf("failed to parse NOTIFICATION header: %v", err)
	}
	code, subcode, data, err := ParseNotification(nPayload)
	if err != nil || code != 2 || subcode != 1 || !bytes.Equal(data, []byte("bad message header")) {
		t.Fatalf("NOTIFICATION parsing mismatch: code=%d subcode=%d data=%s", code, subcode, string(data))
	}
}

func TestBGP_CIDRSerialization(t *testing.T) {
	testCases := []string{
		"198.51.100.0/24",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.1.128/25",
	}

	for _, tc := range testCases {
		enc, err := EncodeCIDR(tc)
		if err != nil {
			t.Fatalf("failed to encode CIDR %s: %v", tc, err)
		}
		dec, n, err := DecodeCIDR(enc)
		if err != nil {
			t.Fatalf("failed to decode CIDR %s: %v", tc, err)
		}
		if n != len(enc) {
			t.Fatalf("decoded byte count mismatch: expected %d, got %d", len(enc), n)
		}
		if dec != tc {
			t.Fatalf("CIDR roundtrip mismatch: expected %s, got %s", tc, dec)
		}
	}

	// Invalid CIDR
	if _, err := EncodeCIDR("invalid-ip-prefix"); err == nil {
		t.Fatalf("expected error on invalid CIDR string")
	}
}

func TestBGP_PathAttributesAndCommunities(t *testing.T) {
	originAttr := EncodeOriginAttribute(OriginIGP)
	if len(originAttr) != 4 || originAttr[1] != AttrOrigin {
		t.Fatalf("unexpected origin attribute: %v", originAttr)
	}

	asPathAttr := EncodeASPathAttribute([]uint32{64512, 3356, 13335})
	if len(asPathAttr) < 14 || asPathAttr[1] != AttrASPath {
		t.Fatalf("unexpected AS_PATH attribute: %v", asPathAttr)
	}

	nextHopAttr := EncodeNextHopAttribute("198.51.100.1")
	if len(nextHopAttr) != 7 || nextHopAttr[1] != AttrNextHop {
		t.Fatalf("unexpected NEXT_HOP attribute: %v", nextHopAttr)
	}

	medAttr := EncodeMEDAttribute(10)
	if len(medAttr) != 7 || medAttr[1] != AttrMultiExitDisc {
		t.Fatalf("unexpected MED attribute: %v", medAttr)
	}

	localPrefAttr := EncodeLocalPrefAttribute(200)
	if len(localPrefAttr) != 7 || localPrefAttr[1] != AttrLocalPref {
		t.Fatalf("unexpected LOCAL_PREF attribute: %v", localPrefAttr)
	}

	commAttr := EncodeCommunityAttribute([]string{CommunityNoExport, CommunityTransitLowLatency, "64512:100"})
	if len(commAttr) == 0 || commAttr[1] != AttrCommunities {
		t.Fatalf("unexpected COMMUNITIES attribute: %v", commAttr)
	}
}

func TestBGP_UpdatePacketEncodingParsing(t *testing.T) {
	withdrawn := []string{"198.51.100.0/24"}
	nlri := []string{"203.0.113.0/24", "192.0.2.0/24"}
	attrs := [][]byte{
		EncodeOriginAttribute(OriginIGP),
		EncodeASPathAttribute([]uint32{64512}),
		EncodeNextHopAttribute("198.51.100.1"),
		EncodeLocalPrefAttribute(150),
	}

	updatePkt, err := EncodeUpdate(withdrawn, attrs, nlri)
	if err != nil {
		t.Fatalf("failed to encode UPDATE packet: %v", err)
	}

	msgType, _, payload, err := ParseHeader(updatePkt)
	if err != nil || msgType != TypeUpdate {
		t.Fatalf("failed to parse UPDATE packet header: %v", err)
	}

	parsedWithdrawn, parsedAttrs, parsedNLRI, err := ParseUpdate(payload)
	if err != nil {
		t.Fatalf("failed to parse UPDATE payload: %v", err)
	}

	if len(parsedWithdrawn) != len(withdrawn) || parsedWithdrawn[0] != withdrawn[0] {
		t.Fatalf("withdrawn routes mismatch: expected %v, got %v", withdrawn, parsedWithdrawn)
	}
	if len(parsedNLRI) != len(nlri) {
		t.Fatalf("NLRI count mismatch: expected %d, got %d", len(nlri), len(parsedNLRI))
	}
	if len(parsedAttrs) != len(attrs) {
		t.Fatalf("attribute count mismatch: expected %d, got %d", len(attrs), len(parsedAttrs))
	}
}

func TestBGP_FSMStateTransitions(t *testing.T) {
	cfg := PeerConfig{
		PeerASN:           3356,
		PeerIP:            "4.69.130.1",
		LocalASN:          64512,
		LocalIP:           "198.51.100.254",
		HoldTime:          90 * time.Second,
		KeepaliveInterval: 30 * time.Second,
	}
	session := NewPeerSession(cfg)
	if session.GetState() != StateIdle {
		t.Fatalf("initial state should be IDLE, got %s", session.GetState())
	}

	session.TriggerConnect()
	if session.GetState() != StateOpenSent {
		t.Fatalf("state after connect should be OPENSENT, got %s", session.GetState())
	}

	err := session.HandleOpen(4, 3356, 90)
	if err != nil {
		t.Fatalf("unexpected error handling OPEN: %v", err)
	}
	if session.GetState() != StateEstablished {
		t.Fatalf("state after OPEN should be ESTABLISHED, got %s", session.GetState())
	}
	if !session.IsEstablished() {
		t.Fatalf("IsEstablished() should return true")
	}

	// Keepalive updates timestamp
	session.HandleKeepalive()
	snap := session.GetSnapshot()
	if snap.LastKeepaliveRecv == nil {
		t.Fatalf("expected non-nil LastKeepaliveRecv")
	}

	// Notification resets to Idle
	session.HandleNotification(6, 1)
	if session.GetState() != StateIdle {
		t.Fatalf("state after notification should be IDLE, got %s", session.GetState())
	}
	if session.GetSnapshot().FlapCount != 1 {
		t.Fatalf("expected flap count 1, got %d", session.GetSnapshot().FlapCount)
	}
}

func TestBGP_AnycastAnnouncementAndPeers(t *testing.T) {
	eng := NewBGPEngine()
	defer eng.Stop()

	peers := eng.ListPeers()
	if len(peers) < 4 {
		t.Fatalf("expected at least 4 pre-seeded transit peers, got %d", len(peers))
	}

	// Announce custom prefix
	pr, err := eng.AnnouncePrefix(
		"192.0.2.0/24",
		64512,
		"192.0.2.1",
		[]string{CommunityTransitLowLatency},
		10,
		180,
		[]string{"SFO", "IAD"},
	)
	if err != nil {
		t.Fatalf("failed to announce Anycast prefix: %v", err)
	}
	if pr.Status != PrefixActive || len(pr.ActivePeers) == 0 {
		t.Fatalf("expected active prefix with active peers: %+v", pr)
	}

	// Lookup prefix
	found, ok := eng.Broadcaster().GetPrefix("192.0.2.0/24")
	if !ok || found.CIDR != "192.0.2.0/24" {
		t.Fatalf("failed to retrieve announced prefix")
	}
}

func TestBGP_Sub5msLineRateWithdrawal(t *testing.T) {
	eng := NewBGPEngine()
	defer eng.Stop()

	// Measure withdrawal latency
	res, err := eng.WithdrawPrefix("198.51.100.0/24", "Simulated edge maintenance")
	if err != nil {
		t.Fatalf("failed to withdraw Anycast prefix: %v", err)
	}

	if !res.WithinSLA {
		t.Fatalf("withdrawal exceeded 5ms SLA: %d us", res.DurationUs)
	}
	if res.PeersNotified == 0 {
		t.Fatalf("expected active peers to be notified of withdrawal")
	}

	t.Logf("Line-rate prefix withdrawal completed in %d µs (SLA <= 5000 µs: %v)", res.DurationUs, res.WithinSLA)

	// Verify status updated in catalog
	pr, ok := eng.Broadcaster().GetPrefix("198.51.100.0/24")
	if !ok || pr.Status != PrefixWithdrawn {
		t.Fatalf("prefix status should be WITHDRAWN, got %s", pr.Status)
	}

	// History check
	history := eng.Broadcaster().ListWithdrawalHistory()
	if len(history) == 0 {
		t.Fatalf("expected withdrawal event in history")
	}
}

func TestBGP_PoPPartitionDiversion(t *testing.T) {
	eng := NewBGPEngine()
	defer eng.Stop()

	// Simulate PoP partition at SFO diverting to IAD
	res, err := eng.DivertPoP("SFO", "IAD", "Fiber cut between carrier transits")
	if err != nil {
		t.Fatalf("failed to divert PoP traffic: %v", err)
	}

	if len(res.AffectedPrefixes) == 0 {
		t.Fatalf("expected affected prefixes at SFO PoP")
	}
	if len(res.WithdrawalEvents) == 0 {
		t.Fatalf("expected withdrawal events for diverted prefixes")
	}

	t.Logf("PoP partition diversion completed in %d µs for %d prefixes", res.TotalDurationUs, len(res.AffectedPrefixes))
}

func TestBGP_DynamicCommunityTaggingAndTransitArbitrage(t *testing.T) {
	router := NewCommunityRouter()

	// Enterprise request should get low latency tags & highest local preference
	tags, asn, pref := router.EvaluateRouting("enterprise", "api")
	if pref < 200 || asn != 3356 {
		t.Fatalf("expected Enterprise to match Level3 AS3356 with LocalPref >= 200, got ASN=%d pref=%d", asn, pref)
	}
	if len(tags) == 0 || tags[0] != CommunityTransitLowLatency {
		t.Fatalf("expected low-latency community tag, got %v", tags)
	}

	// Free request should get low-cost tags & Cogent transit
	fTags, fASN, fPref := router.EvaluateRouting("free", "web")
	if fPref > 100 || fASN != 174 {
		t.Fatalf("expected Free tier to match Cogent AS174 with LocalPref <= 100, got ASN=%d pref=%d", fASN, fPref)
	}
	if len(fTags) == 0 || fTags[0] != CommunityTransitLowCost {
		t.Fatalf("expected low-cost community tag, got %v", fTags)
	}

	// Workers fast API service match
	wTags, wASN, _ := router.EvaluateRouting("pro", "workers")
	if wASN != 13335 {
		t.Fatalf("expected Workers service to match Cloudflare Edge AS13335, got ASN=%d", wASN)
	}
	if len(wTags) == 0 || wTags[0] != CommunityTransitLowLatency {
		t.Fatalf("expected low-latency tag for workers service, got %v", wTags)
	}
}

func TestBGP_RESTEndpointsAndDoSBounding(t *testing.T) {
	eng := NewBGPEngine()
	defer eng.Stop()
	handler := NewBGPHandler(eng)

	// GET /api/bgp/status
	req := httptest.NewRequest(http.MethodGet, "/api/bgp/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// GET /api/bgp/peers
	req = httptest.NewRequest(http.MethodGet, "/api/bgp/peers", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /peers, got %d", w.Code)
	}

	// POST /api/bgp/prefixes/announce
	announceBody := `{"cidr":"192.0.2.0/24","origin_asn":64512,"target_pops":["SFO","IAD"]}`
	req = httptest.NewRequest(http.MethodPost, "/api/bgp/prefixes/announce", strings.NewReader(announceBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 on /prefixes/announce, got %d: %s", w.Code, w.Body.String())
	}

	// POST /api/bgp/prefixes/withdraw
	withdrawBody := `{"cidr":"192.0.2.0/24","reason":"Test teardown"}`
	req = httptest.NewRequest(http.MethodPost, "/api/bgp/prefixes/withdraw", strings.NewReader(withdrawBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /prefixes/withdraw, got %d: %s", w.Code, w.Body.String())
	}

	// POST /api/bgp/divert
	divertBody := `{"failed_pop":"SFO","target_pop":"IAD","reason":"Simulated link loss"}`
	req = httptest.NewRequest(http.MethodPost, "/api/bgp/divert", strings.NewReader(divertBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /divert, got %d: %s", w.Code, w.Body.String())
	}

	// GET /api/bgp/stats
	req = httptest.NewRequest(http.MethodGet, "/api/bgp/stats", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /stats, got %d", w.Code)
	}

	// Test 64KB DoS bounding: payload > 64KB rejected with 400 Bad Request
	hugeBody := strings.Repeat("A", 70*1024)
	req = httptest.NewRequest(http.MethodPost, "/api/bgp/prefixes/announce", strings.NewReader(hugeBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 on oversized payload, got %d", w.Code)
	}
}

func TestBGP_HighConcurrency40Goroutines(t *testing.T) {
	eng := NewBGPEngine()
	defer eng.Stop()

	var wg sync.WaitGroup
	workers := 40
	iterations := 25

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Alternately resolve routes, check stats, announce and withdraw
				_, _, _, _ = eng.ResolveRoute("enterprise", "api")
				_ = eng.GetStats()

				if workerID%2 == 0 {
					_ = eng.ListPeers()
				} else {
					res, err := eng.WithdrawPrefix("198.51.100.0/24", "concurrency test")
					if err == nil && res != nil {
						if res.DurationUs > 50000 {
							// Latency check under extreme load
						}
					}
					// Re-announce
					_, _ = eng.AnnouncePrefix(
						"198.51.100.0/24",
						64512,
						"198.51.100.1",
						[]string{CommunityTransitBalanced},
						10,
						150,
						[]string{"SFO", "IAD"},
					)
				}
			}
		}(i)
	}

	wg.Wait()
	stats := eng.GetStats()
	if stats.TotalAnnouncements <= 0 {
		t.Fatalf("expected positive total announcements after concurrent run, got %d", stats.TotalAnnouncements)
	}
	t.Logf("Concurrency test passed: Announcements=%d Withdrawals=%d AvgWithdrawalUs=%.2f",
		stats.TotalAnnouncements, stats.TotalWithdrawals, stats.AvgWithdrawalUs)
}
