package signing

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
)

// Signature requests of a connected app through /api/v1. A request is an
// ordinary signing document: made from a rendered Markdown document or an
// existing PDF (with a signature page appended as a new file beside it), with
// a signature, printed name and date field per signer, then sent.

var agentStatuses = map[string][]string{
	"pending": {statusSent, statusInProgress, statusFinalizing}, "completed": {statusCompleted}, "cancelled": {statusCanceled},
	"expired": {statusExpired}, "declined": {statusDeclined}, "draft": {statusDraft},
}

func agentStatus(status string) string {
	switch status {
	case statusSent, statusInProgress, statusFinalizing:
		return "pending"
	case statusCanceled:
		return "cancelled"
	default:
		return status
	}
}

func (h *Handler) agentRoutes(mux *http.ServeMux) {
	route := func(pattern, scope string, heavy bool, status int, action func(r *http.Request, userID int64, input body) (any, error)) {
		method, path, _ := strings.Cut(pattern, " ")
		mux.Handle(method+" /api/v1/signing/requests"+path, h.agent.Guard(scope, heavy, func(w http.ResponseWriter, r *http.Request, userID int64) {
			input, err := readBody(w, r)
			if err != nil {
				writeErr(w, err)
				return
			}
			value, err := action(r, userID, input)
			respond(w, status, value, h.agentError(err))
		}))
	}
	route("POST ", "signing:write", true, 201, h.agentCreate)
	route("GET ", "signing:read", false, 200, h.agentList)
	route("GET /{id}", "signing:read", false, 200, func(r *http.Request, userID int64, _ body) (any, error) {
		return h.agentItem(r.Context(), userID, r.PathValue("id"))
	})
	route("POST /{id}/remind", "signing:write", false, 202, h.agentRemind)
	route("POST /{id}/signers/{signerId}/link", "signing:write", false, 200, func(r *http.Request, userID int64, _ body) (any, error) {
		link, err := h.regenerateLink(r, userID, r.PathValue("id"), r.PathValue("signerId"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"url": link["url"]}, nil
	})
	route("POST /{id}/cancel", "signing:write", false, 200, func(r *http.Request, userID int64, _ body) (any, error) {
		if err := h.cancel(r.Context(), userID, r.PathValue("id"), clientOf(r)); err != nil {
			return nil, err
		}
		return h.agentItem(r.Context(), userID, r.PathValue("id"))
	})
}

// agentError keeps the file service's codes under the agent API's names.
func (h *Handler) agentError(err error) error {
	var coded interface {
		error
		StatusCode() int
		ErrorCode() string
	}
	if errors.As(err, &coded) {
		return httpError(coded.StatusCode(), coded.Error(), coded.ErrorCode())
	}
	return err
}

func signerView(p party, url string) map[string]any {
	out := map[string]any{"id": p.ID, "name": p.Name, "email": nullIfEmpty(p.Email.String), "status": p.Status, "signedAt": nullable(p.CompletedAt)}
	if url != "" {
		out["url"] = url
	}
	return out
}

func (h *Handler) agentView(ctx context.Context, d document) (map[string]any, error) {
	parties, err := listParties(ctx, h.db, d.ID)
	if err != nil {
		return nil, err
	}
	signers := make([]map[string]any, 0, len(parties))
	for _, p := range parties {
		signers = append(signers, signerView(p, ""))
	}
	out := map[string]any{"id": d.ID, "title": d.Title, "status": agentStatus(d.Status), "createdAt": d.CreatedAt, "signers": signers}
	if d.FinalNodeID.String != "" {
		out["finalFileId"] = d.FinalNodeID.String
	}
	return out, nil
}

func (h *Handler) agentItem(ctx context.Context, userID int64, id string) (map[string]any, error) {
	d, err := ownedDocument(ctx, h.db, userID, id)
	if err != nil {
		return nil, err
	}
	return h.agentView(ctx, d)
}

func (h *Handler) agentList(r *http.Request, userID int64, _ body) (any, error) {
	ctx := r.Context()
	var statuses []string
	if status := r.URL.Query().Get("status"); status != "" {
		if statuses = agentStatuses[status]; statuses == nil {
			return nil, badRequest("status is one of: pending, completed, cancelled, expired, declined, draft.")
		}
	}
	var filter any
	if statuses != nil {
		filter = encodeJSON(statuses)
	}
	documents, err := queryDocuments(ctx, h.db, `WHERE owner_id=? AND (? IS NULL OR status IN (SELECT value FROM json_each(?))) ORDER BY updated_at DESC,id DESC LIMIT ?`,
		userID, filter, filter, pageSize(r.URL.Query().Get("limit"), 50))
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(documents))
	for _, d := range documents {
		view, err := h.agentView(ctx, d)
		if err != nil {
			return nil, err
		}
		items = append(items, view)
	}
	return map[string]any{"items": items}, nil
}

