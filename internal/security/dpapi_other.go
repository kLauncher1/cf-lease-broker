//go:build !windows

package security

import "errors"

// dpapiEncrypt returns an error on non-Windows platforms.
func dpapiEncrypt(plaintext []byte) ([]byte, error) {
	return nil, errors.New("Windows DPAPI is not available on non-Windows operating systems")
}

// dpapiDecrypt returns an error on non-Windows platforms.
func dpapiDecrypt(ciphertext []byte) ([]byte, error) {
	return nil, errors.New("Windows DPAPI is not available on non-Windows operating systems")
}

// isDPAPIAvailable returns false on non-Windows platforms.
func isDPAPIAvailable() bool {
	return false
}
