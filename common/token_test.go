package common

import (
	"encoding/base64"
	"errors"
	"net/netip"
	"testing"
)

func TestNewTokenCipher_ValidSecret(t *testing.T) {
	tests := []string{
		base64.StdEncoding.EncodeToString([]byte("16-byte-key-A!@#")),      // 16 bytes
		base64.StdEncoding.EncodeToString([]byte("24-byte-key-rightTHERE!X")),  // 24 bytes
		base64.StdEncoding.EncodeToString([]byte("32-byte-key-exactly-right-HERE!1")), // 32 bytes
	}
	for _, secret := range tests {
		c, err := NewTokenCipher(secret)
		if err != nil {
			t.Errorf("NewTokenCipher(%q) unexpected error: %v", secret, err)
		}
		if c == nil {
			t.Errorf("NewTokenCipher(%q) returned nil cipher", secret)
		}
	}
}

func TestNewTokenCipher_InvalidBase64(t *testing.T) {
	_, err := NewTokenCipher("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestNewTokenCipher_InvalidKeySize(t *testing.T) {
	_, err := NewTokenCipher(base64.StdEncoding.EncodeToString([]byte("short")))
	if err == nil {
		t.Fatal("expected error for invalid key size")
	}
}

func TestTokenCipher_RoundTrip_IPv4(t *testing.T) {
	c := mustCipher(t)
	original := netip.MustParseAddr("10.0.0.1")
	token := c.Encode(original)
	decoded, err := c.Decode(token)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if decoded != original {
		t.Errorf("round-trip failed: got %v, want %v", decoded, original)
	}
}

func TestTokenCipher_RoundTrip_IPv6(t *testing.T) {
	c := mustCipher(t)
	original := netip.MustParseAddr("2001:db8::1")
	token := c.Encode(original)
	decoded, err := c.Decode(token)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if decoded != original {
		t.Errorf("round-trip failed: got %v, want %v", decoded, original)
	}
}

func TestTokenCipher_RandomNonce(t *testing.T) {
	c := mustCipher(t)
	ip := netip.MustParseAddr("192.168.1.1")
	t1 := c.Encode(ip)
	t2 := c.Encode(ip)
	if t1 == t2 {
		t.Error("two encodes of same IP should produce different tokens (random nonce)")
	}
}

func TestTokenCipher_Decode_InvalidBase64(t *testing.T) {
	c := mustCipher(t)
	_, err := c.Decode("!@#$not base64")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrorBase64) {
		t.Errorf("error should wrap ErrorBase64, got: %v", err)
	}
}

func TestTokenCipher_Decode_TamperedToken(t *testing.T) {
	c := mustCipher(t)
	ip := netip.MustParseAddr("10.0.0.5")
	token := c.Encode(ip)
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("failed to decode token: %v", err)
	}
	b[len(b)-1] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(b)
	_, err = c.Decode(tampered)
	if err == nil {
		t.Fatal("expected error for tampered token")
	}
	if !errors.Is(err, ErrorCipher) {
		t.Errorf("error should wrap ErrorCipher, got: %v", err)
	}
}

func TestTokenCipher_Decode_WrongKey(t *testing.T) {
	c1 := mustCipherWithSecret(t, "16-byte-key-A!@#")
	c2 := mustCipherWithSecret(t, "other-16byte-key")
	ip := netip.MustParseAddr("10.0.0.5")
	token := c1.Encode(ip)
	_, err := c2.Decode(token)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key")
	}
	if !errors.Is(err, ErrorCipher) {
		t.Errorf("error should wrap ErrorCipher, got: %v", err)
	}
}

func TestTokenCipher_Decode_EmptyToken(t *testing.T) {
	c := mustCipher(t)
	_, err := c.Decode("")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestTokenCipher_TokenFormat(t *testing.T) {
	c := mustCipher(t)
	ip := netip.MustParseAddr("10.0.0.1")
	token := c.Encode(ip)
	for _, ch := range token {
		if ch == '+' || ch == '/' || ch == '=' {
			t.Errorf("token should use raw-URL encoding (no padding), got char %q in %q", ch, token)
		}
	}
}

func mustCipher(t *testing.T) TokenCipher {
	t.Helper()
	return mustCipherWithSecret(t, "16-byte-key-here")
}

func mustCipherWithSecret(t *testing.T, key string) TokenCipher {
	t.Helper()
	secret := base64.StdEncoding.EncodeToString([]byte(key))
	c, err := NewTokenCipher(secret)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}
	return c
}
