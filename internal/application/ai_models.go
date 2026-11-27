package application

import (
	"database/sql"
	"errors"

	"github.com/NeoLabs-Systems/neocloud/internal/ai"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/settings"
)

// aiModelError answers a failed model call with the status, code and message
// the model layer chose.
func aiModelError(err error) error {
	var failed *settings.AIChatError
	if !errors.As(err, &failed) {
		return err
	}
	return &ai.Error{Status: settings.AIErrorStatus(err), Message: settings.AIErrorMessage(err), Code: failed.Code}
}

// aiTree gives AI the file operations it applies suggestions with. The file
// tree sits above AI in the import graph, so it is handed in here.
type aiTree struct{ *files.Handler }

func (t aiTree) UpdateTx(tx *sql.Tx, userID int64, nodeID string, name, parentID *string, numbered bool) (string, string, error) {
	moved, err := t.Handler.UpdateTx(tx, userID, nodeID, name, parentID, numbered)
	return moved.Name, moved.ParentID, err
}
