package api

import (
	"testing"
)

// TestGenerateNonce tests that nonce generation produces unique values
func TestGenerateNonce(t *testing.T) {
	nonce1 := generateNonce()
	nonce2 := generateNonce()

	if nonce1 == "" {
		t.Error("generateNonce returned empty string")
	}

	if nonce2 == "" {
		t.Error("generateNonce returned empty string")
	}

	if nonce1 == nonce2 {
		t.Error("generateNonce produced duplicate nonces")
	}

	// Nonce should be base64 encoded (at least 16 chars for 128-bit)
	if len(nonce1) < 16 {
		t.Errorf("nonce too short: %d chars", len(nonce1))
	}
}

// TestGenerateNonceNotHardcoded tests that nonce is not the old hardcoded value
func TestGenerateNonceNotHardcoded(t *testing.T) {
	nonce := generateNonce()
	if nonce == "wayback-fix-positioning" {
		t.Error("nonce is still hardcoded to 'wayback-fix-positioning'")
	}
}
