package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	wrapPrefix   = "v1:"
	secretPrefix = "enc:v1:"
	keyLength    = 32
	ivLength     = 12
	tagLength    = 16
)

var (
	ErrInvalidMasterKey  = errors.New("NEOCLOUD_MASTER_KEY is missing or malformed.")
	ErrInvalidCiphertext = errors.New("encrypted value is malformed or failed authentication")
)

// Keyring derives the server-side keys from the install's NEOCLOUD_MASTER_KEY.
// The value is copied into fixed-size storage and is never exposed by a getter.
type Keyring struct {
	master [keyLength]byte
}

// NewKeyring validates a 32-byte hexadecimal master key.
func NewKeyring(masterKeyHex string) (*Keyring, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(masterKeyHex))
	if err != nil || len(decoded) != keyLength {
		return nil, ErrInvalidMasterKey
	}
	k := &Keyring{}
	copy(k.master[:], decoded)
	return k, nil
}

// DeriveKey returns the 32-byte key for one established NeoCloud purpose.
// Purpose names are part of the on-disk format and must remain stable.
func (k *Keyring) DeriveKey(purpose string) ([]byte, error) {
	return hkdf.Key(sha256.New, k.master[:], nil, purpose, keyLength)
}

// SealBytes encrypts plaintext as IV | tag | ciphertext, the Node server's
// AES-256-GCM representation. aad must be identical at seal and open time.
func SealBytes(key, plaintext, aad []byte) ([]byte, error) {
	if len(key) != keyLength {
		return nil, fmt.Errorf("cryptox: AES-256 key must be %d bytes", keyLength)
	}
	iv := make([]byte, ivLength)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, fmt.Errorf("cryptox: generate IV: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	sealed := aead.Seal(nil, iv, plaintext, aad) // ciphertext | tag
	out := make([]byte, 0, ivLength+len(sealed))
	out = append(out, iv...)
	out = append(out, sealed[len(sealed)-tagLength:]...)
	out = append(out, sealed[:len(sealed)-tagLength]...)
	return out, nil
}

// OpenBytes opens the Node server's IV | tag | ciphertext representation.
func OpenBytes(key, raw, aad []byte) ([]byte, error) {
	if len(key) != keyLength || len(raw) < ivLength+tagLength {
		return nil, ErrInvalidCiphertext
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidCiphertext
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrInvalidCiphertext
	}
	iv := raw[:ivLength]
	tag := raw[ivLength : ivLength+tagLength]
	ciphertext := raw[ivLength+tagLength:]
	sealed := make([]byte, 0, len(ciphertext)+tagLength)
	sealed = append(sealed, ciphertext...)
	sealed = append(sealed, tag...)
	plain, err := aead.Open(nil, iv, sealed, aad)
	if err != nil {
		return nil, ErrInvalidCiphertext
	}
	return plain, nil
}

// WrapDEK encrypts an account data key with account-bound AAD. The returned
// string can be stored directly in users.wrapped_dek.
func (k *Keyring) WrapDEK(userID int64, dek []byte) (string, error) {
	if len(dek) != keyLength {
		return "", fmt.Errorf("cryptox: account data key must be %d bytes", keyLength)
	}
	key, err := k.DeriveKey("neocloud:dek-wrap")
	if err != nil {
		return "", err
	}
	sealed, err := SealBytes(key, dek, []byte("user:"+strconv.FormatInt(userID, 10)))
	if err != nil {
		return "", err
	}
	return wrapPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// UnwrapDEK opens a users.wrapped_dek value and verifies it is bound to userID.
func (k *Keyring) UnwrapDEK(userID int64, wrapped string) ([]byte, error) {
	if !strings.HasPrefix(wrapped, wrapPrefix) {
		return nil, fmt.Errorf("cryptox: data key of account %d has an unknown format", userID)
	}
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(wrapped, wrapPrefix))
	if err != nil {
		return nil, ErrInvalidCiphertext
	}
	key, err := k.DeriveKey("neocloud:dek-wrap")
	if err != nil {
		return nil, err
	}
	dek, err := OpenBytes(key, sealed, []byte("user:"+strconv.FormatInt(userID, 10)))
	if err != nil || len(dek) != keyLength {
		return nil, ErrInvalidCiphertext
	}
	return dek, nil
}

// EncryptValue seals a small server-side secret using the existing enc:v1
// storage representation.
func (k *Keyring) EncryptValue(value string) (string, error) {
	key, err := k.DeriveKey("neocloud:secrets")
	if err != nil {
		return "", err
	}
	sealed, err := SealBytes(key, []byte(value), nil)
	if err != nil {
		return "", err
	}
	return secretPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptValue opens a server-side secret stored by the Node implementation.
func (k *Keyring) DecryptValue(value string) (string, error) {
	if !strings.HasPrefix(value, secretPrefix) {
		return "", errors.New("Value is not encrypted.")
	}
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, secretPrefix))
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	key, err := k.DeriveKey("neocloud:secrets")
	if err != nil {
		return "", err
	}
	plain, err := OpenBytes(key, sealed, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// BackupKey is the key used for encrypted database snapshots.
func (k *Keyring) BackupKey() ([]byte, error) {
	return k.DeriveKey("neocloud:backup")
}