func (h *Handler) agentRemind(r *http.Request, userID int64, _ body) (any, error) {
	results, err := h.remindEveryone(r.Context(), userID, r.PathValue("id"), body{})
	var refused *apiError
	if errors.As(err, &refused) && refused.code == "EMAIL_NOT_CONFIGURED" {
		return nil, httpError(409, "Email is not set up on this NeoCloud.", refused.code)
	}
	if err != nil {
		return nil, err
	}
	reminded := []any{}
	for _, result := range results {
		if result["status"] == "queued" {
			reminded = append(reminded, result["partyId"])
		}
	}
	if len(reminded) == 0 {
		code := "NO_EMAIL"
		if len(results) > 0 {
			if first, ok := results[0]["code"].(string); ok {
				code = first
			}
		}
		return nil, httpError(409, "Nobody could be reminded (signers need an email address and must still be waiting).", code)
	}
	return map[string]any{"reminded": reminded}, nil
}

type agentSigner struct{ name, email string }

func cleanSigners(input any) ([]agentSigner, error) {
	list, ok := input.([]any)
	if !ok || len(list) < 1 || len(list) > maxParties {
		return nil, badRequest("signers must list 1 to 20 people.")
	}
	out := make([]agentSigner, 0, len(list))
	for _, raw := range list {
		entry, _ := raw.(map[string]any)
		name, _ := entry["name"].(string)
		if name = strings.TrimSpace(name); name == "" {
			return nil, badRequest("Every signer needs a name.")
		}
		email, _ := entry["email"].(string)
		out = append(out, agentSigner{name, strings.TrimSpace(email)})
	}
	return out, nil
}

type preparedFile struct {
	nodeID, title string
	boxes         []signpdf.SignatureBoxes
}

// prepareFile saves the file to sign in the library.
func (h *Handler) prepareFile(ctx context.Context, userID int64, input body, signers []agentSigner) (preparedFile, error) {
	var out preparedFile
	names := make([]string, len(signers))
	for i, s := range signers {
		names[i] = s.name
	}
	if doc, ok := input["document"].(map[string]any); ok {
		path, _ := doc["path"].(string)
		title, _ := doc["title"].(string)
		pdf, boxes, err := h.agent.RenderPDF(title, doc["markdown"], names)
		if err != nil {
			return out, err
		}
		nodeID, err := h.agent.CreateAt(ctx, userID, path, pdf, false)
		return preparedFile{nodeID: nodeID, title: strings.TrimSpace(title), boxes: boxes}, err
	}
	id, _ := input["fileId"].(string)
	source, err := h.agent.ReadFile(ctx, userID, id, maxSourceBytes, "This file is too large to sign (50 MB at most).")
	if err != nil {
		return out, err
	}
	if !strings.Contains(string(source.Data[:min(len(source.Data), 1024)]), "%PDF-") {
		return out, httpError(415, "Only PDF files can be sent for signing this way.", "UNSUPPORTED_TYPE")
	}
	pdf, boxes, err := signpdf.AppendSignaturePage(ctx, source.Data, names, "en")
	if err != nil {
		return out, pdfRefusal(err)
	}
	base, _ := splitExtension(source.Name)
	target := strings.TrimSuffix(source.ParentPath, "/") + "/" + base + " (for signature).pdf"
	nodeID, err := h.agent.CreateAt(ctx, userID, target, pdf, true)
	return preparedFile{nodeID: nodeID, title: base, boxes: boxes}, err
}

