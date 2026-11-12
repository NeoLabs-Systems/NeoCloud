package fido

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
)

// PIN/UV auth protocol 1 (CTAP 2.1 §6.5.6), the one every CTAP2 key
// supports. hmac-secret uses it for its salts and output as well.

// agree runs ECDH between the platform key and the key's COSE key. The
// shared secret is SHA-256 of the x-coordinate of the shared point; the
// platform's public key goes back to the key as a COSE key.
func agree(platform *ecdh.PrivateKey, peer coseKey) (coseKey, []byte, error) {
	if len(peer.X) != 32 || len(peer.Y) != 32 {
		return coseKey{}, nil, errors.New("fido: malformed key agreement key")
	}
	point := append(append([]byte{0x04}, peer.X...), peer.Y...)
	peerKey, err := ecdh.P256().NewPublicKey(point)
	if err != nil {
		return coseKey{}, nil, err
	}
	z, err := platform.ECDH(peerKey)
	if err != nil {
		return coseKey{}, nil, err
	}
	shared := sha256.Sum256(z)
	public := platform.PublicKey().Bytes() // 0x04 || X || Y
	return coseKey{Kty: 2, Alg: -25, Crv: 1, X: public[1:33], Y: public[33:]}, shared[:], nil
}

// encrypt is AES-256-CBC with an all-zero IV and no padding; plaintexts
// are whole blocks (PIN hashes, salts, tokens).
func encrypt(key, plain []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil || len(plain)%aes.BlockSize != 0 {
		panic("fido: encrypt needs a 32-byte key and whole blocks")
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(out, plain)
	return out
}

func decrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("fido: encrypted data is not whole blocks")
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(out, data)
	return out, nil
}

// authenticate is the first 16 bytes of HMAC-SHA-256(key, message).
func authenticate(key, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	return mac.Sum(nil)[:16]
}
