package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

func (p *ProxyHandler) writePoolExhaustedError(w http.ResponseWriter, attempts int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-CF-Broker-Attempts", strconv.Itoa(attempts))
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "attempts": attempts})
}

func parseProxyPath(path string) (service, subPath string, err error) {
	if !strings.HasPrefix(path, "/proxy/") && path != "/proxy" {
		return "", "", errors.New("invalid route: path must start with /proxy/")
	}
	remainder := strings.TrimPrefix(strings.TrimPrefix(path, "/proxy"), "/")
	if remainder == "" {
		return "", "", errors.New("missing service identifier in proxy path")
	}
	parts := strings.SplitN(remainder, "/", 2)
	service = parts[0]
	if service == "" {
		return "", "", errors.New("service identifier cannot be empty")
	}
	if len(parts) > 1 {
		subPath = "/" + parts[1]
	}
	return service, subPath, nil
}

func isFailoverStatusCode(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func isHopByHop(canonicalKey string) bool {
	switch canonicalKey {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailers", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}
