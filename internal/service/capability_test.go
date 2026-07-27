package service

import (
	"testing"
	"time"
)

func cap(host, os string, profiles ...ProfileCap) Capability {
	return Capability{
		Machine: Machine{Hostname: host, OS: os},
		Apps: []AppCap{{
			ID: "firefox", Installed: true, SecretsCrossMachine: true, Profiles: profiles,
		}},
	}
}

// TestMatrixSyncState checks fingerprint reconciliation: identical fingerprints
// read "in sync"; differing ones read "differs" with the newest host named.
func TestMatrixSyncState(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)

	// Same fingerprint on both -> in sync.
	local := cap("desktop", "windows", ProfileCap{ID: "a.default-release", Role: "default-release", Fingerprint: "same", SnapshotAt: t0})
	peer := cap("laptop", "darwin", ProfileCap{ID: "z.default-release", Role: "default-release", Fingerprint: "same", SnapshotAt: t0})
	m := BuildMatrix(local, []Capability{peer})
	if len(m.Rows) != 1 || m.Rows[0].Sync != SyncInSync {
		t.Fatalf("expected in-sync, got %+v", m.Rows)
	}

	// Different fingerprints, laptop newer -> differs, newest=laptop.
	local = cap("desktop", "windows", ProfileCap{Role: "default-release", Fingerprint: "old", SnapshotAt: t0})
	peer = cap("laptop", "darwin", ProfileCap{Role: "default-release", Fingerprint: "new", SnapshotAt: t0.Add(time.Hour)})
	m = BuildMatrix(local, []Capability{peer})
	if m.Rows[0].Sync != SyncDiffers || m.Rows[0].NewestHost != "laptop" {
		t.Fatalf("expected differs/laptop, got sync=%s newest=%s", m.Rows[0].Sync, m.Rows[0].NewestHost)
	}
	// Firefox role matched across differing dir names -> full clone verdict.
	if m.Rows[0].Verdict != VerdictFull {
		t.Fatalf("expected full verdict, got %s", m.Rows[0].Verdict)
	}
}
