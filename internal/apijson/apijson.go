// Package apijson reads request bodies and writes failures the way the API
// answers them.
package apijson

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const bodyLimit = 1 << 20

// Object reads a JSON object body; a request without a body is an empty one.
func Object(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	body := map[string]any{}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, bodyLimit))
	err := decoder.Decode(&body)
	if errors.Is(err, io.EOF) {
		return map[string]any{}, nil
	}
	if err == nil && decoder.Decode(new(any)) != io.EOF {
		err = errors.New("data after the JSON value")
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, apperr.New(http.StatusRequestEntityTooLarge, "The request body is too large.", "BODY_TOO_LARGE")
	}
	if err != nil {
		return nil, apperr.New(http.StatusBadRequest, "The request body is not valid JSON.", "INVALID_JSON")
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, nil
}

type coded interface {
	error
	HTTPStatus() int
	ErrorCode() string
}

// Public returns the client-facing form of err, or nil when it is internal.
func Public(err error) *apperr.Error {
	if known, ok := apperr.As(err); ok {
		return known
	}
	var other coded
	if errors.As(err, &other) {
		return apperr.New(other.HTTPStatus(), other.Error(), other.ErrorCode())
	}
	return nil
}

// Fail answers err. Internal causes are logged and replaced by fallback.
func Fail(w http.ResponseWriter, err error, fallback string) {
	known := Public(err)
	if known == nil {
		slog.Error("request failed", "error", err)
		httpx.ErrorCode(w, http.StatusInternalServerError, fallback, "INTERNAL_ERROR")
		return
	}
	// A refused upload may still be sending its body; closing the connection
	// stops it.
	if known.Status == http.StatusRequestEntityTooLarge || known.Status == http.StatusInsufficientStorage {
		w.Header().Set("Connection", "close")
	}
	if known.Code == "" {
		httpx.Error(w, known.Status, known.Message)
		return
	}
	httpx.ErrorCode(w, known.Status, known.Message, known.Code)
}
