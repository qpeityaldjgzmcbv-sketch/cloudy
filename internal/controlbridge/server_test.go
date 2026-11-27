package controlbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"netease-music-mygo/internal/listening"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// History uses the same authentication and bounds its incremental page size.
func TestListeningEndpoint(t *testing.T) {
	d := Descriptor{1, "http://127.0.0.1:12345", "secret", 1}
	calls := 0
	h := handler(d, Backend{Listening: func(_ context.Context, after int64, limit int) (any, error) {
		calls++
		if after != 12 || limit != 20 {
			t.Fatalf("after %d limit %d", after, limit)
		}
		return listening.Page{SourceID: "test", Cursor: 12, Events: []listening.Event{}}, nil
	}})
	for _, tc := range []struct {
		query, token string
		code         int
	}{
		{"?after=12&limit=20", "", 403},
		{"?after=-1&limit=20", "secret", 400},
		{"?after=12&limit=501", "secret", 400},
		{"?after=x&limit=20", "secret", 400},
		{"?after=12&limit=20", "secret", 200},
	} {
		r := httptest.NewRequest("GET", d.URL+"/v1/listening"+tc.query, nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s: %d", tc.query, w.Code)
		}
	}
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
}

// Local clients must authenticate; a website or a forged Host cannot play songs.
func TestAuthorization(t *testing.T) {
	d := Descriptor{1, "http://127.0.0.1:12345", strings.Repeat("a", 64), 1}
	calls := 0
	b := Backend{Play: func(_ context.Context, id int64) (Status, error) {
		calls++
		if id != 42 {
			t.Fatalf("id %d", id)
		}
		return Status{Playing: true}, nil
	}}
	h := handler(d, b)
	for _, tc := range []struct {
		token, origin, host string
		code                int
	}{
		{"", "", "127.0.0.1:12345", 403},
		{"Bearer " + d.Token, "https://example.com", "127.0.0.1:12345", 403},
		{"Bearer " + d.Token, "", "example.com", 403},
		{"Bearer " + d.Token, "", "127.0.0.1:12345", 200},
	} {
		req := httptest.NewRequest("POST", d.URL+"/v1/play", strings.NewReader(`{"song_id":42}`))
		req.Host = tc.host
		req.Header.Set("Authorization", tc.token)
		req.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("got %d want %d", w.Code, tc.code)
		}
	}
	if calls != 1 {
		t.Fatalf("played %d times", calls)
	}
}

// Malformed song IDs and controls never reach the player.
func TestInvalidCommands(t *testing.T) {
	d := Descriptor{1, "http://127.0.0.1:12345", "secret", 1}
	h := handler(d, Backend{})
	for _, body := range []string{`{"song_id":0}`, `{"song_id":-1}`, `{"song_id":"42"}`, `{"song_id":42,"unknown":true}`, `{"song_id":42} {"song_id":43}`, strings.Repeat("x", 3000)} {
		r := httptest.NewRequest("POST", d.URL+"/v1/play", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("status %d", w.Code)
		}
	}
}

// Discovery describes the live loopback server and is removed when it quits.
func TestDiscoveryLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	s, err := Start(path, Backend{Status: func(context.Context) (Status, error) { return Status{Ready: true}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d Descriptor
	if err = json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", d.URL+"/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+d.Token)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var status Status
	if err = json.NewDecoder(res.Body).Decode(&status); err != nil || !status.Ready {
		t.Fatalf("status %+v %v", status, err)
	}
	s.Close()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("discovery remains: %v", err)
	}
}

// An unwritable default must not silently disable playback in an otherwise running app.
func TestDiscoveryFallback(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "file")
	if err := os.WriteFile(blocked, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	fallback := filepath.Join(dir, "portable", "cloudy-bridge.json")
	s, err := startAvailable([]string{filepath.Join(blocked, "bridge.json"), fallback}, Backend{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.path != fallback {
		t.Fatal("did not use writable fallback")
	}
	raw, _ := os.ReadFile(blocked)
	if string(raw) != "preserve" {
		t.Fatal("changed existing file")
	}
}

// A restarting player replaces stale discovery; an old server cannot erase its successor.
func TestDiscoveryReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	first, err := Start(path, Backend{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Start(path, Backend{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	first.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d Descriptor
	if json.Unmarshal(raw, &d) != nil || d.Token != second.descriptor.Token {
		t.Fatal("lost current descriptor")
	}
}

// Album IDs are distinct from song IDs and share the authenticated local bridge.
func TestAlbumSearchAndPlay(t *testing.T) {
	d := Descriptor{1, "http://127.0.0.1:12345", strings.Repeat("a", 64), 1}
	played := int64(0)
	h := handler(d, Backend{
		SearchAlbums: func(_ context.Context, q string) ([]Album, error) {
			if q != "Album" {
				t.Fatalf("query %q", q)
			}
			return []Album{{ID: 7, Title: "Album", TrackCount: 3}}, nil
		},
		PlayAlbum: func(_ context.Context, id int64) (Status, error) {
			played = id
			return Status{Track: &Song{ID: 42}}, nil
		},
	})
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{
		{"GET", "/v1/albums?q=Album", "", 200},
		{"GET", "/v1/albums?q=", "", 400},
		{"POST", "/v1/play-album", `{"album_id":7}`, 200},
		{"POST", "/v1/play-album", `{"album_id":0}`, 400},
		{"POST", "/v1/play-album", `{"song_id":7}`, 400},
		{"POST", "/v1/play-album", `{"album_id":"7"}`, 400},
	} {
		req := httptest.NewRequest(tc.method, d.URL+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+d.Token)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if out.Code != tc.code {
			t.Fatalf("%s: %d", tc.path, out.Code)
		}
	}
	if played != 7 {
		t.Fatalf("played album %d", played)
	}
	// A website cannot use the new command to bypass authentication either.
	req := httptest.NewRequest("POST", d.URL+"/v1/play-album", strings.NewReader(`{"album_id":9}`))
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 403 || played != 7 {
		t.Fatal("unauthenticated album playback")
	}
}
