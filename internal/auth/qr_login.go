package auth

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/qrlogin"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"github.com/NeoLabs-Systems/neolabs-go/userid"
)

// A signed-out browser shows a code, the signed-in phone approves it, and the
// browser claims a session. The challenge state machine is shared.

var qrMessages = map[string]string{
	qrlogin.CodeNotFound:    "This sign-in request was not found or has expired.",
	qrlogin.CodeDeclined:    "This sign-in request was cancelled.",
	qrlogin.CodeApproved:    "This sign-in request was already approved.",
	qrlogin.CodeUsed:        "This sign-in request was already used.",
	qrlogin.CodeNotApproved: "This sign-in request is not approved yet.",
}

func writeQRError(w http.ResponseWriter, err error) {
	if failure, ok := apperr.As(err); ok {
		writeError(w, failure.Status, qrMessages[failure.Code], failure.Code)
		return
	}
	writeError(w, http.StatusInternalServerError, "Could not process the sign-in request", "INTERNAL_ERROR")
}

func (h *Handler) qrCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RequestMetadata any `json:"requestMetadata"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	ip := truncate(ratelimit.ClientIP(r), 64)
	challenge, err := h.qr.Create(r.Context(), qrlogin.Request{
		UserAgent: truncateRunes(strings.TrimSpace(r.UserAgent()), 500), IPAddress: ip, Metadata: body.RequestMetadata,
	})
	if err != nil {
		writeQRError(w, err)
		return
	}
	origin := h.origin(r)
	query := url.Values{"v": {"1"}, "backend": {origin}, "challenge": {challenge.ChallengeID}, "secret": {challenge.ApproveSecret}}
	writeJSON(w, http.StatusOK, map[string]any{
		"challengeId": challenge.ChallengeID, "pollToken": challenge.PollToken, "expiresAt": challenge.ExpiresAt,
		"status": challenge.Status, "qrPayload": "neocloud://qr-login?" + query.Encode(), "backendUrl": origin,
	})
}

type qrPollRequest struct {
	Token       string `json:"token"`
	KnownStatus string `json:"knownStatus"`
}

func qrPoll(w http.ResponseWriter, r *http.Request) (qrPollRequest, bool) {
	var body qrPollRequest
	if !decodeJSON(w, r, &body) {
		return body, false
	}
	body.Token, body.KnownStatus = strings.TrimSpace(body.Token), strings.TrimSpace(body.KnownStatus)
	if body.Token == "" {
		writeError(w, http.StatusBadRequest, "QR sign-in poll token is required", "MISSING_FIELDS")
		return body, false
	}
	return body, true
}

// qrStatus is held open while the status is still knownStatus, so the
// signing-in screen hears about a scan or approval at once.
func (h *Handler) qrStatus(w http.ResponseWriter, r *http.Request) {
	body, ok := qrPoll(w, r)
	if !ok {
		return
	}
	var status *qrlogin.Poll
	var err error
	if body.KnownStatus == "" {
		status, err = h.qr.PollStatus(r.Context(), r.PathValue("id"), body.Token)
	} else {
		status, err = h.qr.WaitForChange(r.Context(), r.PathValue("id"), body.Token, body.KnownStatus, qrlogin.LongPollMax)
	}
	if err != nil {
		if r.Context().Err() == nil {
			writeQRError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) qrCancel(w http.ResponseWriter, r *http.Request) {
	body, ok := qrPoll(w, r)
	if !ok {
		return
	}
	if err := h.qr.Cancel(r.Context(), r.PathValue("id"), body.Token); err != nil {
		writeQRError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) qrClaim(w http.ResponseWriter, r *http.Request) {
	body, ok := qrPoll(w, r)
	if !ok {
		return
	}
	claimed, err := h.qr.Claim(r.Context(), r.PathValue("id"), body.Token)
	if err != nil {
		writeQRError(w, err)
		return
	}
	userID, err := claimed.UserID.Int64()
	if err != nil {
		writeQRError(w, err)
		return
	}
	u, err := h.loadUser(r.Context(), userID)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, qrMessages[qrlogin.CodeNotFound], qrlogin.CodeNotFound)
		return
	}
	h.establishSession(w, r, u, accountKey(u, u.Username))
}

type qrScanRequest struct {
	ChallengeID      string `json:"challengeId"`
	Secret           string `json:"secret"`
	ScannerMetadata  any    `json:"scannerMetadata"`
	ApprovalMetadata any    `json:"approvalMetadata"`
}

// qrScan reads what the phone scanned; only a browser or the phone app may
// answer a sign-in request.
func (h *Handler) qrScan(w http.ResponseWriter, r *http.Request) (RequestIdentity, qrScanRequest, bool) {
	var body qrScanRequest
	identity, ok := h.accountIdentity(w, r)
	if !ok || !decodeJSON(w, r, &body) {
		return identity, body, false
	}
	body.ChallengeID, body.Secret = strings.TrimSpace(body.ChallengeID), strings.TrimSpace(body.Secret)
	if body.ChallengeID == "" || body.Secret == "" {
		writeError(w, http.StatusBadRequest, "Challenge id and QR secret are required", "MISSING_FIELDS")
		return identity, body, false
	}
	return identity, body, true
}

func (h *Handler) qrResolve(w http.ResponseWriter, r *http.Request) {
	_, body, ok := h.qrScan(w, r)
	if !ok {
		return
	}
	preview, err := h.qr.MarkScanned(r.Context(), body.ChallengeID, body.Secret, body.ScannerMetadata)
	if err != nil {
		writeQRError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *Handler) qrDecline(w http.ResponseWriter, r *http.Request) {
	_, body, ok := h.qrScan(w, r)
	if !ok {
		return
	}
	if err := h.qr.Decline(r.Context(), body.ChallengeID, body.Secret); err != nil {
		writeQRError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) qrApprove(w http.ResponseWriter, r *http.Request) {
	identity, body, ok := h.qrScan(w, r)
	if !ok {
		return
	}
	sessionHash := ""
	if !identity.IsDevice {
		sessionHash = sessionx.HashID(h.secret, sessionx.FromContext(r.Context()).ID())
	}
	challenge, err := h.qr.Approve(r.Context(), body.ChallengeID, body.Secret, userid.FromInt(identity.UserID), sessionHash, body.ApprovalMetadata)
	if err != nil {
		writeQRError(w, err)
		return
	}
	if identity.IsDevice {
		if _, err := h.db.ExecContext(r.Context(), `UPDATE user_qr_login_challenges SET approved_device_id = ? WHERE id = ?`, identity.DeviceID, challenge.ChallengeID); err != nil {
			writeQRError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, challenge)
}
