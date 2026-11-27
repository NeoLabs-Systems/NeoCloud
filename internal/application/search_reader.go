package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	"github.com/NeoLabs-Systems/neocloud/internal/searchindex"
)

var searchBlobIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func searchBlobReader(runtime *runtimeapp.Runtime) searchindex.BlobReader {
	return func(ctx context.Context, userID int64, node searchindex.Node) ([]byte, error) {
		if runtime == nil || runtime.DB == nil || runtime.Keys == nil || userID < 1 || !searchBlobIDPattern.MatchString(node.BlobID) {
			return nil, errors.New("searchindex: invalid encrypted blob reference")
		}
		if node.Size < 0 || node.Size > searchindex.MaxPDFSource {
			return nil, errors.New("searchindex: source exceeds read limit")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(runtime.Config.Paths.BlobDir, strconv.FormatInt(userID, 10), node.BlobID[:2], node.BlobID)
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("searchindex: encrypted blob is not a regular file")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		dek, err := runtime.Keys.GetUserDEK(ctx, runtime.DB.W, userID)
		if err != nil {
			return nil, err
		}
		plain := bytes.NewBuffer(make([]byte, 0, int(node.Size)))
		written, err := cryptox.DecryptBlobRange(file, node.Size, 0, maxInt64(0, node.Size-1), dek, node.BlobID, plain)
		if err != nil {
			return nil, fmt.Errorf("searchindex: decrypt source: %w", err)
		}
		if written != node.Size {
			return nil, errors.New("searchindex: decrypted source size does not match metadata")
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		return plain.Bytes(), nil
	}
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