func (h *Handler) agentCreate(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	fileID, _ := input["fileId"].(string)
	_, hasDocument := input["document"].(map[string]any)
	if (fileID != "") == hasDocument {
		return nil, badRequest("Send exactly one of fileId or document.")
	}
	if hasDocument && !h.agent.HasScope(r, "files:write") {
		return nil, httpError(403, "This connection may not create files (files:write).", "INSUFFICIENT_SCOPE")
	}
	signers, err := cleanSigners(input["signers"])
	if err != nil {
		return nil, err
	}
	order := "parallel"
	if v, ok := input["signingOrder"]; ok {
		order, _ = v.(string)
		if !slices.Contains([]string{"parallel", "sequential"}, order) {
			return nil, badRequest("signingOrder is parallel or sequential.")
		}
	}
	sendEmail := false
	if v, ok := input["sendEmail"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return nil, badRequest("sendEmail must be true or false.")
		}
		sendEmail = b
	}
	settings := body{"signingOrder": map[string]string{"parallel": "random", "sequential": "preserved"}[order]}
	if v, ok := input["expiresInDays"]; ok {
		days, isNumber := v.(float64)
		if !isNumber || days != math.Trunc(days) || days < 1 || days > maxValidityDays {
			return nil, badRequest("expiresInDays is a whole number from 1 to 365.")
		}
		settings["expiresAt"] = h.now().Add(time.Duration(days) * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	}
	file, err := h.prepareFile(ctx, userID, input, signers)
	if err != nil {
		return nil, err
	}
	title, _ := input["title"].(string)
	if title = strings.TrimSpace(title); title == "" {
		title = file.title
	}
	var titleInput any
	if title != "" {
		titleInput = title
	}
	id, _, err := h.newDraft(ctx, userID, file.nodeID, titleInput, title != "", nil, false, clientOf(r))
	if err != nil {
		return nil, err
	}
	out, err := h.sendAgentDraft(r, userID, id, signers, file.boxes, settings, body{"message": input["message"], "deliverEmail": sendEmail})
	if err != nil {
		// Nothing went out: the half-made draft goes again (the file stays).
		if removeErr := h.remove(context.WithoutCancel(ctx), userID, id); removeErr != nil && !errors.Is(removeErr, sql.ErrNoRows) {
			err = errors.Join(err, removeErr)
		}
		return nil, err
	}
	return out, nil
}

func (h *Handler) sendAgentDraft(r *http.Request, userID int64, id string, signers []agentSigner, boxes []signpdf.SignatureBoxes, settings, options body) (map[string]any, error) {
	ctx := r.Context()
	parties := make([]any, len(signers))
	for i, s := range signers {
		parties[i] = map[string]any{"name": s.name, "email": s.email}
	}
	if err := h.setParties(ctx, userID, id, parties); err != nil {
		return nil, err
	}
	stored, err := listParties(ctx, h.db, id)
	if err != nil {
		return nil, err
	}
	area := func(a signpdf.Area) []any {
		return []any{map[string]any{"page": float64(a.Page), "x": a.X, "y": a.Y, "w": a.W, "h": a.H}}
	}
	var fields []any
	for i, p := range stored {
		fields = append(fields,
			map[string]any{"type": "signature", "partyId": p.ID, "name": "Signature", "required": true, "areas": area(boxes[i].Signature)},
			map[string]any{"type": "text", "partyId": p.ID, "name": "Name", "readonly": true, "defaultValue": p.Name, "areas": area(boxes[i].Name)},
			map[string]any{"type": "date", "partyId": p.ID, "name": "Date", "required": true, "prefs": map[string]any{"autoToday": true}, "areas": area(boxes[i].Date)})
	}
	if _, err := h.setFields(ctx, userID, id, fields, clientOf(r)); err != nil {
		return nil, err
	}
	update := r.Clone(ctx)
	update.SetPathValue("id", id)
	if _, err := h.updateSettings(update, userID, settings); err != nil {
		return nil, err
	}
	out, err := h.send(r, userID, id, options)
	if err != nil {
		return nil, err
	}
	d, err := ownedDocument(ctx, h.db, userID, id)
	if err != nil {
		return nil, err
	}
	urls := map[any]string{}
	for _, link := range out.links {
		urls[link["partyId"]], _ = link["url"].(string)
	}
	parts, err := listParties(ctx, h.db, id)
	if err != nil {
		return nil, err
	}
	views := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		views = append(views, signerView(p, urls[p.ID]))
	}
	return map[string]any{"id": id, "title": d.Title, "status": agentStatus(d.Status), "signers": views}, nil
}
