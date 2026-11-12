// Package fidotest is a software FIDO2 authenticator behind a fake HID
// transport, for tests. It speaks CTAPHID packets and implements
// getInfo, clientPIN (getKeyAgreement, getPinToken), makeCredential,
// getAssertion (with hmac-secret, or a plain WebAuthn sign-in with ES256
// signatures and discoverable credentials) and getNextAssertion, with PIN
// protocol 1 and its own crypto built straight from the CTAP 2.1 text so it
// checks the client rather than mirroring it.
package fidotest

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/neolabs/neocloud/clients/lite/internal/fido"
)

// Authenticator is one simulated security key.
type Authenticator struct {
	// PIN, when set, makes makeCredential require a PIN token.
	PIN string
	// NoSilent makes the key refuse up=false with CTAP2_ERR_INVALID_OPTION.
	NoSilent bool
	// NoTouch makes every request that waits for a touch time out.
	NoTouch bool
	// AlwaysUV is reported in getInfo.
	AlwaysUV bool
	// BuiltInUV reports a fingerprint reader ("uv" in getInfo); the uv
	// option then verifies the user without a PIN.
	BuiltInUV bool

	mu       sync.Mutex
	touches  int
	creds    map[string]*credential
	created  int
	agree    *ecdh.PrivateKey
	pinToken []byte
	next     [][]byte // getNextAssertion answers still to give
}

type credential struct {
	rpID       string
	credRandom []byte // CredRandomWithoutUV; the client never sends a PIN to getAssertion
	key        *ecdsa.PrivateKey
	userID     []byte
	resident   bool
	counter    uint32
	order      int
}

// newCredential stores a fresh ES256 credential and returns its id and
// the COSE public key.
func (a *Authenticator) newCredential(rpID string, userID []byte, resident bool) ([]byte, []byte) {
	if a.creds == nil {
		a.creds = map[string]*credential{}
	}
	id := make([]byte, 48)
	rand.Read(id)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cred := &credential{rpID: rpID, credRandom: make([]byte, 32), key: key, userID: userID, resident: resident}
	rand.Read(cred.credRandom)
	a.created++
	cred.order = a.created
	a.creds[string(id)] = cred
	public, _ := key.PublicKey.ECDH()
	point := public.Bytes()
	return id, encode(cose{Kty: 2, Alg: -7, Crv: 1, X: point[1:33], Y: point[33:]})
}

// PublicKey is the signing key of a credential the fake holds, or nil.
func (a *Authenticator) PublicKey(credentialID []byte) *ecdsa.PublicKey {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.creds[string(credentialID)]; ok {
		return &c.key.PublicKey
	}
	return nil
}

// Enroll creates a discoverable credential as a browser registration
// would and returns its id and a "none" attestation object (WebAuthn
// §6.5), for registering the fake key with a server.
func (a *Authenticator) Enroll(rpID string, userID []byte) (credentialID, attestationObject []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, publicKey := a.newCredential(rpID, userID, true)
	attested := append(make([]byte, 16), byte(len(id)>>8), byte(len(id)))
	attested = append(attested, id...)
	attested = append(attested, publicKey...)
	flags := byte(0x01 | 0x40)
	if a.PIN != "" {
		flags |= 0x04
	}
	return id, encode(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData(rpID, flags, 0, attested, nil),
	})
}

// Touches is how many times the key waited for (and got) a touch.
func (a *Authenticator) Touches() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.touches
}

// Transport is a fake HID bus that keys can be plugged into.
type Transport struct {
	mu      sync.Mutex
	next    int
	plugged map[string]*Authenticator
}

// NewTransport returns an empty bus.
func NewTransport() *Transport { return &Transport{plugged: map[string]*Authenticator{}} }

// Plug attaches a key and returns its device ID (new on every plug).
func (t *Transport) Plug(a *Authenticator) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next++
	id := "fake-" + strconv.Itoa(t.next)
	t.plugged[id] = a
	return id
}

// Unplug detaches a key.
func (t *Transport) Unplug(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.plugged, id)
}

