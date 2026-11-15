package app

import "testing"

// Closing hides Cloudy on Windows when its tray icon exists, regardless of old settings.
func TestCloseHidesToTrayWhenAvailable(t *testing.T) {
	for _, test := range []struct {
		goos                       string
		closeQuits, tray, wantHide bool
	}{
		{"windows", false, true, true},
		{"windows", true, true, true},
		{"windows", false, false, false},
		{"linux", false, true, true},
		{"linux", true, true, false},
		{"darwin", true, false, false},
	} {
		if got := hidesOnClose(test.goos, test.closeQuits, test.tray); got != test.wantHide {
			t.Errorf("hidesOnClose(%q, %t, %t) = %t, want %t", test.goos, test.closeQuits, test.tray, got, test.wantHide)
		}
	}
}
