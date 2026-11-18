package zelta

import (
	"time"

	"zelta.space/internal/match"
)

// Snapshot describes one existing snapshot, as Zelta listed it. (To take a
// snapshot, see Client.Snapshot.)
type Snapshot struct {
	// Name is the snapshot name ("@name").
	Name string
	// GUID identifies the snapshot across replicas and renames.
	GUID     string
	Creation time.Time
	// Used is the space only this snapshot holds, a lower bound on what
	// destroying it frees.
	Used int64
	// Written is the space written since the previous snapshot.
	Written int64
	// Referenced is the space the snapshot refers to, shared or not.
	Referenced int64
}

func snapshotOf(r *match.Row) Snapshot {
	return Snapshot{Name: r.Savepoint, GUID: r.GUID, Creation: time.Unix(r.Creation, 0), Used: r.Used, Written: r.Written, Referenced: r.Referenced}
}
