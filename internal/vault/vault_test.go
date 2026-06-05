package vault_test

import (
	"os"
	"testing"

	"github.com/duck-labs/agentsdx/internal/types"
	"github.com/duck-labs/agentsdx/internal/vault"
)

func TestStoreAndLoad(t *testing.T) {
	dir := t.TempDir()
	secret := "test-secret"
	profile := "test-profile"

	original := types.VaultData{
		GitPrivateKey:      "git-priv",
		GitPublicKey:       "git-pub",
		VMAccessPrivateKey: "vm-priv",
		VMAccessPublicKey:  "vm-pub",
		Secrets:            map[string]string{"KEY": "VALUE"},
	}

	if err := vault.StoreVaultData(dir, profile, secret, original); err != nil {
		t.Fatalf("StoreVaultData: %v", err)
	}

	loaded, err := vault.LoadVaultData(dir, profile, secret)
	if err != nil {
		t.Fatalf("LoadVaultData: %v", err)
	}

	if loaded.GitPrivateKey != original.GitPrivateKey {
		t.Errorf("GitPrivateKey mismatch: got %q want %q", loaded.GitPrivateKey, original.GitPrivateKey)
	}
	if loaded.Secrets["KEY"] != "VALUE" {
		t.Errorf("Secrets mismatch: got %q want %q", loaded.Secrets["KEY"], "VALUE")
	}
}

func TestVaultExists(t *testing.T) {
	dir := t.TempDir()
	if vault.VaultExists(dir, "missing") {
		t.Error("VaultExists returned true for missing vault")
	}
	data := types.DefaultVaultData()
	_ = vault.StoreVaultData(dir, "present", "secret", data)
	if !vault.VaultExists(dir, "present") {
		t.Error("VaultExists returned false for existing vault")
	}
}

func TestGenerateKeyPair(t *testing.T) {
	priv, pub, err := vault.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if priv == "" || pub == "" {
		t.Error("empty key pair returned")
	}
}

func TestWrongSecret(t *testing.T) {
	dir := t.TempDir()
	_ = vault.StoreVaultData(dir, "p", "right", types.DefaultVaultData())
	_, err := vault.LoadVaultData(dir, "p", "wrong")
	if err == nil {
		t.Error("expected error with wrong secret, got nil")
	}
}

func TestDeriveKeyEmptySecret(t *testing.T) {
	_, err := vault.DeriveKey("", "profile")
	if err == nil {
		t.Error("expected error for empty secret")
	}
}

var _ = os.TempDir // ensure os is used
