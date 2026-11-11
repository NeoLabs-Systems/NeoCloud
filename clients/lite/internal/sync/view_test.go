package sync

import (
	"context"
	"sort"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

func TestRemoteViewSkipsVaultsAndIgnored(t *testing.T) {
	tree := &api.Tree{RootID: "root", Nodes: []api.Node{
		{ID: "root", Type: "folder", Path: ""},
		{ID: "s", ParentID: "root", Type: "folder", Name: "Shared", Path: "Shared"},
		{ID: "a", ParentID: "s", Type: "file", Name: "a.txt", Path: "Shared/a.txt"},
		{ID: "v", ParentID: "s", Type: "folder", Name: "Secret", Path: "Shared/Secret", VaultID: "vault1"},
		{ID: "v1", ParentID: "v", Type: "file", Name: "x", Path: "Shared/Secret/x", VaultID: "vault1"},
		{ID: "d", ParentID: "s", Type: "file", Name: ".DS_Store", Path: "Shared/.DS_Store"},
		{ID: "o", ParentID: "root", Type: "file", Name: "outside.txt", Path: "outside.txt"},
		{ID: "sx", ParentID: "root", Type: "folder", Name: "SharedX", Path: "SharedX"},
	}}
	// "shared" in lower case: the server compares names without case.
	view, err := buildRemoteView(context.Background(), nil, tree, "shared", false, NewMatcher(nil))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for rel := range view.nodes {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	if len(paths) != 1 || paths[0] != "a.txt" || view.rootID != "s" {
		t.Errorf("view = %v (root %s), want only a.txt under s", paths, view.rootID)
	}
	if !view.inVault("Secret") || !view.inVault("Secret/new.txt") || view.inVault("Secrets") {
		t.Error("vault paths not excluded")
	}
}

func TestRemoteViewRefusesVanishedRoot(t *testing.T) {
	tree := &api.Tree{RootID: "root", Nodes: []api.Node{{ID: "root", Type: "folder"}}}
	// A pair that synced before must not treat a missing folder as
	// "everything was deleted".
	if _, err := buildRemoteView(context.Background(), nil, tree, "Gone", false, NewMatcher(nil)); err != errRemoteRootMissing {
		t.Errorf("err = %v, want errRemoteRootMissing", err)
	}
	vault := &api.Tree{RootID: "root", Nodes: []api.Node{{ID: "v", Type: "folder", Path: "Vault", VaultID: "x"}}}
	if _, err := buildRemoteView(context.Background(), nil, vault, "Vault", false, NewMatcher(nil)); err == nil {
		t.Error("a vault folder was accepted as a pair root")
	}
}
