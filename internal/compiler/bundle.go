package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	// WasmMagicHeader defines the standard WebAssembly binary format magic bytes (\x00asm).
	WasmMagicHeader = []byte{0x00, 0x61, 0x73, 0x6d}
	// WasmVersionOne defines the standard WebAssembly version 1 header.
	WasmVersionOne = []byte{0x01, 0x00, 0x00, 0x00}

	ErrEmptyScript       = errors.New("worker script cannot be empty")
	ErrInvalidWasmHeader = errors.New("invalid WebAssembly binary: header mismatch")
	ErrBundleNotFound    = errors.New("worker bundle not found")
	ErrDuplicateBundle   = errors.New("worker bundle with this name already exists for tenant")
)

// BundleConfig specifies packaging and minification parameters.
type BundleConfig struct {
	TenantID          string            `json:"tenant_id"`
	Name              string            `json:"name"`
	Minify            bool              `json:"minify"`
	CompatibilityDate string            `json:"compatibility_date"`
	MemoryLimitMB     int               `json:"memory_limit_mb"`
	CPUTimeLimitMs    int               `json:"cpu_time_limit_ms"`
	EnvBindings       map[string]string `json:"env_bindings,omitempty"`
}

// WorkerBundle represents a packaged and validated Edge Worker deployment bundle.
type WorkerBundle struct {
	ID                string            `json:"id"`
	TenantID          string            `json:"tenant_id"`
	Name              string            `json:"name"`
	EntryScript       string            `json:"entry_script"`
	WasmModules       map[string]string `json:"wasm_modules,omitempty"` // module name -> hex encoded binary
	EnvBindings       map[string]string `json:"env_bindings,omitempty"`
	CompatibilityDate string            `json:"compatibility_date"`
	MemoryLimitMB     int               `json:"memory_limit_mb"`
	CPUTimeLimitMs    int               `json:"cpu_time_limit_ms"`
	ContentHash       string            `json:"content_hash"`
	OriginalBytes     int               `json:"original_bytes"`
	MinifiedBytes     int               `json:"minified_bytes"`
	HasValidExport    bool              `json:"has_valid_export"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// MinifyJavaScript strips comments, eliminates redundant whitespace, and normalizes delimiters.
func MinifyJavaScript(src []byte) ([]byte, bool) {
	if len(src) == 0 {
		return []byte{}, false
	}

	var buf bytes.Buffer
	buf.Grow(len(src))

	inSingleComment := false
	inMultiComment := false
	inString := byte(0) // '\'', '"', or '`'
	escaped := false

	n := len(src)
	for i := 0; i < n; i++ {
		ch := src[i]

		// Handle string escape sequences
		if inString != 0 {
			buf.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == inString {
				inString = 0
			}
			continue
		}

		// Handle comments
		if inSingleComment {
			if ch == '\n' || ch == '\r' {
				inSingleComment = false
				buf.WriteByte('\n')
			}
			continue
		}

		if inMultiComment {
			if ch == '*' && i+1 < n && src[i+1] == '/' {
				inMultiComment = false
				i++ // skip '/'
			}
			continue
		}

		// Check for comment starts
		if ch == '/' && i+1 < n {
			next := src[i+1]
			if next == '/' {
				inSingleComment = true
				i++
				continue
			} else if next == '*' {
				inMultiComment = true
				i++
				continue
			}
		}

		// Check for string starts
		if ch == '\'' || ch == '"' || ch == '`' {
			inString = ch
			escaped = false
			buf.WriteByte(ch)
			continue
		}

		// Whitespace collapsing
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			// Write single space only if previous character wasn't whitespace or operator
			if buf.Len() > 0 {
				last := buf.Bytes()[buf.Len()-1]
				if !isOperatorOrDelimiter(last) && last != ' ' && last != '\n' {
					buf.WriteByte(' ')
				}
			}
			continue
		}

		// Eliminate space preceding operators or delimiters
		if isOperatorOrDelimiter(ch) && buf.Len() > 0 && buf.Bytes()[buf.Len()-1] == ' ' {
			trimmed := buf.Bytes()[:buf.Len()-1]
			buf.Reset()
			buf.Write(trimmed)
		}

		buf.WriteByte(ch)
	}

	minified := bytes.TrimSpace(buf.Bytes())
	hasExport := detectWorkerExports(minified)
	return minified, hasExport
}

func isOperatorOrDelimiter(ch byte) bool {
	switch ch {
	case '=', '+', '-', '*', '/', '%', '!', '&', '|', '^', '~', '<', '>',
		'?', ':', '{', '}', '(', ')', '[', ']', ';', ',':
		return true
	default:
		return false
	}
}

