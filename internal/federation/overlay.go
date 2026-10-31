package federation

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// OverlayRouter manages dynamic boundary route advertisements and overlay path resolution.
type OverlayRouter struct {
	mu            sync.RWMutex
	routes        map[string]*OverlayRoute
	tunnelManager *TunnelManager
}

// NewOverlayRouter creates an overlay router linked with a WAN tunnel manager.
func NewOverlayRouter(tm *TunnelManager) *OverlayRouter {
	return &OverlayRouter{
		routes:        make(map[string]*OverlayRoute),
		tunnelManager: tm,
	}
}

// AdvertiseRoute installs or updates an overlay boundary route.
func (or *OverlayRouter) AdvertiseRoute(route *OverlayRoute) error {
	if route == nil {
		return fmt.Errorf("cannot advertise nil route")
	}
	if route.ID == "" {
		return fmt.Errorf("route ID cannot be empty")
	}
	if route.DestinationCIDR == "" {
		return fmt.Errorf("route destination CIDR/prefix cannot be empty")
	}
	if route.TargetClusterID == "" {
		return fmt.Errorf("target cluster ID cannot be empty")
	}

	or.mu.Lock()
	defer or.mu.Unlock()

	if route.Status == "" {
		route.Status = RouteStatusActive
	}
	if route.LastAdvertised.IsZero() {
		route.LastAdvertised = time.Now()
	}

	rCopy := *route
	or.routes[route.ID] = &rCopy
	return nil
}

// WithdrawRoute marks a route as withdrawn by ID.
func (or *OverlayRouter) WithdrawRoute(routeID string) error {
	or.mu.Lock()
	defer or.mu.Unlock()

	route, exists := or.routes[routeID]
	if !exists {
		return ErrRouteNotFound
	}

	route.Status = RouteStatusWithdrawn
	route.LastAdvertised = time.Now()
	return nil
}

// WithdrawByCluster withdraws all advertised routes targeting a specific cluster ID.
func (or *OverlayRouter) WithdrawByCluster(clusterID string) int {
	or.mu.Lock()
	defer or.mu.Unlock()

	count := 0
	now := time.Now()
	for _, route := range or.routes {
		if route.TargetClusterID == clusterID && route.Status == RouteStatusActive {
			route.Status = RouteStatusWithdrawn
			route.LastAdvertised = now
			count++
		}
	}
	return count
}

// GetRoute retrieves a specific overlay route by ID.
func (or *OverlayRouter) GetRoute(routeID string) (*OverlayRoute, bool) {
	or.mu.RLock()
	defer or.mu.RUnlock()

	route, exists := or.routes[routeID]
	if !exists {
		return nil, false
	}
	rCopy := *route
	return &rCopy, true
}

// ListRoutes returns all known overlay routes sorted deterministically by ID.
func (or *OverlayRouter) ListRoutes() []*OverlayRoute {
	or.mu.RLock()
	defer or.mu.RUnlock()

	result := make([]*OverlayRoute, 0, len(or.routes))
	for _, r := range or.routes {
		rCopy := *r
		result = append(result, &rCopy)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// ListActiveRoutes returns only routes that are currently active.
func (or *OverlayRouter) ListActiveRoutes() []*OverlayRoute {
	or.mu.RLock()
	defer or.mu.RUnlock()

	var result []*OverlayRoute
	for _, r := range or.routes {
		if r.Status == RouteStatusActive {
			rCopy := *r
			result = append(result, &rCopy)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// ResolveRoute finds the optimal next-hop route for a target destination.
func (or *OverlayRouter) ResolveRoute(destination string) (*OverlayRoute, error) {
	if destination == "" {
		return nil, fmt.Errorf("destination cannot be empty")
	}

	or.mu.RLock()
	defer or.mu.RUnlock()

	var candidates []*OverlayRoute
	destLower := strings.ToLower(destination)

	for _, r := range or.routes {
		if r.Status != RouteStatusActive {
			continue
		}

		destPrefix := strings.ToLower(r.DestinationCIDR)
		if matchDestination(destLower, destPrefix) {
			rCopy := *r
			candidates = append(candidates, &rCopy)
		}
	}

	if len(candidates) == 0 {
		return nil, ErrNoViableRoute
	}

	// Rank candidates:
	// 1. NextHop tunnel health (connected preferred)
	// 2. Metric (lower is better)
	// 3. ASPath length (shorter is better)
	// 4. Most recently advertised
	sort.Slice(candidates, func(i, j int) bool {
		ci, cj := candidates[i], candidates[j]

		// Tunnel health check
		tunIConnected := or.isTunnelConnected(ci.NextHopTunnelID)
		tunJConnected := or.isTunnelConnected(cj.NextHopTunnelID)
		if tunIConnected != tunJConnected {
			return tunIConnected
		}

		if ci.Metric != cj.Metric {
			return ci.Metric < cj.Metric
		}

		if len(ci.ASPath) != len(cj.ASPath) {
			return len(ci.ASPath) < len(cj.ASPath)
		}

		return ci.LastAdvertised.After(cj.LastAdvertised)
	})

	return candidates[0], nil
}

// SimulateBoundaryExchange accepts advertised boundary routes from an upstream peer cluster.
func (or *OverlayRouter) SimulateBoundaryExchange(advertisedBy string, routes []*OverlayRoute) (int, error) {
	if len(routes) == 0 {
		return 0, nil
	}

	or.mu.Lock()
	defer or.mu.Unlock()

	count := 0
	now := time.Now()
	for _, r := range routes {
		if r == nil || r.ID == "" || r.DestinationCIDR == "" {
			continue
		}

		rCopy := *r
		rCopy.AdvertisedBy = advertisedBy
		rCopy.LastAdvertised = now
		rCopy.Status = RouteStatusActive
		or.routes[rCopy.ID] = &rCopy
		count++
	}
	return count, nil
}

func (or *OverlayRouter) isTunnelConnected(tunnelID string) bool {
	if or.tunnelManager == nil || tunnelID == "" {
		return false
	}
	tun, found := or.tunnelManager.GetTunnel(tunnelID)
	return found && tun.Status == TunnelStatusConnected
}

func matchDestination(dest, rule string) bool {
	if dest == rule || rule == "*" {
		return true
	}
	// Wildcard prefix matching like "tenant-eu/*" or "service-*-eu"
	if strings.HasSuffix(rule, "/*") {
		prefix := strings.TrimSuffix(rule, "/*")
		return strings.HasPrefix(dest, prefix)
	}
	if strings.HasSuffix(rule, "*") {
		prefix := strings.TrimSuffix(rule, "*")
		return strings.HasPrefix(dest, prefix)
	}
	if strings.Contains(rule, "*") {
		// Pattern like "service-*-eu"
		parts := strings.Split(rule, "*")
		if len(parts) == 2 {
			return strings.HasPrefix(dest, parts[0]) && strings.HasSuffix(dest, parts[1])
		}
	}
	// CIDR subnet matching
	if strings.Contains(rule, "/") {
		if _, ipNet, err := net.ParseCIDR(rule); err == nil {
			if ip := net.ParseIP(dest); ip != nil {
				return ipNet.Contains(ip)
			}
		}
		basePrefix := strings.Split(rule, "/")[0]
		if strings.HasSuffix(basePrefix, ".0") {
			basePrefix = strings.TrimSuffix(basePrefix, "0")
		}
		return strings.HasPrefix(dest, basePrefix) || strings.HasPrefix(dest, rule)
	}
	return strings.Contains(dest, rule)
}
