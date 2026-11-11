package fido

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// CTAPHID commands (CTAP 2.1 §11.2.9), with the init-packet bit set.
const (
	cmdInit      = 0x86
	cmdCBOR      = 0x90
	cmdCancel    = 0x91
	cmdKeepalive = 0xbb
	cmdError     = 0xbf

	broadcastCID = 0xffffffff
	initData     = reportSize - 7 // CID, CMD, BCNTH, BCNTL
	contData     = reportSize - 5 // CID, SEQ
	maxMessage   = initData + 128*contData

	capCBOR = 0x04

	// Keepalive status bytes.
	keepaliveUPNeeded = 2

	readSlice = 200 * time.Millisecond
)

// frame splits a message into CTAPHID packets: one init packet, then
// continuation packets with sequence numbers 0, 1, 2…
func frame(cid uint32, cmd byte, data []byte) ([][]byte, error) {
	if len(data) > maxMessage {
		return nil, fmt.Errorf("fido: message of %d bytes is too long", len(data))
	}
	first := make([]byte, reportSize)
	binary.BigEndian.PutUint32(first, cid)
	first[4] = cmd
	binary.BigEndian.PutUint16(first[5:], uint16(len(data)))
	data = data[copy(first[7:], data):]
	packets := [][]byte{first}
	for seq := byte(0); len(data) > 0; seq++ {
		packet := make([]byte, reportSize)
		binary.BigEndian.PutUint32(packet, cid)
		packet[4] = seq
		data = data[copy(packet[5:], data):]
		packets = append(packets, packet)
	}
	return packets, nil
}

// message reassembles one response from packets on one channel.
type message struct {
	cid     uint32
	cmd     byte
	data    []byte
	size    int
	nextSeq byte
	started bool
}

// add takes the next packet for m.cid. It returns true once the message
// is complete. Keepalives are reported through keepalive and skipped.
func (m *message) add(packet []byte, keepalive func(status byte)) (bool, error) {
	if len(packet) < reportSize {
		return false, fmt.Errorf("fido: short HID report (%d bytes)", len(packet))
	}
	if binary.BigEndian.Uint32(packet) != m.cid {
		return false, nil // another application's channel
	}
	if packet[4]&0x80 != 0 {
		if packet[4] == cmdKeepalive {
			if keepalive != nil {
				keepalive(packet[7])
			}
			return false, nil
		}
		m.cmd = packet[4]
		m.size = int(binary.BigEndian.Uint16(packet[5:]))
		if m.size > maxMessage {
			return false, fmt.Errorf("fido: response of %d bytes is too long", m.size)
		}
		m.data = append(m.data[:0], packet[7:7+min(m.size, initData)]...)
		m.nextSeq, m.started = 0, true
		return len(m.data) == m.size, nil
	}
	if !m.started || packet[4] != m.nextSeq {
		return false, fmt.Errorf("fido: unexpected continuation packet %d", packet[4])
	}
	m.nextSeq++
	m.data = append(m.data, packet[5:5+min(m.size-len(m.data), contData)]...)
	return len(m.data) == m.size, nil
}

// conn is a CTAPHID channel on an opened device.
type conn struct {
	dev Device
	cid uint32
	// onKeepalive sees each keepalive status while a request waits.
	onKeepalive func(status byte)
}

// newConn allocates a channel with CTAPHID_INIT (§11.2.9.1.3) and checks
// that the device speaks CTAP2 (the CBOR capability).
func newConn(ctx context.Context, dev Device) (*conn, error) {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	c := &conn{dev: dev, cid: broadcastCID}
	if err := c.send(cmdInit, nonce); err != nil {
		return nil, err
	}
	for {
		cmd, data, err := c.receive(ctx)
		if err != nil {
			return nil, err
		}
		if cmd != cmdInit || len(data) < 17 || !bytes.Equal(data[:8], nonce) {
			continue // a reply to another application's INIT
		}
		if data[16]&capCBOR == 0 {
			return nil, errors.New("this security key only speaks U2F; the lock needs a FIDO2 key")
		}
		c.cid = binary.BigEndian.Uint32(data[8:12])
		return c, nil
	}
}

// transact sends one request and waits for its response until ctx ends;
// then it sends CTAPHID_CANCEL so the key stops waiting for a touch.
func (c *conn) transact(ctx context.Context, cmd byte, data []byte) (byte, []byte, error) {
	if err := c.send(cmd, data); err != nil {
		return 0, nil, err
	}
	return c.receive(ctx)
}

func (c *conn) send(cmd byte, data []byte) error {
	packets, err := frame(c.cid, cmd, data)
	if err != nil {
		return err
	}
	for _, packet := range packets {
		if err := c.dev.Write(packet); err != nil {
			return err
		}
	}
	return nil
}

// receive waits for the next complete message on the channel.
func (c *conn) receive(ctx context.Context) (byte, []byte, error) {
	reply := message{cid: c.cid}
	for {
		if ctx.Err() != nil {
			if cancel, err := frame(c.cid, cmdCancel, nil); err == nil {
				_ = c.dev.Write(cancel[0])
			}
			return 0, nil, ctx.Err()
		}
		packet, err := c.dev.Read(readSlice)
		if errors.Is(err, ErrTimeout) {
			continue
		}
		if err != nil {
			return 0, nil, err
		}
		done, err := reply.add(packet, c.onKeepalive)
		if err != nil {
			return 0, nil, err
		}
		if !done {
			continue
		}
		if reply.cmd == cmdError {
			code := byte(0)
			if len(reply.data) > 0 {
				code = reply.data[0]
			}
			return 0, nil, fmt.Errorf("fido: CTAPHID error 0x%02x", code)
		}
		return reply.cmd, reply.data, nil
	}
}

// cbor sends one CTAP2 command (a command byte plus a CBOR map) and
// returns the CBOR body of a successful response.
func (c *conn) cbor(ctx context.Context, command byte, body []byte) ([]byte, error) {
	cmd, data, err := c.transact(ctx, cmdCBOR, append([]byte{command}, body...))
	if err != nil {
		return nil, err
	}
	if cmd != cmdCBOR || len(data) == 0 {
		return nil, fmt.Errorf("fido: unexpected CTAPHID response 0x%02x", cmd)
	}
	if data[0] != 0 {
		return nil, StatusError(data[0])
	}
	return data[1:], nil
}
