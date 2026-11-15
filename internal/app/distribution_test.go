package app

import (
	"path/filepath"
	"testing"
)

// A release switch must not move the portable history and queue with the executable.
func TestManagedPortablePaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLOUDY_PORTABLE_DIR", dir)
	history, err := historyFallbackPath()
	if err != nil || history != filepath.Join(dir, "cloudy-history.json") {
		t.Fatalf("history path: %q, %v", history, err)
	}
	session, err := sessionFallbackPath()
	if err != nil || session != filepath.Join(dir, "cloudy-session.json") {
		t.Fatalf("session path: %q, %v", session, err)
	}
}

// A personal bridge build must never install an upstream binary without the bridge.
func TestManagedBuildDisablesUpstreamUpdates(t *testing.T) {
	old := managedBuild
	managedBuild = "1"
	t.Cleanup(func() { managedBuild = old })
	a := &app{}
	a.useUpdater()
	a.startCheck(false)
	a.autoCheckUpdates()
	if a.updates || a.update.checking || a.update.open {
		t.Fatal("managed build enabled upstream updates")
	}
}
