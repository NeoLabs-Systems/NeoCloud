// Package fidotest is a software FIDO2 authenticator behind a fake HID
// transport, for tests. It speaks CTAPHID packets and implements
// getInfo, clientPIN (getKeyAgreement, getPinToken), makeCredential and
// getAssertion with hmac-secret and PIN protocol 1, with its own crypto
// built straight from the CTAP 2.1 text so it checks the client rather
// than mirroring it.
package fidotest

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
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

	mu       sync.Mutex
	touches  int
	creds    map[string]credential
	agree    *ecdh.PrivateKey
	pinToken []byte
}

type credential struct {
	rpID       string
	credRandom []byte // CredRandomWithoutUV; the client never sends a PIN to getAssertion
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
	if a.creds == nil {
		a.creds = map[string]credential{}
	}
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
		return a.getAssertion(params)
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

func authData(rpID string, flags byte, attested, extensions []byte) []byte {
	hash := sha256.Sum256([]byte(rpID))
	out := append(hash[:], flags, 0, 0, 0, 1)
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
	id := make([]byte, 48)
	rand.Read(id)
	cred := credential{rpID: rp.ID, credRandom: make([]byte, 32)}
	rand.Read(cred.credRandom)
	a.creds[string(id)] = cred

	key, _ := ecdh.P256().GenerateKey(rand.Reader)
	public := key.PublicKey().Bytes()
	attested := append(make([]byte, 16), byte(len(id)>>8), byte(len(id)))
	attested = append(attested, id...)
	attested = append(attested, encode(cose{Kty: 2, Alg: -7, Crv: 1, X: public[1:33], Y: public[33:]})...)
	flags := byte(0x01 | 0x40)
	var ext []byte
	if extensions["hmac-secret"] {
		flags |= 0x80
		ext = encode(map[string]bool{"hmac-secret": true})
	}
	return statusOK, encode(map[int]any{
		1: "none",
		2: authData(rp.ID, flags, attested, ext),
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
	var cred credential
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
		2: authData(rpID, flags, nil, extensions),
		3: make([]byte, 64),
	}), up
}
