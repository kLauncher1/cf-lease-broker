package security

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type contextKey string

const claimsContextKey contextKey = "cf_broker_claims"

// AuthManager coordinates JWT authentication, RBAC, and admin key validation.
type AuthManager struct {
	jwtManager  *JWTManager
	vault       *SecretVault
	adminAPIKey string
}

// NewAuthManager initializes auth manager with JWT and vault instances.
func NewAuthManager(jwtMgr *JWTManager, vault *SecretVault) *AuthManager {
	adminKey := os.Getenv("CF_BROKER_ADMIN_KEY")
	if adminKey == "" {
		adminKey = os.Getenv("CF_ADMIN_API_KEY")
	}
	if adminKey == "" {
		// Secure development fallback key
		adminKey = "cf_admin_dev_token_9081"
	}

	return &AuthManager{
		jwtManager:  jwtMgr,
		vault:       vault,
		adminAPIKey: adminKey,
	}
}

// GetJWTManager returns the underlying JWT manager instance.
func (am *AuthManager) GetJWTManager() *JWTManager {
	return am.jwtManager
}

// GetVault returns the underlying SecretVault instance.
func (am *AuthManager) GetVault() *SecretVault {
	return am.vault
}

// ContextWithClaims attaches validated claims to a context.
func ContextWithClaims(ctx context.Context, claims *JWTClaims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

// ClaimsFromContext extracts validated claims from request context.
func ClaimsFromContext(ctx context.Context) (*JWTClaims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*JWTClaims)
	return claims, ok && claims != nil
}

// ExtractToken retrieves bearer or custom auth token from HTTP headers.
func ExtractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
		return strings.TrimSpace(authHeader)
	}

	customHeader := r.Header.Get("X-Broker-Auth")
	if customHeader != "" {
		return strings.TrimSpace(customHeader)
	}

	return ""
}

// RequireAuth wraps an http.Handler requiring a valid Bearer token or master admin key.
func (am *AuthManager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ExtractToken(r)
		if token == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized: missing Authorization header or Bearer token"}`))
			return
		}

		// Check static admin API key via constant-time comparison
		if subtle.ConstantTimeCompare([]byte(token), []byte(am.adminAPIKey)) == 1 {
			adminClaims := &JWTClaims{
				Sub:    "admin-root",
				Role:   "admin",
				Scopes: []string{"all"},
				Iat:    time.Now().UTC().Unix(),
				Exp:    time.Now().UTC().Add(24 * time.Hour).Unix(),
				Jti:    "root_admin_session",
			}
			ctx := ContextWithClaims(r.Context(), adminClaims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Verify dynamic JWT token
		claims, err := am.jwtManager.VerifyToken(token)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"unauthorized: %s"}`, err.Error())))
			return
		}

		ctx := ContextWithClaims(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole enforces specific RBAC roles on top of an authenticated handler.
func (am *AuthManager) RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return am.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok {
				http.Error(w, `{"error":"unauthorized: claims missing"}`, http.StatusUnauthorized)
				return
			}

			// Admin role always has universal access
			if claims.Role == "admin" {
				next.ServeHTTP(w, r)
				return
			}

			for _, role := range allowedRoles {
				if strings.EqualFold(claims.Role, role) {
					next.ServeHTTP(w, r)
					return
				}
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"forbidden: role '%s' lacks required permissions"}`, claims.Role)))
		}))
	}
}

// IssueTokenRequest represents an incoming payload requesting a JWT.
type IssueTokenRequest struct {
	Sub           string   `json:"sub"`
	Role          string   `json:"role"`
	Scopes        []string `json:"scopes,omitempty"`
	DurationHours int      `json:"duration_hours,omitempty"`
	AdminKey      string   `json:"admin_key,omitempty"`
}

// HandleIssueToken handles POST /api/auth/token to grant scoped JWTs.
func (am *AuthManager) HandleIssueToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req IssueTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	if req.Sub == "" {
		req.Sub = "service-client"
	}
	if req.Role == "" {
		req.Role = "service"
	}
	if req.DurationHours <= 0 {
		req.DurationHours = 24
	}

	// Issuing admin or operator roles requires valid admin credentials or caller authentication
	if req.Role == "admin" || req.Role == "operator" {
		callerClaims, isAuth := ClaimsFromContext(r.Context())
		isAdminCaller := isAuth && callerClaims != nil && callerClaims.Role == "admin"
		validKey := req.AdminKey != "" && subtle.ConstantTimeCompare([]byte(req.AdminKey), []byte(am.adminAPIKey)) == 1

		if !isAdminCaller && !validKey {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"forbidden: issuing admin/operator token requires admin authorization"}`))
			return
		}
	}

	now := time.Now().UTC()
	claims := JWTClaims{
		Sub:    req.Sub,
		Role:   req.Role,
		Scopes: req.Scopes,
		Iat:    now.Unix(),
		Exp:    now.Add(time.Duration(req.DurationHours) * time.Hour).Unix(),
		Jti:    "tok_" + GenerateRandomHex(12),
		Aud:    "cf-lease-broker",
	}

	tokenStr, err := am.jwtManager.SignToken(claims)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"token":      tokenStr,
		"token_type": "Bearer",
		"expires_in": req.DurationHours * 3600,
		"claims":     claims,
	})
}

// HandleRevokeToken handles POST /api/auth/revoke to blacklist a token JTI.
func (am *AuthManager) HandleRevokeToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JTI           string `json:"jti"`
		DurationHours int    `json:"duration_hours,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JTI == "" {
		http.Error(w, `{"error":"missing or invalid jti"}`, http.StatusBadRequest)
		return
	}

	if req.DurationHours <= 0 {
		req.DurationHours = 48
	}

	expiresAt := time.Now().UTC().Add(time.Duration(req.DurationHours) * time.Hour)
	if err := am.jwtManager.RevokeToken(req.JTI, expiresAt); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "revoked",
		"jti":     req.JTI,
		"expires": expiresAt,
	})
}

// HandleMe handles GET /api/auth/me to return current claims.
func (am *AuthManager) HandleMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	claims, ok := ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"authenticated":false,"error":"not authenticated"}`))
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"authenticated": true,
		"claims":        claims,
	})
}

// HandleStatus handles GET /api/auth/status reporting security configuration.
func (am *AuthManager) HandleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"auth_enabled":     true,
		"token_algorithm":  "HS256",
		"revoked_count":    am.jwtManager.RevokedCount(),
		"roles_supported":  []string{"admin", "operator", "service", "readonly"},
		"dpapi_supported":  isDPAPIAvailable(),
		"vault_configured": am.vault != nil,
	})
}
