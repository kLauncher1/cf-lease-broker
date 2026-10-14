package mtls

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// MTLSHandler provides RESTful endpoints for zero-trust certificate lifecycle, verification, and hardware attestation.
type MTLSHandler struct {
	ca       *EphemeralCA
	verifier *CertVerifier
}

// NewMTLSHandler constructs the HTTP routing handler for /api/mtls/* endpoints.
func NewMTLSHandler(ca *EphemeralCA, verifier *CertVerifier) *MTLSHandler {
	return &MTLSHandler{
		ca:       ca,
		verifier: verifier,
	}
}

// ServeHTTP dispatches inbound REST requests under /api/mtls/*.
func (h *MTLSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mtls")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "status" && r.Method == http.MethodGet:
		h.handleStatus(w, r)
	case path == "ca/cert" && r.Method == http.MethodGet:
		h.handleCACert(w, r)
	case path == "ca/issue" && r.Method == http.MethodPost:
		h.handleIssue(w, r)
	case path == "verify" && r.Method == http.MethodPost:
		h.handleVerify(w, r)
	case path == "revoke" && r.Method == http.MethodPost:
		h.handleRevoke(w, r)
	case path == "tpm/attest" && r.Method == http.MethodGet:
		h.handleTPMAttest(w, r)
	case path == "peer-token" && r.Method == http.MethodPost:
		h.handlePeerTokenIssue(w, r)
	case path == "peer-token/verify" && r.Method == http.MethodPost:
		h.handlePeerTokenVerify(w, r)
	case path == "policy" && (r.Method == http.MethodPost || r.Method == http.MethodPut):
		h.handleUpdatePolicy(w, r)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "endpoint_not_found",
			"path":  r.URL.Path,
		})
	}
}

func (h *MTLSHandler) handleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tpmStatus := CheckTPMAttestation()
	stats := h.ca.Stats()

	resp := map[string]interface{}{
		"status":          "active",
		"ca":              stats,
		"active_policy":   h.verifier.GetPolicy(),
		"tpm_attestation": tpmStatus,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *MTLSHandler) handleCACert(w http.ResponseWriter, _ *http.Request) {
	pemBytes := h.ca.GetRootCertPEM()
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", "inline; filename=\"cf-broker-root-ca.pem\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pemBytes)
}

func (h *MTLSHandler) handleIssue(w http.ResponseWriter, r *http.Request) {
	var req IssueCertRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	validity := time.Duration(req.ValidityHours) * time.Hour
	if req.ValidityHours <= 0 {
		validity = 24 * time.Hour
	}

	resp, err := h.ca.IssueClientCert(req.CommonName, req.SANs, validity)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "cert_generation_failed",
			"message": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *MTLSHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req VerifyCertRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	var res VerificationResult
	if req.CustomPolicy != nil {
		res = h.verifier.VerifyPEM([]byte(req.CertificatePEM), *req.CustomPolicy)
	} else {
		res = h.verifier.VerifyPEM([]byte(req.CertificatePEM))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (h *MTLSHandler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	var req RevokeCertRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	if err := h.ca.RevokeCertificate(req.SerialNumber); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "revocation_failed",
			"message": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "revoked",
		"serial": req.SerialNumber,
	})
}

func (h *MTLSHandler) handleTPMAttest(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := CheckTPMAttestation()
	_ = json.NewEncoder(w).Encode(status)
}

func (h *MTLSHandler) handlePeerTokenIssue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BrokerID   string `json:"broker_id"`
		ClusterID  string `json:"cluster_id"`
		Role       string `json:"role"`
		TTLMinutes int    `json:"ttl_minutes"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	ttl := time.Duration(req.TTLMinutes) * time.Minute
	if req.TTLMinutes <= 0 {
		ttl = 60 * time.Minute
	}

	token, err := h.ca.GeneratePeeringToken(req.BrokerID, req.ClusterID, req.Role, ttl)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "token_generation_failed",
			"message": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(token)
}

func (h *MTLSHandler) handlePeerTokenVerify(w http.ResponseWriter, r *http.Request) {
	var token PeeringToken
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&token); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	valid, reason := h.ca.VerifyPeeringToken(&token)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"valid":   valid,
		"reason":  reason,
		"broker":  token.BrokerID,
		"cluster": token.ClusterID,
	})
}

func (h *MTLSHandler) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	var policy VerificationPolicy
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&policy); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}

	h.verifier.SetPolicy(policy)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "policy_updated",
		"updated_policy": h.verifier.GetPolicy(),
	})
}
