package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

var errBodyTooLarge = errors.New("request body too large")

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

func (p *ProxyHandler) bufferRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer func() { _ = r.Body.Close() }()

	maxBytes := p.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}

	limitedReader := io.LimitReader(r.Body, maxBytes+1)
	buf, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxBytes {
		return nil, errBodyTooLarge
	}
	return buf, nil
}

func (p *ProxyHandler) buildTargetURL(service, accountID, subPath, rawQuery string) string {
	var dest string
	if p.TargetURLBuilder != nil {
		dest = p.TargetURLBuilder(service, accountID, subPath)
	} else {
		trimmed := strings.TrimPrefix(subPath, "/")
		if trimmed != "" {
			dest = fmt.Sprintf("https://%s.%s.workers.dev/%s", service, accountID, trimmed)
		} else {
			dest = fmt.Sprintf("https://%s.%s.workers.dev", service, accountID)
		}
	}

	if rawQuery != "" {
		if strings.Contains(dest, "?") {
			dest += "&" + rawQuery
		} else {
			dest += "?" + rawQuery
		}
	}
	return dest
}
