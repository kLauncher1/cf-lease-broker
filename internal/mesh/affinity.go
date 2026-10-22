package mesh

import (
	"math"
	"strings"
)

// RegionCoordinates maps edge region identifiers to latitude and longitude.
type RegionCoordinates struct {
	Lat float64
	Lon float64
}

// Predefined Cloudflare edge regional coordinates for anycast affinity steering.
var regionGeoTable = map[string]RegionCoordinates{
	"iad": {Lat: 39.0438, Lon: -77.4874},  // North America East (Ashburn)
	"sfo": {Lat: 37.7749, Lon: -122.4194}, // North America West (San Francisco)
	"ord": {Lat: 41.8781, Lon: -87.6298},  // North America Central (Chicago)
	"lhr": {Lat: 51.5074, Lon: -0.1278},   // Europe West (London)
	"fra": {Lat: 50.1109, Lon: 8.6821},    // Europe Central (Frankfurt)
	"ams": {Lat: 52.3676, Lon: 4.9041},    // Europe North (Amsterdam)
	"nrt": {Lat: 35.6762, Lon: 139.6503},  // Asia East (Tokyo)
	"sin": {Lat: 1.3521, Lon: 103.8198},   // Asia South (Singapore)
	"hkg": {Lat: 22.3193, Lon: 114.1694},  // Asia Central (Hong Kong)
	"syd": {Lat: -33.8688, Lon: 151.2093}, // Oceania (Sydney)
	"gru": {Lat: -23.5505, Lon: -46.6333}, // South America (São Paulo)
	"jnb": {Lat: -26.2041, Lon: 28.0473},  // Africa (Johannesburg)
}

// AffinityEngine evaluates geographic and network distance to steer requests.
type AffinityEngine struct {
	localID     string
	localRegion string
}

// NewAffinityEngine constructs an edge connection affinity resolver.
func NewAffinityEngine(localID, localRegion string) *AffinityEngine {
	return &AffinityEngine{
		localID:     localID,
		localRegion: strings.ToLower(strings.TrimSpace(localRegion)),
	}
}

// haversineKm computes Great-Circle distance between two coordinates in kilometers.
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0

	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	rLat1 := lat1 * (math.Pi / 180.0)
	rLat2 := lat2 * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(rLat1)*math.Cos(rLat2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusKm * c
}

// CalculateRegionDistance computes the geographic distance between two edge regions.
func (a *AffinityEngine) CalculateRegionDistance(r1, r2 string) float64 {
	r1 = strings.ToLower(strings.TrimSpace(r1))
	r2 = strings.ToLower(strings.TrimSpace(r2))

	if r1 == r2 || r1 == "" || r2 == "" {
		return 0.0
	}

	geo1, ok1 := regionGeoTable[r1]
	geo2, ok2 := regionGeoTable[r2]

	if !ok1 || !ok2 {
		// Default cross-continental distance estimate if unmapped
		return 5000.0
	}

	return haversineKm(geo1.Lat, geo1.Lon, geo2.Lat, geo2.Lon)
}

// ResolveAffinity decides whether the local broker serves the key or steers to a peer.
func (a *AffinityEngine) ResolveAffinity(clientRegion string, loc *ShardLocation) *AffinityDecision {
	clientRegion = strings.ToLower(strings.TrimSpace(clientRegion))
	if clientRegion == "" {
		clientRegion = a.localRegion
	}

	candidates := make([]PeerNode, 0, len(loc.ReplicaNodes)+1)
	if loc.PrimaryNode.Status != NodeStatusDead {
		candidates = append(candidates, loc.PrimaryNode)
	}
	for _, rep := range loc.ReplicaNodes {
		if rep.Status != NodeStatusDead {
			candidates = append(candidates, rep)
		}
	}

	if len(candidates) == 0 {
		return &AffinityDecision{
			Key:          loc.Key,
			ClientRegion: clientRegion,
			SelectedNode: loc.PrimaryNode,
			IsLocalServe: loc.PrimaryNode.ID == a.localID,
			DistanceKm:   a.CalculateRegionDistance(clientRegion, loc.PrimaryNode.Region),
			Reason:       "fallback_primary_node",
		}
	}

	var bestNode PeerNode
	bestDist := math.MaxFloat64
	localIsCandidate := false
	var localCandidateNode PeerNode

	for _, cand := range candidates {
		if cand.ID == a.localID {
			localIsCandidate = true
			localCandidateNode = cand
		}
		dist := a.CalculateRegionDistance(clientRegion, cand.Region)
		if dist < bestDist {
			bestDist = dist
			bestNode = cand
		}
	}

	// Affinity rule: if local node is in the shard candidate pool and has exact region match
	// or is within 250km of the closest candidate, serve locally to minimize latency & WAN egress.
	if localIsCandidate {
		localDist := a.CalculateRegionDistance(clientRegion, localCandidateNode.Region)
		if localCandidateNode.Region == clientRegion || localDist <= bestDist+250.0 {
			return &AffinityDecision{
				Key:          loc.Key,
				ClientRegion: clientRegion,
				SelectedNode: localCandidateNode,
				IsLocalServe: true,
				DistanceKm:   localDist,
				Reason:       "local_replica_affinity_match",
			}
		}
	}

	// Otherwise, steer request towards the closest healthy regional replica
	isLocal := bestNode.ID == a.localID
	return &AffinityDecision{
		Key:          loc.Key,
		ClientRegion: clientRegion,
		SelectedNode: bestNode,
		IsLocalServe: isLocal,
		DistanceKm:   bestDist,
		Reason:       "nearest_healthy_regional_replica",
	}
}
