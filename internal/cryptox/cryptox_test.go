package cryptox

import (
	"bytes"
	"encoding/hex"
	"io"
	"strconv"
	"testing"
)

func TestNodeWrappedDataKeyCompatibility(t *testing.T) {
	keyring, err := NewKeyring("0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	dek, err := keyring.UnwrapDEK(42, "v1:oKGio6SlpqeoqaqrEV7xsLu7sqEbIyhU93JrnX1UIKyUxjjHOCyA8VcflC9RnTTacJ6/sZM+H6dPB5Qe")
	if err != nil {
		t.Fatal(err)
	}
	want := make([]byte, keyLength)
	for i := range want {
		want[i] = byte(i)
	}
	if !bytes.Equal(dek, want) {
		t.Fatalf("unwrapped DEK = %x, want %x", dek, want)
	}
	if _, err := keyring.UnwrapDEK(43, "v1:oKGio6SlpqeoqaqrEV7xsLu7sqEbIyhU93JrnX1UIKyUxjjHOCyA8VcflC9RnTTacJ6/sZM+H6dPB5Qe"); err == nil {
		t.Fatal("accepted account key wrapped for a different account")
	}
	derived, err := keyring.DeriveKey("neocloud:secrets")
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(derived); got != "9e4c94daf5a4dde68dfbad5397475ae8970a0d4ff7ad8c9d56d0dfd478850d5f" {
		t.Fatalf("secrets key = %s", got)
	}
}

func TestNodeSegmentedBlobCompatibility(t *testing.T) {
	// Produced by server/services/crypto/stream_cipher.js with fixed nonce
	// prefix 0102030405060708, blob id "blob-test-42", and DEK 00..1f.
	fixture, err := hex.DecodeString("4e43423101000000000100000102030405060708cfda21b996acb8197f3c01daae136c8c78753fbcfa257fce5f36b30ec0ed370fac060cc9a7b07dcce8")
	if err != nil {
		t.Fatal(err)
	}
	dek := make([]byte, keyLength)
	for i := range dek {
		dek[i] = byte(i)
	}
	want := []byte("NeoCloud Go matches Node.")
	var plain bytes.Buffer
	if _, err := DecryptBlobRange(bytes.NewReader(fixture), int64(len(want)), 0, int64(len(want)-1), dek, "blob-test-42", &plain); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain.Bytes(), want) {
		t.Fatalf("decrypted fixture = %q, want %q", plain.Bytes(), want)
	}

	tampered := append([]byte(nil), fixture...)
	tampered[len(tampered)-1] ^= 0x80
	if _, err := DecryptBlobRange(bytes.NewReader(tampered), int64(len(want)), 0, int64(len(want)-1), dek, "blob-test-42", io.Discard); err == nil {
		t.Fatal("accepted a blob with a modified GCM tag")
	}
}

func TestSegmentedBlobRoundTripsAndRanges(t *testing.T) {
	dek := bytes.Repeat([]byte{0x73}, keyLength)
	for _, size := range []int{0, 1, segmentSize - 1, segmentSize, segmentSize + 1, segmentSize*2 + 19} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			input := make([]byte, size)
			for i := range input {
				input[i] = byte((i*31 + size) % 251)
			}
			var encrypted bytes.Buffer
			written, err := EncryptBlob(&encrypted, bytes.NewReader(input), dek, "roundtrip-blob")
			if err != nil {
				t.Fatal(err)
			}
			wantSize, err := EncryptedSize(int64(size))
			if err != nil {
				t.Fatal(err)
			}
			if written != wantSize || int64(encrypted.Len()) != wantSize {
				t.Fatalf("encrypted bytes = %d/%d, want %d", written, encrypted.Len(), wantSize)
			}
			start, end := int64(0), int64(size-1)
			if size > 200 {
				start, end = int64(size/3), int64(size*2/3)
			}
			if size == 0 {
				end = -1
			}
			var got bytes.Buffer
			if _, err := DecryptBlobRange(bytes.NewReader(encrypted.Bytes()), int64(size), start, end, dek, "roundtrip-blob", &got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got.Bytes(), input[start:maxInt64(end+1, 0)]) {
				t.Fatal("decrypted range differs from plaintext")
			}
		})
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func TestSmallSecretRoundTrip(t *testing.T) {
	keyring, err := NewKeyring("0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := keyring.EncryptValue("sensitive setting")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := keyring.DecryptValue(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if opened != "sensitive setting" {
		t.Fatalf("opened %q", opened)
	}
}