// Devices implements fido.Transport.
func (t *Transport) Devices() ([]fido.DeviceInfo, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []fido.DeviceInfo
	for id := range t.plugged {
		out = append(out, fido.DeviceInfo{ID: id, Path: "fake:" + id, Product: "Fake Key"})
	}
	return out, nil
}

// Open implements fido.Transport.
func (t *Transport) Open(info fido.DeviceInfo) (fido.Device, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	a, ok := t.plugged[info.ID]
	if !ok {
		return nil, errors.New("fidotest: no such device")
	}
	return &device{bus: t, id: info.ID, key: a}, nil
}

func (t *Transport) present(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.plugged[id]
	return ok
}

// device is one open handle: it reassembles requests and queues replies.
type device struct {
	bus     *Transport
	id      string
	key     *Authenticator
	nextCID uint32
	in      []byte
	inCID   uint32
	inCmd   byte
	inSize  int
	out     [][]byte
}

func (d *device) Write(report []byte) error {
	if !d.bus.present(d.id) {
		return errors.New("fidotest: device removed")
	}
	if len(report) != 64 {
		return fmt.Errorf("fidotest: report of %d bytes", len(report))
	}
	cid := binary.BigEndian.Uint32(report)
	if report[4]&0x80 != 0 {
		d.inCID, d.inCmd = cid, report[4]
		d.inSize = int(binary.BigEndian.Uint16(report[5:]))
		d.in = append([]byte(nil), report[7:7+min(d.inSize, 57)]...)
	} else {
		if cid != d.inCID {
			return errors.New("fidotest: continuation on another channel")
		}
		d.in = append(d.in, report[5:5+min(d.inSize-len(d.in), 59)]...)
	}
	if len(d.in) < d.inSize {
		return nil
	}
	switch d.inCmd {
	case 0x86: // INIT
		d.nextCID++
		resp := append(append([]byte(nil), d.in[:8]...), 0, 0, 0, 0, 2, 1, 0, 0, 0x04|0x01)
		binary.BigEndian.PutUint32(resp[8:], d.nextCID)
		d.reply(d.inCID, 0x86, resp)
	case 0x90: // CBOR
		status, body, touched := d.key.handle(d.in)
		if touched {
			d.reply(d.inCID, 0xbb, []byte{2}) // KEEPALIVE: UPNEEDED
		}
		d.reply(d.inCID, 0x90, append([]byte{status}, body...))
	case 0x91: // CANCEL: nothing is pending in this synchronous fake
	default:
		d.reply(d.inCID, 0xbf, []byte{0x01})
	}
	return nil
}

func (d *device) reply(cid uint32, cmd byte, data []byte) {
	first := make([]byte, 64)
	binary.BigEndian.PutUint32(first, cid)
	first[4] = cmd
	binary.BigEndian.PutUint16(first[5:], uint16(len(data)))
	data = data[copy(first[7:], data):]
	d.out = append(d.out, first)
	for seq := byte(0); len(data) > 0; seq++ {
		packet := make([]byte, 64)
		binary.BigEndian.PutUint32(packet, cid)
		packet[4] = seq
		data = data[copy(packet[5:], data):]
		d.out = append(d.out, packet)
	}
}

func (d *device) Read(timeout time.Duration) ([]byte, error) {
	if !d.bus.present(d.id) {
		return nil, errors.New("fidotest: device removed")
	}
	if len(d.out) == 0 {
		return nil, fido.ErrTimeout
	}
	packet := d.out[0]
	d.out = d.out[1:]
	return packet, nil
}

func (d *device) Close() error { return nil }

// CTAP2 status codes the fake returns.
const (
	statusOK             = 0x00
	statusInvalidCBOR    = 0x12
	statusMissingParam   = 0x14
	statusInvalidOption  = 0x2c
	statusNoCredentials  = 0x2e
	statusActionTimeout  = 0x2f
	statusPINInvalid     = 0x31
	statusPINAuthInvalid = 0x33
	statusPINRequired    = 0x36
)

