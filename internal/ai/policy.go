package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// features are the account's AI switches (setting ai_features). On-demand
// features are on once the person opted in; anything that acts without being
// asked, or makes something new in their tree, is off until switched on.
type features map[string]bool

var featureDefaults = features{
	"names": true, "folders": true, "tags": true, "summaries": true,
	"onUpload": false, "autoApplyHighConfidence": false, "newTags": true, "newFolders": false,
}

// jobKinds are what a job can suggest; each is also the name of its switch.
var jobKinds = []string{"names", "folders", "tags"}

type fileTypeCategory struct {
	ID         string   `json:"id"`
	Extensions []string `json:"extensions"`
}

// The file types a person can leave out of every AI feature. "other" is every
// file whose extension is in no category.
var fileTypeCategories = []fileTypeCategory{
	{"images", []string{"jpg", "jpeg", "png", "heic", "heif", "webp", "gif", "tiff", "tif", "bmp", "avif", "svg", "raw", "dng", "cr2", "cr3", "nef", "arw", "orf", "rw2", "raf"}},
	{"videos", []string{"mp4", "mov", "m4v", "avi", "mkv", "webm", "wmv", "3gp", "mts"}},
	{"documents", []string{"pdf", "docx", "doc", "odt", "rtf", "txt", "md", "pages", "epub"}},
	{"spreadsheets", []string{"xlsx", "xls", "ods", "csv", "tsv", "numbers"}},
	{"presentations", []string{"pptx", "ppt", "odp", "key"}},
	{"archives", []string{"zip", "rar", "7z", "tar", "gz", "tgz", "bz2", "xz", "dmg", "iso"}},
	{"audio", []string{"mp3", "m4a", "wav", "flac", "aac", "ogg", "opus", "aiff", "wma"}},
	{"code", []string{"js", "ts", "jsx", "tsx", "py", "java", "kt", "swift", "go", "rs", "c", "h", "cpp", "cs", "rb", "php", "sh", "dart", "html", "css", "json", "yaml", "yml", "xml", "sql", "toml"}},
	{"other", []string{}},
}

var fileExtension = regexp.MustCompile(`\.([^./\\]+)$`)

// MonthlyTokenBudget parses the positive-integer-only budget setting.
func MonthlyTokenBudget(raw string) int64 {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

type account struct {
	id       int64
	settings map[string]any
}

func readSettings(ctx context.Context, q querier, userID int64) (account, error) {
	rows, err := queryAll(ctx, q, func(row scanner) ([2]string, error) {
		var pair [2]string
		err := row.Scan(&pair[0], &pair[1])
		return pair, err
	}, `SELECT key,value FROM user_settings WHERE user_id=?`, userID)
	if err != nil {
		return account{}, err
	}
	result := account{id: userID, settings: make(map[string]any, len(rows))}
	for _, pair := range rows {
		var value any
		if json.Unmarshal([]byte(pair[1]), &value) != nil {
			value = pair[1]
		}
		result.settings[pair[0]] = value
	}
	return result, nil
}

func (a account) optedIn() bool { return a.settings["ai_opt_in"] == true }

func (a account) features() features {
	result := features{}
	stored, _ := a.settings["ai_features"].(map[string]any)
	for key, value := range featureDefaults {
		if set, ok := stored[key].(bool); ok {
			value = set
		}
		result[key] = value
	}
	return result
}

func (a account) text(key string) string {
	value, _ := a.settings[key].(string)
	return value
}

// skipsType says whether the person left files of this name's type out of AI.
func (a account) skipsType(name string) bool {
	skip, _ := a.settings["ai_skip_types"].(map[string]any)
	categories, _ := skip["categories"].([]any)
	extensions, _ := skip["extensions"].([]any)
	if len(categories) == 0 && len(extensions) == 0 {
		return false
	}
	extension := ""
	if match := fileExtension.FindStringSubmatch(path.Base(name)); match != nil {
		extension = strings.ToLower(match[1])
	}
	if extension != "" && slices.Contains(extensions, any(extension)) {
		return true
	}
	category := "other"
	for _, item := range fileTypeCategories {
		if slices.Contains(item.Extensions, extension) {
			category = item.ID
			break
		}
	}
	return slices.Contains(categories, any(category))
}

func (a account) assertFeature(feature string) error {
	if !a.features()[feature] {
		return fail(http.StatusForbidden, "This AI feature is turned off in Settings › AI.", "AI_FEATURE_OFF")
	}
	return nil
}

func (a account) assertTypeAllowed(name string) error {
	if a.skipsType(name) {
		return fail(http.StatusUnprocessableEntity, "AI is turned off for this kind of file in Settings › AI.", "AI_TYPE_SKIPPED")
	}
	return nil
}

// budget is this month's tokens and the account's limit; 0 is no limit.
func (h *Handler) budget(ctx context.Context, q querier, userID int64) (used, limit int64, err error) {
	if err = q.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_tokens),0) FROM ai_usage WHERE user_id=? AND created_at>=strftime('%Y-%m-01 00:00:00','now')`, userID).Scan(&used); err != nil {
		return 0, 0, err
	}
	var override sql.NullInt64
	err = q.QueryRowContext(ctx, `SELECT ai_monthly_tokens FROM users WHERE id=?`, userID).Scan(&override)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, err
	}
	if override.Valid {
		return used, max(override.Int64, 0), nil
	}
	_, limit = h.policy()
	return used, limit, nil
}

// assertUsable refuses unless AI is on here and the person opted in.
func (h *Handler) assertUsable(settings account) error {
	if enabled, _ := h.policy(); !enabled {
		return fail(http.StatusForbidden, "AI features aren’t turned on.", "AI_DISABLED")
	}
	if !settings.optedIn() {
		return fail(http.StatusForbidden, "Turn on AI features in Settings › AI first.", "AI_NOT_OPTED_IN")
	}
	return nil
}

func (h *Handler) assertWithinBudget(ctx context.Context, userID int64) error {
	used, limit, err := h.budget(ctx, h.db, userID)
	if err != nil {
		return err
	}
	if limit > 0 && used >= limit {
		return fail(http.StatusTooManyRequests, "This month’s AI budget is used up. It resets on the 1st.", "AI_BUDGET_EXCEEDED")
	}
	return nil
}

// pauseReason is why AI can't run for the account now, or "".
func (h *Handler) pauseReason(ctx context.Context, userID int64) (string, error) {
	settings, err := readSettings(ctx, h.db, userID)
	if err != nil {
		return "", err
	}
	err = h.assertUsable(settings)
	if err == nil {
		err = h.assertWithinBudget(ctx, userID)
	}
	var refusal *Error
	if errors.As(err, &refusal) {
		return refusal.Code, nil
	}
	return "", err
}
