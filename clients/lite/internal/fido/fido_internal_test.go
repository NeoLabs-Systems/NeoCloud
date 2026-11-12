package fido

import (
	"bytes"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	out, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func seq(n int, start byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = start + byte(i)
	}
	return out
}

// CTAPHID framing per CTAP 2.1 §11.2.4: init packet CID|CMD|BCNT|57 bytes,
// continuation packets CID|SEQ|59 bytes, zero padded.
func TestFrameAndReassemble(t *testing.T) {
	data := seq(120, 0)
	packets, err := frame(0x01020304, cmdCBOR, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 3 {
		t.Fatalf("%d packets, want 3 (57 + 59 + 4 bytes)", len(packets))
	}
	wantFirst := append(unhex(t, "01020304 90 0078"), data[:57]...)
	if !bytes.Equal(packets[0], wantFirst) {
		t.Errorf("init packet\n got %x\nwant %x", packets[0], wantFirst)
	}
	wantSecond := append(unhex(t, "01020304 00"), data[57:116]...)
	if !bytes.Equal(packets[1], wantSecond) {
		t.Errorf("continuation 0\n got %x\nwant %x", packets[1], wantSecond)
	}
	wantThird := append(append(unhex(t, "01020304 01"), data[116:]...), make([]byte, 55)...)
	if !bytes.Equal(packets[2], wantThird) {
		t.Errorf("continuation 1\n got %x\nwant %x", packets[2], wantThird)
	}

	// Reassembly skips other channels and keepalives.
	keepalive := make([]byte, reportSize)
	copy(keepalive, unhex(t, "01020304 bb 0001 02"))
	foreign := make([]byte, reportSize)
	copy(foreign, unhex(t, "0a0b0c0d 90 0001 00"))
	var statuses []byte
	m := message{cid: 0x01020304}
	stream := [][]byte{foreign, keepalive, packets[0], packets[1], packets[2]}
	for i, packet := range stream {
		done, err := m.add(packet, func(s byte) { statuses = append(statuses, s) })
		if err != nil {
			t.Fatal(err)
		}
		if done != (i == len(stream)-1) {
			t.Fatalf("packet %d: done=%v", i, done)
		}
	}
	if m.cmd != cmdCBOR || !bytes.Equal(m.data, data) || !bytes.Equal(statuses, []byte{keepaliveUPNeeded}) {
		t.Errorf("reassembled cmd %x, %d bytes, keepalives %v", m.cmd, len(m.data), statuses)
	}

	// Out-of-order continuation packets are an error.
	m = message{cid: 0x01020304}
	m.add(packets[0], nil)
	if _, err := m.add(packets[2], nil); err == nil {
		t.Error("sequence gap accepted")
	}
	if _, err := frame(1, cmdCBOR, make([]byte, maxMessage+1)); err == nil {
		t.Error("oversized message framed")
	}
	if maxMessage != 7609 {
		t.Errorf("maxMessage %d, want 7609 (57 + 128*59)", maxMessage)
	}
}

// makeCredential in CTAP2 canonical CBOR (§8): integer keys ascending,
// text keys shorter first, as in the spec's example request.
func TestMakeCredentialEncoding(t *testing.T) {
	req := makeCredentialRequest{
		ClientDataHash:    seq(32, 0),
		RP:                rpEntity{ID: "neocloud-lite.local", Name: "NeoCloud Lite"},
		User:              userEntity{ID: []byte{0xaa, 0xbb, 0xcc, 0xdd}, Name: "neo", DisplayName: "neo"},
		PubKeyCredParams:  []credentialParameter{{Alg: coseES256, Type: "public-key"}},
		Extensions:        map[string]bool{hmacSecret: true},
		PinUvAuthParam:    seq(16, 0x40),
		PinUvAuthProtocol: 1,
	}
	got, err := encMode.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	want := unhex(t, `
		a7
		01 5820 000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f
		02 a2 62 6964 73 6e656f636c6f75642d6c6974652e6c6f63616c
		      64 6e616d65 6d 4e656f436c6f7564204c697465
		03 a3 62 6964 44 aabbccdd
		      64 6e616d65 63 6e656f
		      6b 646973706c61794e616d65 63 6e656f
		04 81 a2 63 616c67 26 64 74797065 6a 7075626c69632d6b6579
		06 a1 6b 686d61632d736563726574 f5
		08 50 404142434445464748494a4b4c4d4e4f
		09 01`)
	if !bytes.Equal(got, want) {
		t.Errorf("makeCredential CBOR\n got %x\nwant %x", got, want)
	}
}

func TestGetAssertionEncoding(t *testing.T) {
	req := getAssertionRequest{
		RPID:           "neocloud-lite.local",
		ClientDataHash: seq(32, 0),
		AllowList:      []credentialDescriptor{{ID: []byte{1, 2, 3}, Type: "public-key"}},
		Extensions: assertionExtensions{HMACSecret: hmacSecretInput{
			KeyAgreement: coseKey{Kty: 2, Alg: -25, Crv: 1, X: seq(32, 0x10), Y: seq(32, 0x30)},
			SaltEnc:      seq(32, 0x50),
			SaltAuth:     seq(16, 0x70),
		}},
		Options: map[string]bool{"up": false},
	}
	got, err := encMode.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	want := unhex(t, `
		a5
		01 73 6e656f636c6f75642d6c6974652e6c6f63616c
		02 5820 000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f
		03 81 a2 62 6964 43 010203 64 74797065 6a 7075626c69632d6b6579
		04 a1 6b 686d61632d736563726574 a3
		      01 a5 01 02 03 3818 20 01
		            21 5820 101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f
		            22 5820 303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f
		      02 5820 505152535455565758595a5b5c5d5e5f606162636465666768696a6b6c6d6e6f
		      03 50 707172737475767778797a7b7c7d7e7f
		05 a1 62 7570 f4`)
	if !bytes.Equal(got, want) {
		t.Errorf("getAssertion CBOR\n got %x\nwant %x", got, want)
	}
}

// PIN protocol 1 primitives against published vectors.
func TestPinProtocolOnePrimitives(t *testing.T) {
	// FIPS-197 Appendix C.3: one block of AES-256; CBC with a zero IV is
	// the same as ECB for the first block.
	key := seq(32, 0)
	plain := unhex(t, "00112233445566778899aabbccddeeff")
	cipherText := encrypt(key, plain)
	if want := unhex(t, "8ea2b7ca516745bfeafc49904b496089"); !bytes.Equal(cipherText, want) {
		t.Errorf("encrypt = %x, want %x", cipherText, want)
	}
	if back, err := decrypt(key, cipherText); err != nil || !bytes.Equal(back, plain) {
		t.Errorf("decrypt = %x, %v", back, err)
	}
	if _, err := decrypt(key, []byte{1, 2, 3}); err == nil {
		t.Error("partial block decrypted")
	}
	// RFC 4231 test case 2, truncated to 16 bytes (LEFT(HMAC, 16)).
	mac := authenticate([]byte("Jefe"), []byte("what do ya want for nothing?"))
	if want := unhex(t, "5bdcc146bf60754e6a042426089575c7"); !bytes.Equal(mac, want) {
		t.Errorf("authenticate = %x, want %x", mac, want)
	}
}

func TestKeyAgreement(t *testing.T) {
	curve := ecdh.P256()
	authenticatorKey, err := curve.NewPrivateKey(seq(32, 1))
	if err != nil {
		t.Fatal(err)
	}
	platformKey, err := curve.NewPrivateKey(seq(32, 2))
	if err != nil {
		t.Fatal(err)
	}
	pub := authenticatorKey.PublicKey().Bytes()
	platform, shared, err := agree(platformKey, coseKey{Kty: 2, Alg: -25, Crv: 1, X: pub[1:33], Y: pub[33:]})
	if err != nil {
		t.Fatal(err)
	}
	if platform.Kty != 2 || platform.Alg != -25 || platform.Crv != 1 || len(platform.X) != 32 || len(platform.Y) != 32 {
		t.Errorf("platform COSE key %+v", platform)
	}
	// The authenticator's side: SHA-256 of the shared point's x-coordinate.
	peer, _ := curve.NewPublicKey(append(append([]byte{4}, platform.X...), platform.Y...))
	z, _ := authenticatorKey.ECDH(peer)
	if want := sha256.Sum256(z); !bytes.Equal(shared, want[:]) {
		t.Error("shared secrets differ")
	}
	if _, _, err := agree(platformKey, coseKey{X: []byte{1}, Y: []byte{2}}); err == nil {
		t.Error("malformed peer key accepted")
	}
}

func TestParseAuthData(t *testing.T) {
	rpHash := sha256.Sum256([]byte("neocloud-lite.local"))
	cose := unhex(t, "a5 01 02 03 26 20 01 21 5820"+hex.EncodeToString(seq(32, 0))+" 22 5820"+hex.EncodeToString(seq(32, 0x20)))
	raw := append(rpHash[:], 0x41|0x80, 0, 0, 0, 7)
	raw = append(raw, make([]byte, 16)...) // aaguid
	raw = append(raw, 0, 3, 9, 8, 7)       // credential id length 3
	raw = append(raw, cose...)
	raw = append(raw, unhex(t, "a1 6b 686d61632d736563726574 f5")...)
	data, err := parseAuthData(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data.credentialID, []byte{9, 8, 7}) || string(data.extensions[hmacSecret]) != "\xf5" {
		t.Errorf("parsed %+v", data)
	}
	if _, err := parseAuthData(append(raw, 0)); err == nil {
		t.Error("trailing bytes accepted")
	}
	if _, err := parseAuthData(raw[:40]); err == nil {
		t.Error("truncated data accepted")
	}
}

func TestFIDODescriptor(t *testing.T) {
	// The FIDO interface of a YubiKey 5: usage page 0xF1D0, usage 1, 64-byte in/out reports.
	yubikey := unhex(t, "06d0f1 0901 a101 0920 1500 26ff00 7508 9540 8102 0921 1500 26ff00 7508 9540 9102 c0")
	if !isFIDODescriptor(yubikey) {
		t.Error("FIDO descriptor not recognized")
	}
	keyboard := unhex(t, "0501 0906 a101 0507 19e0 29e7 1500 2501 7501 9508 8102 c0")
	if isFIDODescriptor(keyboard) {
		t.Error("keyboard recognized as FIDO")
	}
	if isFIDODescriptor(unhex(t, "06d0")) {
		t.Error("truncated descriptor recognized")
	}
}
