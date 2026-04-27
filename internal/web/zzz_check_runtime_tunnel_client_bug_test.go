package web

import "testing"

func TestRuntimeTunnelClientAllowsInsecureSkipVerifyWithoutCA(t *testing.T) {
	db := newTestDB(t)
	wm := newTestManager(db)
	if err := wm.StartTunnelClientRuntime("classic", "127.0.0.1:7443", "secret-token", "edge-node", "", "", true, false); err != nil {
		t.Fatalf("StartTunnelClientRuntime: %v", err)
	}
}
