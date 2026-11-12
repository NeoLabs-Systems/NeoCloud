package keylock

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// hkdfInfo binds the derived key to its one use.
const hkdfInfo = "neocloud-lite device token v1"

// errWrongSecret means the key's hmac-secret output does not open the
// sealed token: another credential, or a key that was reset.
var errWrongSecret = errors.New("this security key does not unlock the stored token (was it reset?); sign in again")

// tokenCipher is AES-256-GCM under HKDF-SHA-256(hmac-secret output).
func tokenCipher(secret []byte) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, secret, nil, hkdfInfo, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal encrypts the token; server is the additional data, so a sealed
// token only opens for the server it was issued by. The result is
// base64(nonce || ciphertext).
func seal(secret []byte, token, server string) (string, error) {
	aead, err := tokenCipher(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(token), []byte(server))), nil
}

func unseal(secret []byte, sealed, server string) (string, error) {
	aead, err := tokenCipher(secret)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < aead.NonceSize() {
		return "", errors.New("the sealed token in the config is damaged")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(server))
	if err != nil {
		return "", errWrongSecret
	}
	return string(plain), nil
}
