package app

import (
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"path/filepath"
	"testing"
)

// Album playback replaces the old queue, resets single/shuffle mode, and follows catalogue order.
func TestBridgeAlbumQueueOrder(t *testing.T) {
	for _, mode := range []player.PlayMode{player.ModeOne, player.ModeShuffle} {
		a := newTestApp()
		a.pl.SetMode(mode)
		tracks := []netease.Track{{ID: 3, Name: "disc1 track1"}, {ID: 1, Name: "disc1 track2"}, {ID: 2, Name: "disc2 track1"}}
		for i := range tracks {
			tracks[i].LocalPath = filepath.Join(t.TempDir(), "missing.flac")
		}
		status, err := a.playBridgeAlbum(tracks)
		if err != nil {
			t.Fatal(err)
		}
		if a.pl.Mode() != player.ModeOrder || status.Track == nil || status.Track.ID != 3 {
			t.Fatal("album did not start in sequence")
		}
		queue := a.pl.Queue()
		if len(queue) != 3 || queue[0].ID != 3 || queue[1].ID != 1 || queue[2].ID != 2 {
			t.Fatal("catalogue order changed")
		}
		a.pl.Next()
		if a.pl.Current().ID != 1 {
			t.Fatal("next did not continue the album")
		}
		a.pl.Next()
		if a.pl.Current().ID != 2 {
			t.Fatal("disc transition failed")
		}
	}
}

// Empty catalogues must leave the current queue untouched.
func TestBridgeEmptyAlbumPreservesQueue(t *testing.T) {
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 8}})
	if _, err := a.playBridgeAlbum(nil); err == nil {
		t.Fatal("empty album accepted")
	}
	if a.pl.Current().ID != 8 {
		t.Fatal("empty album changed queue")
	}
}
