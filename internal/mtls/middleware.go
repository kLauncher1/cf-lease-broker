package mtls

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ClientCertFromContext extracts verified certificate details stored in request context.
func ClientCertFromContext(ctx context.Context) (*ClientCertDetails, bool) {
	if ctx == nil {
		return nil, false
	}
	details, ok := ctx.Value(ClientCertContextKey).(*ClientCertDetails)
	return details, ok && details != nil
}

// RequireClientCert enforces mTLS certificate presence and policy compliance on inbound HTTP routes.
func RequireClientCert(verifier *CertVerifier, policyOverride ...VerificationPolicy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			policy := verifier.GetPolicy()
			if len(policyOverride) > 0 {
				policy = policyOverride[0]
			}

			cert, err := verifier.ExtractCertFromRequest(r)
			if err != nil {
				if policy.RequireClientCert {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"error":        "client_certificate_required",
						"message":      "Mutual TLS client certificate missing from connection or edge headers",
						"reason":       err.Error(),
						"evaluated_at": time.Now().UTC().Format(time.RFC3339),
					})
					return
				}
				// Optional client cert: continue without context injection
				next.ServeHTTP(w, r)
				return
			}

			result := verifier.VerifyCertificate(cert, policy)
			if !result.Valid {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error":        "client_certificate_verification_failed",
					"reason":       result.Reason,
					"certificate":  result.Certificate,
					"evaluated_at": result.EvaluatedAt.Format(time.RFC3339),
				})
				return
			}

			// Inject verified certificate metadata into request context
			ctx := context.WithValue(r.Context(), ClientCertContextKey, result.Certificate)

			// Propagate telemetry headers downstream
			if result.Certificate != nil {
				w.Header().Set("X-Client-Cert-Verified", "true")
				w.Header().Set("X-Client-Cert-CN", result.Certificate.SubjectCommonName)
				w.Header().Set("X-Client-Cert-Fingerprint", result.Certificate.FingerprintSHA256)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PeeringAuth guards inter-broker peering RPCs by verifying signed ephemeral CA tokens.
func PeeringAuth(ca *EphemeralCA) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenRaw := r.Header.Get("X-Broker-Peering-Token")
			if tokenRaw == "" {
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Peering ") {
					tokenRaw = strings.TrimPrefix(authHeader, "Peering ")
				}
			}

			if tokenRaw == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "missing_peering_token",
					"message": "Header X-Broker-Peering-Token or Authorization: Peering <json> is required",
				})
				return
			}

			var token PeeringToken
			if err := json.Unmarshal([]byte(tokenRaw), &token); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "invalid_peering_token_json",
					"message": err.Error(),
				})
				return
			}

			valid, reason := ca.VerifyPeeringToken(&token)
			if !valid {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "invalid_peering_token",
					"reason":  reason,
					"broker":  token.BrokerID,
					"cluster": token.ClusterID,
				})
				return
			}

			w.Header().Set("X-Broker-Peer-ID", token.BrokerID)
			w.Header().Set("X-Broker-Cluster-ID", token.ClusterID)
			next.ServeHTTP(w, r)
		})
	}
}
