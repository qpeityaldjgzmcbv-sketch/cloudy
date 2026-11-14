package app

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo"
)

// Windows close must quit even when old settings request hiding to an unreachable tray.
func TestWindowsCloseQuitsWithOldTraySettings(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows close policy")
	}
	oldQuit := quitCloudy
	defer func() { quitCloudy = oldQuit }()
	called := 0
	quitCloudy = func() { called++ }
	for _, tray := range []*mygo.Tray{nil, new(mygo.Tray)} {
		for _, setting := range []bool{false, true} {
			a := &app{settings: settings{CloseQuits: setting}, tray: tray}
			a.closeMain(&mygo.CloseEvent{})
		}
	}
	if called != 4 {
		t.Fatalf("quit requested %d times, want 4", called)
	}
	// During normal quitting the framework closes windows again; do not restart quitting.
	a := &app{quitting: true}
	a.closeMain(&mygo.CloseEvent{})
	if called != 4 {
		t.Fatal("closing during quit requested another quit")
	}
}