func detectWorkerExports(src []byte) bool {
	s := string(src)
	patterns := []string{
		"export default",
		"addEventListener('fetch'",
		"addEventListener(\"fetch\"",
		"export async function fetch",
		"export function fetch",
		"module.exports",
	}
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// ValidateWasmModule checks whether the provided byte slice is a valid WebAssembly binary.
func ValidateWasmModule(data []byte) error {
	if len(data) < 8 {
		return ErrInvalidWasmHeader
	}
	if !bytes.Equal(data[:4], WasmMagicHeader) {
		return ErrInvalidWasmHeader
	}
	if !bytes.Equal(data[4:8], WasmVersionOne) {
		return fmt.Errorf("%w: expected v1 (0x01000000), got %v", ErrInvalidWasmHeader, data[4:8])
	}
	return nil
}

// BundleManager provides thread-safe lifecycle management for packaged Edge Worker bundles.
type BundleManager struct {
	mu      sync.RWMutex
	bundles map[string]*WorkerBundle // bundle ID -> WorkerBundle
}

// NewBundleManager instantiates a clean in-memory bundle repository.
func NewBundleManager() *BundleManager {
	return &BundleManager{
		bundles: make(map[string]*WorkerBundle),
	}
}

// PackageWorker compiles, minifies, validates, and packages an Edge Worker bundle.
func (m *BundleManager) PackageWorker(cfg BundleConfig, script []byte, wasmModules map[string][]byte) (*WorkerBundle, error) {
	if len(script) == 0 {
		return nil, ErrEmptyScript
	}
	if cfg.TenantID == "" {
		cfg.TenantID = "tenant-default"
	}
	if cfg.Name == "" {
		cfg.Name = fmt.Sprintf("worker-%d", time.Now().UnixNano()%10000)
	}
	if cfg.CompatibilityDate == "" {
		cfg.CompatibilityDate = time.Now().Format("2006-01-02")
	}
	if cfg.MemoryLimitMB <= 0 {
		cfg.MemoryLimitMB = 128
	}
	if cfg.CPUTimeLimitMs <= 0 {
		cfg.CPUTimeLimitMs = 50
	}

	origLen := len(script)
	var processedScript []byte
	var hasValidExport bool

	if cfg.Minify {
		processedScript, hasValidExport = MinifyJavaScript(script)
	} else {
		processedScript = script
		hasValidExport = detectWorkerExports(script)
	}

	// Validate any attached WebAssembly modules
	encodedWasm := make(map[string]string)
	for name, wasmData := range wasmModules {
		if err := ValidateWasmModule(wasmData); err != nil {
			return nil, fmt.Errorf("wasm module %q validation failed: %w", name, err)
		}
		encodedWasm[name] = hex.EncodeToString(wasmData)
	}

	hasher := sha256.New()
	hasher.Write(processedScript)
	for name, wasmHex := range encodedWasm {
		hasher.Write([]byte(name))
		hasher.Write([]byte(wasmHex))
	}
	contentHash := hex.EncodeToString(hasher.Sum(nil))

	bundleID := fmt.Sprintf("bundle-%s-%s", cfg.TenantID, contentHash[:12])

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check duplicate name for tenant
	for _, b := range m.bundles {
		if b.TenantID == cfg.TenantID && b.Name == cfg.Name {
			return nil, ErrDuplicateBundle
		}
	}

	now := time.Now().UTC()
	bundle := &WorkerBundle{
		ID:                bundleID,
		TenantID:          cfg.TenantID,
		Name:              cfg.Name,
		EntryScript:       string(processedScript),
		WasmModules:       encodedWasm,
		EnvBindings:       cfg.EnvBindings,
		CompatibilityDate: cfg.CompatibilityDate,
		MemoryLimitMB:     cfg.MemoryLimitMB,
		CPUTimeLimitMs:    cfg.CPUTimeLimitMs,
		ContentHash:       contentHash,
		OriginalBytes:     origLen,
		MinifiedBytes:     len(processedScript),
		HasValidExport:    hasValidExport,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	m.bundles[bundle.ID] = bundle
	return bundle, nil
}

// GetBundle retrieves a bundle by its unique ID.
func (m *BundleManager) GetBundle(id string) (*WorkerBundle, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, exists := m.bundles[id]
	if !exists {
		return nil, false
	}
	cp := *b
	return &cp, true
}

// ListBundles returns a snapshot of all registered worker bundles.
func (m *BundleManager) ListBundles() []*WorkerBundle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*WorkerBundle, 0, len(m.bundles))
	for _, b := range m.bundles {
		cp := *b
		out = append(out, &cp)
	}
	return out
}

// UndeployBundle removes a bundle from active deployment.
func (m *BundleManager) UndeployBundle(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.bundles[id]; !exists {
		return ErrBundleNotFound
	}
	delete(m.bundles, id)
	return nil
}
