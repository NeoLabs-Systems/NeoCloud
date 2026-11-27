package agentapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// Rendered documents, and the file operations signature requests are built on.

// RenderPDF renders markdown (a JSON value) to a PDF, with a signature block
// when signers are named.
func (h *Handler) RenderPDF(title string, markdown any, signers []string) ([]byte, []signpdf.SignatureBoxes, error) {
	text, ok := markdown.(string)
	if !ok {
		return nil, nil, fileError{http.StatusBadRequest, "markdown is required.", "INVALID_REQUEST"}
	}
	tooLong := fileError{http.StatusRequestEntityTooLarge, "The text is too long (200 KB at most).", "TOO_LARGE"}
	if len(text) > signpdf.MaxMarkdownBytes {
		return nil, nil, tooLong
	}
	pdf, boxes, err := signpdf.RenderMarkdown(title, text, signers, "en")
	var refused *signpdf.Error
	if errors.As(err, &refused) && (refused.Code == signpdf.CodeTooLarge || refused.Code == signpdf.CodeTooManyPages) {
		err = tooLong
	}
	return pdf, boxes, err
}

// CreateAt writes a server-made file at a user-visible path and returns its
// node id. With overwrite an existing file gets a new version.
func (h *Handler) CreateAt(ctx context.Context, userID int64, filePath string, data []byte, overwrite bool) (string, error) {
	parts, err := h.pathParts(filePath)
	if err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return "", fileError{http.StatusBadRequest, "path must name a file.", "INVALID_PATH"}
	}
	name, err := validateAgentName(parts[len(parts)-1])
	if err != nil {
		return "", err
	}
	parentID, _, err := h.ensureFolderPath(ctx, userID, parts[:len(parts)-1])
	if err != nil {
		return "", err
	}
	item, err := h.storeBytes(ctx, userID, parentID, name, data, overwrite, "")
	return item.ID, err
}

// EnsureFolder returns the folder of that name in the root, making it when it
// is missing.
func (h *Handler) EnsureFolder(ctx context.Context, userID int64, name string) (string, error) {
	id, _, err := h.ensureFolderPath(ctx, userID, []string{name})
	return id, err
}

// SourceFile is a file of the account with its content.
type SourceFile struct {
	Name, ParentPath string
	Data             []byte
}

// ReadFile reads a file outside every vault; anything else is 404. A file over
// limit is refused with tooLarge.
func (h *Handler) ReadFile(ctx context.Context, userID int64, id string, limit int64, tooLarge string) (SourceFile, error) {
	item, err := h.itemAt(ctx, userID, id)
	if err != nil {
		return SourceFile{}, err
	}
	if item.Type != "file" {
		return SourceFile{}, fileError{http.StatusBadRequest, "That is not a file.", "NOT_A_FILE"}
	}
	if item.Size > limit {
		return SourceFile{}, fileError{http.StatusRequestEntityTooLarge, tooLarge, "DOCUMENT_TOO_LARGE"}
	}
	data, err := h.readContent(ctx, userID, item)
	return SourceFile{Name: item.Name, ParentPath: path.Dir(item.Path), Data: data}, err
}

func (h *Handler) readContent(ctx context.Context, userID int64, item node) ([]byte, error) {
	var blobID string
	if err := h.db.QueryRowContext(ctx, `SELECT blob_id FROM nodes WHERE id=? AND user_id=?`, item.ID, userID).Scan(&blobID); err != nil {
		return nil, err
	}
	if len(blobID) != 36 || strings.Trim(blobID, "0123456789abcdef-") != "" {
		return nil, errors.New("invalid blob id")
	}
	if item.Size == 0 {
		return []byte{}, nil
	}
	dek, err := h.keys.GetUserDEK(ctx, h.db, userID)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(h.blobFile(userID, blobID))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var plain bytes.Buffer
	if _, err = cryptox.DecryptBlobRange(file, item.Size, 0, item.Size-1, dek, blobID, &plain); err != nil {
		return nil, err
	}
	if int64(plain.Len()) != item.Size {
		return nil, errors.New("blob size mismatch")
	}
	return plain.Bytes(), nil
}

func (h *Handler) createPDF(w http.ResponseWriter, r *http.Request, userID int64) {
	body, ok := decodeWriteBody(w, r)
	if !ok {
		return
	}
	item, err := func() (node, error) {
		title, _, err := bodyString(body, "title")
		if err != nil {
			title = ""
		}
		var markdown any
		if value, set, err := bodyString(body, "markdown"); set && err == nil {
			markdown = value
		}
		pdf, _, err := h.RenderPDF(title, markdown, nil)
		if err != nil {
			return node{}, err
		}
		filePath, _, err := bodyString(body, "path")
		if err != nil {
			return node{}, err
		}
		overwrite, err := bodyBool(body, "overwrite", false)
		if err != nil {
			return node{}, err
		}
		id, err := h.CreateAt(r.Context(), userID, filePath, pdf, overwrite)
		if err != nil {
			return node{}, err
		}
		return h.itemAt(r.Context(), userID, id)
	}()
	if err != nil {
		writeFileError(w, err)
		return
	}
	item.Revision = ""
	httpx.WriteJSON(w, http.StatusCreated, item)
}