func (a *Authenticator) handle(request []byte) (status byte, body []byte, touched bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(request) == 0 {
		return statusInvalidCBOR, nil, false
	}
	params := map[int]cbor.RawMessage{}
	if len(request) > 1 {
		if err := cbor.Unmarshal(request[1:], &params); err != nil {
			return statusInvalidCBOR, nil, false
		}
	}
	switch request[0] {
	case 0x04:
		return a.getInfo()
	case 0x06:
		return a.clientPIN(params)
	case 0x01:
		return a.makeCredential(params)
	case 0x02:
		if _, ok := params[4]; ok {
			return a.getAssertion(params)
		}
		return a.signIn(params)
	case 0x08:
		if len(a.next) == 0 {
			return 0x30, nil, false // CTAP2_ERR_NOT_ALLOWED
		}
		answer := a.next[0]
		a.next = a.next[1:]
		return statusOK, answer, false
	}
	return 0x01, nil, false
}

func encode(v any) []byte {
	mode, _ := cbor.CTAP2EncOptions().EncMode()
	out, err := mode.Marshal(v)
	if err != nil {
		panic(err)
	}
	return out
}

func (a *Authenticator) getInfo() (byte, []byte, bool) {
	options := map[string]bool{"rk": true, "up": true, "clientPin": a.PIN != ""}
	if a.BuiltInUV {
		options["uv"] = true
	}
	if a.AlwaysUV {
		options["alwaysUv"] = true
	}
	return statusOK, encode(map[int]any{
		1: []string{"FIDO_2_0", "FIDO_2_1"},
		2: []string{"hmac-secret"},
		3: make([]byte, 16),
		4: options,
		6: []int{1},
	}), false
}

// cose is the fake's view of a COSE EC2 key.
type cose struct {
	Kty int    `cbor:"1,keyasint"`
	Alg int    `cbor:"3,keyasint"`
	Crv int    `cbor:"-1,keyasint"`
	X   []byte `cbor:"-2,keyasint"`
	Y   []byte `cbor:"-3,keyasint"`
}

func (a *Authenticator) keyAgreementKey() cose {
	if a.agree == nil {
		a.agree, _ = ecdh.P256().GenerateKey(rand.Reader)
	}
	public := a.agree.PublicKey().Bytes()
	return cose{Kty: 2, Alg: -25, Crv: 1, X: public[1:33], Y: public[33:]}
}

// sharedSecret is SHA-256(x) of ECDH with the platform's key (§6.5.6).
func (a *Authenticator) sharedSecret(raw cbor.RawMessage) ([]byte, bool) {
	var platform cose
	if cbor.Unmarshal(raw, &platform) != nil || platform.Kty != 2 || platform.Crv != 1 {
		return nil, false
	}
	a.keyAgreementKey()
	peer, err := ecdh.P256().NewPublicKey(append(append([]byte{4}, platform.X...), platform.Y...))
	if err != nil {
		return nil, false
	}
	z, err := a.agree.ECDH(peer)
	if err != nil {
		return nil, false
	}
	sum := sha256.Sum256(z)
	return sum[:], true
}

func cbc(key, data []byte, encrypt bool) []byte {
	block, _ := aes.NewCipher(key)
	out := make([]byte, len(data))
	iv := make([]byte, 16)
	if encrypt {
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, data)
	} else {
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	}
	return out
}

func mac16(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)[:16]
}

func (a *Authenticator) clientPIN(params map[int]cbor.RawMessage) (byte, []byte, bool) {
	var protocol, sub int
	if cbor.Unmarshal(params[1], &protocol) != nil || protocol != 1 || cbor.Unmarshal(params[2], &sub) != nil {
		return statusMissingParam, nil, false
	}
	switch sub {
	case 0x02: // getKeyAgreement
		return statusOK, encode(map[int]any{1: a.keyAgreementKey()}), false
	case 0x05: // getPinToken
		shared, ok := a.sharedSecret(params[3])
		var pinHashEnc []byte
		if !ok || cbor.Unmarshal(params[6], &pinHashEnc) != nil || len(pinHashEnc) != 16 {
			return statusMissingParam, nil, false
		}
		want := sha256.Sum256([]byte(a.PIN))
		if a.PIN == "" || !bytes.Equal(cbc(shared, pinHashEnc, false), want[:16]) {
			a.agree = nil // a new key agreement key after a wrong PIN
			return statusPINInvalid, nil, false
		}
		a.pinToken = make([]byte, 32)
		rand.Read(a.pinToken)
		return statusOK, encode(map[int]any{2: cbc(shared, a.pinToken, true)}), false
	}
	return statusMissingParam, nil, false
}

