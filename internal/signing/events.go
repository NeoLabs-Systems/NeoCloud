package signing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sort"
	"strings"
)

// The audit trail of a document is an append-only list whose entries are
// chained by hash: each hash covers the previous one and the canonical JSON of
// the entry, so an edited, removed or reordered entry shows up in verify.

const genesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// client is who and where a request came from.
type client struct{ ip, userAgent string }

func clientOf(r *http.Request) client {
	if r == nil {
		return client{}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return client{strings.TrimPrefix(ip, "::ffff:"), shortUserAgent(r.UserAgent())}
}

func shortUserAgent(value string) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value))
	return truncate(value, 300)
}

type event struct {
	kind, partyID, at string
	client            client
	data              map[string]any
}

func chainHash(previous, documentID string, partyID, ip, userAgent any, kind, at string, data any) string {
	var out strings.Builder
	out.WriteString(previous)
	canonicalJSON(&out, map[string]any{
		"documentId": documentID, "partyId": partyID, "type": kind, "at": at, "ip": ip, "userAgent": userAgent, "data": data,
	})
	sum := sha256.Sum256([]byte(out.String()))
	return hex.EncodeToString(sum[:])
}

// canonicalJSON writes sorted keys and JavaScript's string escaping.
func canonicalJSON(out *strings.Builder, value any) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			jsString(out, key)
			out.WriteByte(':')
			canonicalJSON(out, v[key])
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			canonicalJSON(out, item)
		}
		out.WriteByte(']')
	case string:
		jsString(out, v)
	case json.Number:
		out.WriteString(v.String())
	case nil:
		out.WriteString("null")
	default:
		out.WriteString(encodeJSON(v))
	}
}

func jsString(out *strings.Builder, s string) {
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

// numbered re-reads data the way it is stored, so the hash made now equals the
// one recomputed from the row.
func numbered(raw string) any {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return map[string]any{}
	}
	return value
}

// appendEvent records one entry. Call it in the transaction that makes the
// change it records.
func appendEvent(ctx context.Context, q querier, documentID string, e event) error {
	var previous string
	err := q.QueryRowContext(ctx, `SELECT hash FROM sign_events WHERE document_id=? ORDER BY id DESC LIMIT 1`, documentID).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		previous = genesisHash
	} else if err != nil {
		return err
	}
	if e.data == nil {
		e.data = map[string]any{}
	}
	raw := encodeJSON(e.data)
	partyID, ip, userAgent := nullIfEmpty(e.partyID), nullIfEmpty(e.client.ip), nullIfEmpty(e.client.userAgent)
	hash := chainHash(previous, documentID, partyID, ip, userAgent, e.kind, e.at, numbered(raw))
	_, err = q.ExecContext(ctx, `INSERT INTO sign_events(document_id,party_id,type,at,ip,user_agent,data_json,prev_hash,hash) VALUES(?,?,?,?,?,?,?,?,?)`,
		documentID, partyID, e.kind, e.at, ip, userAgent, raw, previous, hash)
	return err
}

type storedEvent struct {
	id                     int64
	kind, at               string
	partyID, ip, userAgent text
	data                   any
	prevHash, hash         string
}

func listEvents(ctx context.Context, q querier, documentID string) ([]storedEvent, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,type,at,party_id,ip,user_agent,data_json,prev_hash,hash FROM sign_events WHERE document_id=? ORDER BY id`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedEvent
	for rows.Next() {
		var e storedEvent
		var raw text
		if err = rows.Scan(&e.id, &e.kind, &e.at, &e.partyID, &e.ip, &e.userAgent, &raw, &e.prevHash, &e.hash); err != nil {
			return nil, err
		}
		if raw.String == "" {
			raw.String = "{}"
		}
		e.data = numbered(raw.String)
		out = append(out, e)
	}
	return out, rows.Err()
}

func trail(ctx context.Context, q querier, documentID string) ([]map[string]any, map[string]any, error) {
	events, err := listEvents(ctx, q, documentID)
	if err != nil {
		return nil, nil, err
	}
	previous := genesisHash
	var broken any
	views := make([]map[string]any, 0, len(events))
	for _, e := range events {
		expected := chainHash(previous, documentID, nullable(e.partyID), nullable(e.ip), nullable(e.userAgent), e.kind, e.at, e.data)
		if broken == nil {
			if e.prevHash != previous || e.hash != expected {
				broken = e.id
			} else {
				previous = e.hash
			}
		}
		views = append(views, map[string]any{"id": e.id, "type": e.kind, "partyId": nullable(e.partyID), "at": e.at, "ip": nullable(e.ip),
			"userAgent": nullable(e.userAgent), "data": e.data, "prevHash": e.prevHash, "hash": e.hash})
	}
	verify := map[string]any{"chainValid": broken == nil, "count": len(events), "head": previous}
	if broken != nil {
		verify["brokenAt"] = broken
	}
	return views, verify, nil
}

func (h *Handler) eventsRoute(r *http.Request, userID int64, _ body) (any, error) {
	d, err := ownedDocument(r.Context(), h.db, userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	events, verify, err := trail(r.Context(), h.db, d.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"events": events, "verify": verify}, nil
}

// auditJSON is the whole record of a document in one file.
func (h *Handler) auditJSON(w http.ResponseWriter, r *http.Request, userID int64) {
	ctx := r.Context()
	record, err := func() (map[string]any, error) {
		d, err := ownedDocument(ctx, h.db, userID, r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		parties, err := listParties(ctx, h.db, d.ID)
		if err != nil {
			return nil, err
		}
		partyRecords := make([]map[string]any, 0, len(parties))
		for _, p := range parties {
			// What each party signed with: details a signer's page never shows.
			view := partyView(p)
			var offset any
			if p.TZOffset.Valid {
				offset = p.TZOffset.Int64
			}
			view["ip"], view["userAgent"], view["timezone"], view["tzOffsetMinutes"] = nullable(p.IP), nullable(p.UserAgent), nullable(p.Timezone), offset
			partyRecords = append(partyRecords, view)
		}
		fields, err := listFields(ctx, h.db, d.ID)
		if err != nil {
			return nil, err
		}
		values, err := listValues(ctx, h.db, d.ID)
		if err != nil {
			return nil, err
		}
		valueRecords := make([]map[string]any, 0, len(values))
		for _, v := range values {
			valueRecords = append(valueRecords, map[string]any{"fieldId": v.FieldID, "partyId": v.PartyID, "value": v.decoded(), "updatedAt": v.UpdatedAt})
		}
		events, verify, err := trail(ctx, h.db, d.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"format": 1, "kind": "neocloud-signing-audit", "exportedAt": h.stampNow(), "document": documentView(d),
			"parties": partyRecords, "fields": fieldViews(slices.Clip(fields)), "values": valueRecords, "events": events, "verify": verify}, nil
	}()
	if err == nil {
		w.Header().Set("Content-Disposition", `attachment; filename="signing-audit-`+r.PathValue("id")+`.json"`)
	}
	respond(w, 200, record, err)
}
