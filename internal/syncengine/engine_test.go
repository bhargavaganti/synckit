package syncengine

import (
	"testing"
	"time"

	"github.com/bhargav/synckit/internal/service"
)

func bundle(host, app string, t time.Time) service.Bundle {
	return service.Bundle{Name: host + ".zip", Apps: []string{app}, CreatedTime: t, OriginHost: host}
}

// TestComputeUpdates covers the seamless policy's core decision: a peer bundle
// only surfaces when it is newer than everything we hold AND came from another
// host (never our own bundle echoed back).
func TestComputeUpdates(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	me := "desktop"

	ov := service.Overview{
		LocalBundles: []service.Bundle{
			bundle(me, "chrome", now.Add(-2*time.Hour)), // our chrome watermark
			bundle(me, "firefox", now),                  // our firefox is current
		},
		Peers: []service.Peer{
			{Host: "laptop", IP: "100.0.0.2", Online: true, Serving: true, Bundles: []service.Bundle{
				bundle("laptop", "chrome", now),             // newer chrome → update
				bundle("laptop", "firefox", now.Add(-time.Hour)), // older firefox → no
			}},
			{Host: "self-echo", IP: "100.0.0.3", Online: true, Serving: true, Bundles: []service.Bundle{
				bundle(me, "chrome", now.Add(time.Hour)), // our own bundle on a peer → ignore
			}},
			{Host: "offline", IP: "100.0.0.4", Online: false, Serving: false, Bundles: []service.Bundle{
				bundle("offline", "dbeaver", now.Add(time.Hour)), // not serving → ignore
			}},
		},
	}

	ups := computeUpdates(me, ov)
	if len(ups) != 1 {
		t.Fatalf("expected exactly 1 update, got %d: %+v", len(ups), ups)
	}
	u := ups[0]
	if u.App != "chrome" || u.PeerHost != "laptop" {
		t.Fatalf("wrong update: %+v", u)
	}
}

// TestComputeUpdates_NoneWhenLocalNewer ensures we never nag when we already
// hold the newest copy.
func TestComputeUpdates_NoneWhenLocalNewer(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	me := "desktop"
	ov := service.Overview{
		LocalBundles: []service.Bundle{bundle(me, "chrome", now)},
		Peers: []service.Peer{{Host: "laptop", IP: "1", Online: true, Serving: true,
			Bundles: []service.Bundle{bundle("laptop", "chrome", now.Add(-time.Hour))}}},
	}
	if ups := computeUpdates(me, ov); len(ups) != 0 {
		t.Fatalf("expected no updates, got %+v", ups)
	}
}