// touch waits for the user: it reports false when NoTouch simulates
// nobody touching the key.
func (a *Authenticator) touch() bool {
	if a.NoTouch {
		return false
	}
	a.touches++
	return true
}

func authData(rpID string, flags byte, counter uint32, attested, extensions []byte) []byte {
	hash := sha256.Sum256([]byte(rpID))
	out := append(hash[:], flags, byte(counter>>24), byte(counter>>16), byte(counter>>8), byte(counter))
	out = append(out, attested...)
	return append(out, extensions...)
}

func (a *Authenticator) makeCredential(params map[int]cbor.RawMessage) (byte, []byte, bool) {
	var clientDataHash []byte
	var rp struct {
		ID string `cbor:"id"`
	}
	var extensions map[string]bool
	if cbor.Unmarshal(params[1], &clientDataHash) != nil || cbor.Unmarshal(params[2], &rp) != nil || rp.ID == "" {
		return statusMissingParam, nil, false
	}
	if raw, ok := params[6]; ok && cbor.Unmarshal(raw, &extensions) != nil {
		return statusInvalidCBOR, nil, false
	}
	if a.PIN != "" {
		var pinAuth []byte
		if cbor.Unmarshal(params[8], &pinAuth) != nil {
			return statusPINRequired, nil, false
		}
		if a.pinToken == nil || !hmac.Equal(pinAuth, mac16(a.pinToken, clientDataHash)) {
			return statusPINAuthInvalid, nil, false
		}
	}
	if !a.touch() {
		return statusActionTimeout, nil, true
	}
	id, publicKey := a.newCredential(rp.ID, nil, false)
	attested := append(make([]byte, 16), byte(len(id)>>8), byte(len(id)))
	attested = append(attested, id...)
	attested = append(attested, publicKey...)
	flags := byte(0x01 | 0x40)
	var ext []byte
	if extensions["hmac-secret"] {
		flags |= 0x80
		ext = encode(map[string]bool{"hmac-secret": true})
	}
	return statusOK, encode(map[int]any{
		1: "none",
		2: authData(rp.ID, flags, 1, attested, ext),
		3: map[string]any{},
	}), true
}

func (a *Authenticator) getAssertion(params map[int]cbor.RawMessage) (byte, []byte, bool) {
	var rpID string
	var allow []struct {
		ID   []byte `cbor:"id"`
		Type string `cbor:"type"`
	}
	options := map[string]bool{"up": true}
	if cbor.Unmarshal(params[1], &rpID) != nil || cbor.Unmarshal(params[3], &allow) != nil {
		return statusMissingParam, nil, false
	}
	if raw, ok := params[5]; ok && cbor.Unmarshal(raw, &options) != nil {
		return statusInvalidCBOR, nil, false
	}
	up, ok := options["up"]
	if !ok {
		up = true
	}
	var found []byte
	var cred *credential
	for _, entry := range allow {
		if c, ok := a.creds[string(entry.ID)]; ok && c.rpID == rpID {
			found, cred = entry.ID, c
		}
	}
	if found == nil {
		return statusNoCredentials, nil, false
	}
	if !up && a.NoSilent {
		return statusInvalidOption, nil, false
	}
	var ext struct {
		HMACSecret map[int]cbor.RawMessage `cbor:"hmac-secret"`
	}
	if raw, ok := params[4]; !ok || cbor.Unmarshal(raw, &ext) != nil || ext.HMACSecret == nil {
		return statusMissingParam, nil, false
	}
	shared, ok := a.sharedSecret(ext.HMACSecret[1])
	var saltEnc, saltAuth []byte
	if !ok || cbor.Unmarshal(ext.HMACSecret[2], &saltEnc) != nil || cbor.Unmarshal(ext.HMACSecret[3], &saltAuth) != nil {
		return statusMissingParam, nil, false
	}
	if !hmac.Equal(saltAuth, mac16(shared, saltEnc)) {
		return statusPINAuthInvalid, nil, false
	}
	if len(saltEnc) != 32 && len(saltEnc) != 64 {
		return statusMissingParam, nil, false
	}
	salt := cbc(shared, saltEnc, false)
	flags := byte(0x80)
	if up {
		if !a.touch() {
			return statusActionTimeout, nil, true
		}
		flags |= 0x01
	}
	var output []byte
	for i := 0; i < len(salt); i += 32 {
		m := hmac.New(sha256.New, cred.credRandom)
		m.Write(salt[i : i+32])
		output = append(output, m.Sum(nil)...)
	}
	extensions := encode(map[string][]byte{"hmac-secret": cbc(shared, output, true)})
	return statusOK, encode(map[int]any{
		1: map[string]any{"id": found, "type": "public-key"},
		2: authData(rpID, flags, 1, nil, extensions),
		3: make([]byte, 64),
	}), up
}

