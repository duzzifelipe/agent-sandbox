package vault_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"testing"

	"github.com/duck-labs/agentsdx-server/internal/vault"
	"github.com/duck-labs/agentsdx-shared/types"
)

func TestDeriveKey_Deterministic(t *testing.T) {
	key1, err := vault.DeriveKey("mysecret", "profile-a")
	if err != nil {
		t.Fatal(err)
	}
	key2, err := vault.DeriveKey("mysecret", "profile-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(key1) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key1))
	}
	if !bytes.Equal(key1, key2) {
		t.Fatal("expected same inputs to produce the same key")
	}
}

func TestDeriveKey_ProfileIsolation(t *testing.T) {
	key1, err := vault.DeriveKey("mysecret", "profile-a")
	if err != nil {
		t.Fatal(err)
	}
	key2, err := vault.DeriveKey("mysecret", "profile-b")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(key1, key2) {
		t.Fatal("expected different profile names to produce different keys")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key, err := vault.DeriveKey("mysecret", "profile-a")
	if err != nil {
		t.Fatal(err)
	}
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
	key, err := vault.DeriveKey("mysecret", "profile-a")
	if err != nil {
		t.Fatal(err)
	}
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
	key, err := vault.DeriveKey("secret", "profile")
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("sensitive data")
	ct, err := vault.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Determine nonce size from the cipher
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonceSize := gcm.NonceSize()

	tampered := make([]byte, len(ct))
	copy(tampered, ct)
	tampered[nonceSize] ^= 0xFF // flip first byte of ciphertext body

	_, err = vault.Decrypt(key, tampered)
	if err == nil {
		t.Fatal("expected error decrypting tampered ciphertext")
	}
}

func TestDecrypt_ShortInput(t *testing.T) {
	key, err := vault.DeriveKey("secret", "profile")
	if err != nil {
		t.Fatal(err)
	}

	_, err = vault.Decrypt(key, []byte{})
	if err == nil {
		t.Fatal("expected error for empty ciphertext")
	}

	_, err = vault.Decrypt(key, []byte("short"))
	if err == nil {
		t.Fatal("expected error for ciphertext shorter than nonce")
	}
}

func TestStoreAndLoadVaultData_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	secret := "test-secret-roundtrip"
	profile := "test-profile"

	original := types.VaultData{
		GitPrivateKey:      "git-priv-key",
		GitPublicKey:       "git-pub-key",
		VMAccessPrivateKey: "vm-priv-key",
		VMAccessPublicKey:  "vm-pub-key",
		AgentStatePaths:    []string{".claude/", ".claude.json"},
	}

	if err := vault.StoreVaultData(dir, profile, secret, original); err != nil {
		t.Fatalf("store: %v", err)
	}

	loaded, err := vault.LoadVaultData(dir, profile, secret)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.GitPrivateKey != original.GitPrivateKey {
		t.Errorf("GitPrivateKey: got %q, want %q", loaded.GitPrivateKey, original.GitPrivateKey)
	}
	if loaded.GitPublicKey != original.GitPublicKey {
		t.Errorf("GitPublicKey: got %q, want %q", loaded.GitPublicKey, original.GitPublicKey)
	}
	if loaded.VMAccessPrivateKey != original.VMAccessPrivateKey {
		t.Errorf("VMAccessPrivateKey: got %q, want %q", loaded.VMAccessPrivateKey, original.VMAccessPrivateKey)
	}
	if loaded.VMAccessPublicKey != original.VMAccessPublicKey {
		t.Errorf("VMAccessPublicKey: got %q, want %q", loaded.VMAccessPublicKey, original.VMAccessPublicKey)
	}
	if len(loaded.AgentStatePaths) != len(original.AgentStatePaths) {
		t.Fatalf("AgentStatePaths length: got %d, want %d", len(loaded.AgentStatePaths), len(original.AgentStatePaths))
	}
	for i, p := range original.AgentStatePaths {
		if loaded.AgentStatePaths[i] != p {
			t.Errorf("AgentStatePaths[%d]: got %q, want %q", i, loaded.AgentStatePaths[i], p)
		}
	}
}

func TestLoadVaultData_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := vault.LoadVaultData(dir, "nonexistent-profile", "some-secret")
	if err == nil {
		t.Fatal("expected error loading non-existent vault file")
	}
}

func TestLoadVaultData_WrongSecret(t *testing.T) {
	dir := t.TempDir()
	profile := "test-profile"
	data := types.DefaultVaultData()

	if err := vault.StoreVaultData(dir, profile, "correct-secret", data); err != nil {
		t.Fatalf("store: %v", err)
	}

	_, err := vault.LoadVaultData(dir, profile, "wrong-secret")
	if err == nil {
		t.Fatal("expected error loading vault with wrong secret")
	}
}
