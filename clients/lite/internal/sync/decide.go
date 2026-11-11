package sync

import "github.com/neolabs/neocloud/clients/lite/internal/config"

// Side is how one side of a path compares with the last synced state.
// Without a state row a side can only be Absent or New; with one it can
// only be Same, Changed or Deleted.
type Side int

const (
	SideAbsent Side = iota
	SideNew
	SideSame
	SideChanged
	SideDeleted
)

// Action is what the engine does for one path.
type Action int

const (
	None Action = iota
	Upload
	Download
	DeleteLocal
	DeleteRemote
	// Conflict renames the local file to a conflict copy and downloads the
	// remote one under the original name. In "both" mode the copy is then
	// uploaded like any new file.
	Conflict
	// Forget drops the state row and touches nothing on disk or server.
	Forget
)

func (a Action) String() string {
	return [...]string{"none", "upload", "download", "delete-local", "delete-remote", "conflict", "forget"}[a]
}

// Decide is the three-way merge table of docs/clients.md for one path.
// Equal content on both sides is settled before this is called, so a
// New/New or Changed/Changed pair here really differs.
func Decide(local, remote Side, direction string) Action {
	switch direction {
	case config.Up:
		return decideUp(local, remote)
	case config.Down:
		return decideDown(local, remote)
	}
	return decideBoth(local, remote)
}

func decideBoth(local, remote Side) Action {
	switch {
	case local == SideDeleted && remote == SideDeleted:
		return Forget
	case local == SideDeleted && remote == SideSame:
		return DeleteRemote
	case local == SideSame && remote == SideDeleted:
		return DeleteLocal
	// Remote delete + local edit → re-upload; local delete + remote edit →
	// download again. An edit always wins over a delete.
	case local == SideChanged && remote == SideDeleted:
		return Upload
	case local == SideDeleted && remote == SideChanged:
		return Download
	case (local == SideNew || local == SideChanged) && (remote == SideNew || remote == SideChanged):
		return Conflict
	case local == SideNew || local == SideChanged:
		return Upload
	case remote == SideNew || remote == SideChanged:
		return Download
	}
	return None
}

// Upload only: the local folder is the truth and nothing is ever written
// locally. Remote-only edits are left alone until the local file changes.
func decideUp(local, remote Side) Action {
	switch {
	case local == SideDeleted && remote == SideDeleted:
		return Forget
	case local == SideDeleted:
		return DeleteRemote
	case local == SideNew || local == SideChanged:
		return Upload
	case local == SideSame && remote == SideDeleted:
		return Upload
	}
	return None
}

// Download only: the server is the truth and nothing is ever written
// remotely. Local edits survive until the remote file changes, and then
// become a conflict copy rather than being overwritten.
func decideDown(local, remote Side) Action {
	switch {
	case remote == SideDeleted && local == SideSame:
		return DeleteLocal
	case remote == SideDeleted:
		return Forget
	case (local == SideNew || local == SideChanged) && (remote == SideNew || remote == SideChanged):
		return Conflict
	case remote == SideNew || remote == SideChanged:
		return Download
	case local == SideDeleted:
		return Download
	}
	return None
}