// signIn is getAssertion without extensions, as a WebAuthn sign-in sends
// it (CTAP 2.1 §6.2.2): an ES256 signature over authenticatorData ||
// clientDataHash. An empty allow list picks the discoverable credentials
// of the relying party, newest first, the rest via getNextAssertion.
func (a *Authenticator) signIn(params map[int]cbor.RawMessage) (byte, []byte, bool) {
	var rpID string
	var clientDataHash []byte
	var allow []struct {
		ID   []byte `cbor:"id"`
		Type string `cbor:"type"`
	}
	options := map[string]bool{}
	if cbor.Unmarshal(params[1], &rpID) != nil || cbor.Unmarshal(params[2], &clientDataHash) != nil || len(clientDataHash) != 32 {
		return statusMissingParam, nil, false
	}
	if raw, ok := params[3]; ok && cbor.Unmarshal(raw, &allow) != nil {
		return statusInvalidCBOR, nil, false
	}
	if raw, ok := params[5]; ok && cbor.Unmarshal(raw, &options) != nil {
		return statusInvalidCBOR, nil, false
	}
	verified := false
	if raw, ok := params[6]; ok {
		var pinAuth []byte
		var protocol int
		if cbor.Unmarshal(raw, &pinAuth) != nil || cbor.Unmarshal(params[7], &protocol) != nil || protocol != 1 {
			return statusMissingParam, nil, false
		}
		if a.pinToken == nil || !hmac.Equal(pinAuth, mac16(a.pinToken, clientDataHash)) {
			return statusPINAuthInvalid, nil, false
		}
		verified = true
	} else if options["uv"] {
		if !a.BuiltInUV {
			return statusInvalidOption, nil, false
		}
		verified = true
	}
	if a.AlwaysUV && !verified {
		return statusPINRequired, nil, false
	}

	var ids [][]byte
	if len(allow) > 0 {
		for _, entry := range allow {
			if c, ok := a.creds[string(entry.ID)]; ok && c.rpID == rpID {
				ids = append(ids, entry.ID)
				break
			}
		}
	} else {
		for id, c := range a.creds {
			if c.resident && c.rpID == rpID {
				ids = append(ids, []byte(id))
			}
		}
		sort.Slice(ids, func(i, j int) bool { return a.creds[string(ids[i])].order > a.creds[string(ids[j])].order })
	}
	if len(ids) == 0 {
		return statusNoCredentials, nil, false
	}
	if !a.touch() {
		return statusActionTimeout, nil, true
	}
	flags := byte(0x01)
	if verified {
		flags |= 0x04
	}
	var answers [][]byte
	for i, id := range ids {
		c := a.creds[string(id)]
		c.counter++
		data := authData(rpID, flags, c.counter, nil, nil)
		digest := sha256.Sum256(append(append([]byte(nil), data...), clientDataHash...))
		signature, err := ecdsa.SignASN1(rand.Reader, c.key, digest[:])
		if err != nil {
			return 0x7f, nil, true
		}
		answer := map[int]any{
			1: map[string]any{"id": id, "type": "public-key"},
			2: data,
			3: signature,
		}
		if len(allow) == 0 {
			answer[4] = map[string]any{"id": c.userID}
			if i == 0 && len(ids) > 1 {
				answer[5] = len(ids)
			}
		}
		answers = append(answers, encode(answer))
	}
	a.next = answers[1:]
	return statusOK, answers[0], true
}
