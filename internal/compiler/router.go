package compiler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidPattern = errors.New("invalid route pattern format")
	ErrRouteNotFound  = errors.New("route pattern not found")
)

// RouteBinding defines an active zone route mapped to an Edge Worker bundle.
type RouteBinding struct {
	ID        string    `json:"id"`
	ZoneID    string    `json:"zone_id"`
	Pattern   string    `json:"pattern"`
	HostPart  string    `json:"host_part"`
	PathPart  string    `json:"path_part"`
	TenantID  string    `json:"tenant_id"`
	WorkerID  string    `json:"worker_id"`
	Priority  int       `json:"priority"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// RouteCompiler parses, scores, and matches incoming request URLs against registered routes.
type RouteCompiler struct {
	mu     sync.RWMutex
	routes map[string]*RouteBinding // route ID -> RouteBinding
}

// NewRouteCompiler initializes a thread-safe multi-zone route compiler.
func NewRouteCompiler() *RouteCompiler {
	return &RouteCompiler{
		routes: make(map[string]*RouteBinding),
	}
}

// ParsePattern decomposes a route pattern string into host and path parts and calculates priority.
func ParsePattern(pattern string) (hostPart string, pathPart string, score int, err error) {
	clean := strings.TrimSpace(pattern)
	if clean == "" {
		return "", "", 0, ErrInvalidPattern
	}

	// Normalize scheme if present
	if strings.Contains(clean, "://") {
		u, parseErr := url.Parse(clean)
		if parseErr != nil {
			return "", "", 0, fmt.Errorf("%w: %v", ErrInvalidPattern, parseErr)
		}
		clean = u.Host + u.Path
	}

	slashIdx := strings.IndexByte(clean, '/')
	if slashIdx == -1 {
		hostPart = clean
		pathPart = "/*"
	} else {
		hostPart = clean[:slashIdx]
		pathPart = clean[slashIdx:]
	}

	if hostPart == "" {
		hostPart = "*"
	}
	if pathPart == "" {
		pathPart = "/*"
	}

	score = calculatePatternScore(hostPart, pathPart)
	return strings.ToLower(hostPart), pathPart, score, nil
}

func calculatePatternScore(host, path string) int {
	score := 0

	// Host scoring
	if host == "*" {
		score += 1000
	} else if strings.HasPrefix(host, "*.") {
		score += 2000 + len(host)
	} else {
		score += 3000 + len(host)
	}

	// Path scoring
	if path == "/*" || path == "*" {
		score += 100
	} else if strings.HasSuffix(path, "/*") {
		score += 200 + len(path)
	} else {
		score += 300 + len(path)
	}

	return score
}

// RegisterRoute compiles and stores a new dynamic route binding.
func (c *RouteCompiler) RegisterRoute(pattern, zoneID, tenantID, workerID string) (*RouteBinding, error) {
	hostPart, pathPart, score, err := ParsePattern(pattern)
	if err != nil {
		return nil, err
	}

	if zoneID == "" {
		zoneID = "zone-default"
	}
	if tenantID == "" {
		tenantID = "tenant-default"
	}
	if workerID == "" {
		workerID = "worker-default"
	}

	idBytes := make([]byte, 6)
	_, _ = rand.Read(idBytes)
	routeID := fmt.Sprintf("route-%s", hex.EncodeToString(idBytes))

	binding := &RouteBinding{
		ID:        routeID,
		ZoneID:    zoneID,
		Pattern:   pattern,
		HostPart:  hostPart,
		PathPart:  pathPart,
		TenantID:  tenantID,
		WorkerID:  workerID,
		Priority:  score,
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.routes[routeID] = binding
	return binding, nil
}

// RemoveRoute deletes a route binding by its ID.
func (c *RouteCompiler) RemoveRoute(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.routes[id]; !exists {
		return ErrRouteNotFound
	}
	delete(c.routes, id)
	return nil
}

// Match evaluates an incoming request host and path against all enabled routes, returning the best match.
func (c *RouteCompiler) Match(host, path string) (*RouteBinding, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	reqHost := strings.ToLower(strings.TrimSpace(host))
	// Strip port if present
	if colon := strings.IndexByte(reqHost, ':'); colon != -1 {
		reqHost = reqHost[:colon]
	}

	reqPath := path
	if !strings.HasPrefix(reqPath, "/") {
		reqPath = "/" + reqPath
	}

	var candidates []*RouteBinding
	for _, r := range c.routes {
		if !r.Enabled {
			continue
		}
		if matchHost(r.HostPart, reqHost) && matchPath(r.PathPart, reqPath) {
			candidates = append(candidates, r)
		}
	}

	if len(candidates) == 0 {
		return nil, false
	}

	// Sort candidates by priority descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority > candidates[j].Priority
	})

	cp := *candidates[0]
	return &cp, true
}

func matchHost(pattern, target string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == target {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // e.g. ".example.com"
		return strings.HasSuffix(target, suffix) || target == pattern[2:]
	}
	return false
}

func matchPath(pattern, target string) bool {
	if pattern == "/*" || pattern == "*" {
		return true
	}
	if pattern == target {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(target, prefix)
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(target, prefix)
	}
	return false
}

// ListRoutes returns all active route bindings sorted by priority descending.
func (c *RouteCompiler) ListRoutes() []*RouteBinding {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]*RouteBinding, 0, len(c.routes))
	for _, r := range c.routes {
		cp := *r
		out = append(out, &cp)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Priority > out[j].Priority
	})
	return out
}
