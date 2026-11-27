package application

import (
	"encoding/json"

	"github.com/NeoLabs-Systems/neocloud/internal/searchindex"
)

// searchIndexStatus shapes the index status for the administrator API.
func searchIndexStatus(status searchindex.Status, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(encoded, &out)
}
