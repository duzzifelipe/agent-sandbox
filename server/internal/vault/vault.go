// Package vault provides AES-256-GCM encryption and HKDF key derivation for agentsdx vault data.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"

	"github.com/duck-labs/agentsdx-shared/types"
)

// hkdfSalt is a fixed application-specific salt used in HKDF key derivation.
var hkdfSalt = []byte("agentsdx-vault-v1")

// DeriveKey derives a 32-byte AES-256 key from a master secret and a profile-specific
// context using HKDF-SHA256. Each profile name produces a unique key.
func DeriveKey(secret, profileName string) ([]byte, error) {
	if secret == "" {
		return nil, fmt.Errorf("vault secret must not be empty")
	}
	r := hkdf.New(sha256.New, []byte(secret), hkdfSalt, []byte(profileName))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("hkdf derive key: %w", err)
	}
	return key, nil
}

// Encrypt encrypts plaintext using AES-256-GCM and returns nonce || ciphertext.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts a nonce || ciphertext produced by Encrypt using AES-256-GCM.
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w", err)
	}
	return plaintext, nil
}

// vaultPath returns the path to the encrypted vault file for a given profile.
func vaultPath(dir, profileName string) string {
	return filepath.Join(dir, profileName+".vault.enc")
}

// StoreVaultData JSON-marshals data, encrypts it, and writes it to the vault directory.
func StoreVaultData(dir, profileName, secret string, data types.VaultData) error {
	plaintext, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("store vault data: marshal: %w", err)
	}
	key, err := DeriveKey(secret, profileName)
	if err != nil {
		return fmt.Errorf("store vault data: derive key: %w", err)
	}
	encrypted, err := Encrypt(key, plaintext)
	if err != nil {
		return fmt.Errorf("store vault data: encrypt: %w", err)
	}
	if err := os.WriteFile(vaultPath(dir, profileName), encrypted, 0600); err != nil {
		return fmt.Errorf("store vault data: write file: %w", err)
	}
	return nil
}

// LoadVaultData reads and decrypts an encrypted vault file, returning the VaultData.
func LoadVaultData(dir, profileName, secret string) (types.VaultData, error) {
	encrypted, err := os.ReadFile(vaultPath(dir, profileName))
	if err != nil {
		return types.VaultData{}, fmt.Errorf("load vault data: read file: %w", err)
	}
	key, err := DeriveKey(secret, profileName)
	if err != nil {
		return types.VaultData{}, fmt.Errorf("load vault data: derive key: %w", err)
	}
	plaintext, err := Decrypt(key, encrypted)
	if err != nil {
		return types.VaultData{}, fmt.Errorf("load vault data: decrypt: %w", err)
	}
	var data types.VaultData
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return types.VaultData{}, fmt.Errorf("load vault data: unmarshal: %w", err)
	}
	return data, nil
}
