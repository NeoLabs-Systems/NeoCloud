package sync

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// TestRandomizedConvergence runs random edits, deletes, renames and moves
// on both sides between passes, then checks two things:
//   - nothing is ever lost: every content hash present before a pass is
//     still somewhere afterwards (local folder incl. conflict copies and
//     local trash, server files, server trash, server versions);
//   - once the edits stop, both sides converge to the same tree.
func TestRandomizedConvergence(t *testing.T) {
	seeds := 40
	if testing.Short() {
		seeds = 5
	}
	for seed := int64(1); seed <= int64(seeds); seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) { fuzzOnce(t, seed) })
	}
}

var (
	fuzzDirs  = []string{"", "x", "y", "x/z"}
	fuzzFiles = []string{"a.txt", "b.txt", "c.txt", "d.txt"}
)

func fuzzOnce(t *testing.T, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	h := newHarness(t, srv, config.Both)
	clock := time.Now().Add(-time.Hour)
	content := func() string {
		return fmt.Sprintf("seed %d content %d %s", seed, rng.Int63(), strings.Repeat("x", rng.Intn(64)))
	}
	randomPath := func() string {
		dir := fuzzDirs[rng.Intn(len(fuzzDirs))]
		name := fuzzFiles[rng.Intn(len(fuzzFiles))]
		if dir == "" {
			return name
		}
		return dir + "/" + name
	}
	localOp := func() {
		rel := randomPath()
		switch rng.Intn(6) {
		case 0, 1: // create or edit
			h.write(rel, content())
			clock = clock.Add(time.Second)
			os.Chtimes(h.path(rel), clock, clock)
		case 2: // delete a file
			os.Remove(h.path(rel))
		case 3: // delete a folder
			if dir := fuzzDirs[1+rng.Intn(len(fuzzDirs)-1)]; rng.Intn(3) == 0 {
				os.RemoveAll(h.path(dir))
			}
		case 4: // rename or move a file
			to := randomPath()
			if !h.exists(to) {
				os.MkdirAll(filepath.Dir(h.path(to)), 0o755)
				os.Rename(h.path(rel), h.path(to))
			}
		case 5: // rename a folder
			if !h.exists("y") {
				os.Rename(h.path("x"), h.path("y"))
			}
		}
	}
	remoteOp := func() {
		rel := randomPath()
		switch rng.Intn(5) {
		case 0, 1:
			srv.put("Shared/"+rel, content())
		case 2:
			srv.del("Shared/" + rel)
		case 3:
			srv.rename("Shared/"+rel, fuzzFiles[rng.Intn(len(fuzzFiles))])
		case 4:
			if rng.Intn(3) == 0 {
				srv.del("Shared/" + fuzzDirs[1+rng.Intn(len(fuzzDirs)-1)])
			}
		}
	}

	syncChecked := func() error {
		before := everyHash(t, h, srv)
		err := h.sync()
		after := everyHash(t, h, srv)
		for hash := range before {
			if !after[hash] {
				t.Fatalf("seed %d: content %s disappeared", seed, hash[:8])
			}
		}
		return err
	}

	for step := 0; step < 30; step++ {
		for i := rng.Intn(4); i >= 0; i-- {
			if rng.Intn(2) == 0 {
				localOp()
			} else {
				remoteOp()
			}
		}
		if err := syncChecked(); err != nil {
			var confirm *NeedsConfirmationError
			if !errors.As(err, &confirm) {
				t.Fatalf("seed %d step %d: %v", seed, step, err)
			}
			if _, err := h.engine.ConfirmDeletes(h.pair.ID); err != nil {
				t.Fatal(err)
			}
		}
	}

	var local, remote map[string]string
	for round := 0; round < 6; round++ {
		if err := syncChecked(); err != nil {
			var confirm *NeedsConfirmationError
			if !errors.As(err, &confirm) {
				t.Fatalf("seed %d settling: %v", seed, err)
			}
			h.engine.ConfirmDeletes(h.pair.ID)
			continue
		}
		local, remote = localTree(t, h.root), srv.files("Shared")
		if equalTrees(local, remote) {
			return
		}
	}
	t.Fatalf("seed %d did not converge:\nlocal  %v\nremote %v", seed, local, remote)
}

// everyHash is every content hash held locally (incl. trash) or by the server.
func everyHash(t *testing.T, h *harness, srv *fakeServer) map[string]bool {
	t.Helper()
	out := srv.everyHash()
	filepath.WalkDir(h.root, func(full string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(full, api.PartSuffix) || filepath.Base(full) == markerName {
			return nil
		}
		hash, err := api.HashFile(full)
		if err == nil {
			out[hash] = true
		}
		return nil
	})
	return out
}

// localTree maps the pair's files to hashes and folders to "/", leaving
// .neocloud out.
func localTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
		if err != nil || full == root {
			return err
		}
		rel, _ := filepath.Rel(root, full)
		rel = filepath.ToSlash(rel)
		if rel == metaDir {
			return filepath.SkipDir
		}
		if d.IsDir() {
			out[rel] = "/"
			return nil
		}
		hash, _ := api.HashFile(full)
		out[rel] = hash
		return nil
	})
	return out
}

func equalTrees(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	keys := make([]string, 0, len(a))
	for key := range a {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if b[key] != a[key] {
			return false
		}
	}
	return true
}
