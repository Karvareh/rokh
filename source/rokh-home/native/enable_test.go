package native

import "testing"

// A restored runtime lists the app it holds; a disabled one must be enabled
// as it is before its cell answers (the host's CellDisabled after restore).
// Synthetic list-apps answers in the shapes a conductor gives.
func TestARestoredAppIsEnabledUnlessItRuns(t *testing.T) {
	cases := []struct {
		name, listed, status string
		enable               bool
	}{
		{"running, tagged", `[{"installed_app_id":"rokh","status":{"type":"running"}}]`, "running", false},
		{"enabled, tagged", `[{"installed_app_id":"rokh","status":{"type":"enabled"}}]`, "enabled", false},
		{"disabled with a reason", `[{"installed_app_id":"rokh","status":{"type":"disabled","value":{"reason":{"type":"never_started"}}}}]`, "disabled", true},
		{"paused", `[{"installed_app_id":"rokh","status":{"type":"paused","value":{}}}]`, "paused", true},
		{"a plain word", `[{"installed_app_id":"rokh","status":"Disabled"}]`, "disabled", true},
		{"an externally tagged status", `[{"installed_app_id":"rokh","status":{"Disabled":{"reason":"NeverStarted"}}}]`, "disabled", true},
		{"another app runs, this one is disabled", `[{"installed_app_id":"other","status":{"type":"running"}},{"installed_app_id":"rokh","status":{"type":"disabled"}}]`, "disabled", true},
		{"no status said", `[{"installed_app_id":"rokh"}]`, "", true},
	}
	for _, c := range cases {
		got := appStatus(c.listed, "rokh")
		if got != c.status || needsEnabling(got) != c.enable {
			t.Errorf("%s: status %q (want %q), enable %v (want %v)", c.name, got, c.status, needsEnabling(got), c.enable)
		}
	}
}
