package vault_test

import (
	"bytes"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vault"
)

func TestDeriveKey_Deterministic(t *testing.T) {
	key1 := vault.DeriveKey("mysecret", "profile-a")
	key2 := vault.DeriveKey("mysecret", "profile-a")
	if len(key1) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key1))
	}
	if !bytes.Equal(key1, key2) {
		t.Fatal("expected same inputs to produce the same key")
	}
}

func TestDeriveKey_ProfileIsolation(t *testing.T) {
	key1 := vault.DeriveKey("mysecret", "profile-a")
	key2 := vault.DeriveKey("mysecret", "profile-b")
	if bytes.Equal(key1, key2) {
		t.Fatal("expected different profile names to produce different keys")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := vault.DeriveKey("mysecret", "profile-a")
	plaintext := []byte("hello, agentsdx vault!")

	ciphertext, err := vault.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := vault.Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestEncrypt_ProducesRandomCiphertexts(t *testing.T) {
	key := vault.DeriveKey("mysecret", "profile-a")
	plaintext := []byte("same plaintext every time")

	ct1, err := vault.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt 1: %v", err)
	}
	ct2, err := vault.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt 2: %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Fatal("expected different ciphertexts for the same plaintext due to random nonces")
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	key := vault.DeriveKey("mysecret", "profile-a")
	plaintext := []byte("tamper me")

	ciphertext, err := vault.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Flip a byte in the ciphertext portion (after the 12-byte nonce)
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[12] ^= 0xFF

	_, err = vault.Decrypt(key, tampered)
	if err == nil {
		t.Fatal("expected an error when decrypting tampered ciphertext")
	}
}
