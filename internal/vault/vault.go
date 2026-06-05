package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/ssh"

	"github.com/duck-labs/agentsdx/internal/types"
)

var hkdfSalt = []byte("agentsdx-vault-v1")

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
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w", err)
	}
	return plaintext, nil
}

func vaultPath(dir, profileName string) string {
	return filepath.Join(dir, profileName+".vault.enc")
}

func VaultExists(dir, profileName string) bool {
	_, err := os.Stat(vaultPath(dir, profileName))
	return err == nil
}

func GenerateKeyPair() (privateKeyPEM, publicKeyOpenSSH string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate ed25519 key: %w", err)
	}
	privBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", "", fmt.Errorf("marshal private key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", fmt.Errorf("new ssh public key: %w", err)
	}
	return string(pem.EncodeToMemory(privBlock)), string(ssh.MarshalAuthorizedKey(sshPub)), nil
}

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
