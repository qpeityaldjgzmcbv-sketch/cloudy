// Package controlbridge exposes Cloudy's player to local desktop clients.
package controlbridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Song is the stable, deliberately small view of a NetEase song.
type Song struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int64  `json:"duration_ms"`
}
type Album struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Year       int    `json:"year"`
	TrackCount int    `json:"track_count"`
}
type Status struct {
	Track      *Song  `json:"track"`
	Playing    bool   `json:"playing"`
	Loading    bool   `json:"loading"`
	Ready      bool   `json:"ready"`
	PositionMS int64  `json:"position_ms"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error"`
}
type Backend struct {
	Listening    func(context.Context, int64, int) (any, error)
	Diagnostics  func(context.Context) (any, error)
	SearchAlbums func(context.Context, string) ([]Album, error)
	PlayAlbum    func(context.Context, int64) (Status, error)
	Search       func(context.Context, string) ([]Song, error)
	Play         func(context.Context, int64) (Status, error)
	Status       func(context.Context) (Status, error)
	Control      func(context.Context, string) (Status, error)
}
type Descriptor struct {
	Version int    `json:"version"`
	URL     string `json:"url"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
}
type Server struct {
	http       *http.Server
	listener   net.Listener
	descriptor Descriptor
	path       string
}

func DefaultPath() string {
	if p := os.Getenv("CLOUDY_BRIDGE_FILE"); p != "" {
		return p
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "AlbumShelf", "cloudy-bridge.json")
}

// StartDefault uses a shared user path, falling back beside the executable when
// that directory is unavailable. Explicit overrides remain isolated.
func StartDefault(b Backend) (*Server, error) {
	if os.Getenv("CLOUDY_BRIDGE_FILE") != "" {
		return Start(DefaultPath(), b)
	}
	exe, err := os.Executable()
	if err != nil {
		return Start(DefaultPath(), b)
	}
	return startAvailable([]string{DefaultPath(), filepath.Join(filepath.Dir(exe), "cloudy-bridge.json")}, b)
}

func startAvailable(paths []string, b Backend) (*Server, error) {
	var failures []error
	for _, path := range paths {
		s, err := Start(path, b)
		if err == nil {
			return s, nil
		}
		failures = append(failures, err)
	}
	return nil, errors.Join(failures...)
}

// Start binds exclusively to IPv4 loopback and publishes a fresh per-run secret.
func Start(path string, b Backend) (*Server, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		listener.Close()
		return nil, err
	}
	d := Descriptor{1, "http://" + listener.Addr().String(), hex.EncodeToString(secret), os.Getpid()}
	s := &Server{listener: listener, descriptor: d, path: path}
	s.http = &http.Server{Handler: handler(d, b), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		listener.Close()
		return nil, err
	}
	raw, _ := json.Marshal(d)
	// A unique temporary file also avoids collisions with a dying previous process.
	f, err := os.CreateTemp(filepath.Dir(path), ".cloudy-bridge-*")
	if err == nil {
		err = f.Chmod(0600)
		if err == nil {
			_, err = f.Write(raw)
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(f.Name(), path)
		}
		if err != nil {
			os.Remove(f.Name())
		}
	}
	if err != nil {
		listener.Close()
		return nil, err
	}
	go s.http.Serve(listener)
	return s, nil
}
func (s *Server) Close() {
	s.http.Close()
	raw, err := os.ReadFile(s.path)
	var d Descriptor
	if err == nil && json.Unmarshal(raw, &d) == nil && d.Token == s.descriptor.Token {
		os.Remove(s.path)
	}
}
func handler(d Descriptor, b Backend) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		// No CORS: browser pages cannot command the player's authenticated API.
		if r.Host != strings.TrimPrefix(d.URL, "http://") || r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+d.Token)) != 1 {
			fail(w, 403, "连接未授权")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var result any
		var err error
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/listening":
			if b.Listening == nil {
				fail(w, 404, "聆听记录接口不可用")
				return
			}
			after, parseErr := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
			limit, limitErr := strconv.Atoi(r.URL.Query().Get("limit"))
			if parseErr != nil || limitErr != nil || after < 0 || limit < 1 || limit > 500 {
				fail(w, 400, "聆听记录游标无效")
				return
			}
			result, err = b.Listening(ctx, after, limit)
		case r.Method == "GET" && r.URL.Path == "/v1/diagnostics":
			if b.Diagnostics == nil {
				fail(w, 404, "诊断接口不可用")
				return
			}
			result, err = b.Diagnostics(ctx)
		case r.Method == "GET" && r.URL.Path == "/v1/status":
			result, err = b.Status(ctx)
		case r.Method == "GET" && (r.URL.Path == "/v1/search" || r.URL.Path == "/v1/albums"):
			q := strings.TrimSpace(r.URL.Query().Get("q"))
			if q == "" || len(q) > 1000 {
				fail(w, 400, "搜索词无效")
				return
			}
			if r.URL.Path == "/v1/albums" {
				if b.SearchAlbums == nil {
					fail(w, 404, "专辑搜索不可用")
					return
				}
				result, err = b.SearchAlbums(ctx, q)
			} else {
				result, err = b.Search(ctx, q)
			}
		case r.Method == "POST" && (r.URL.Path == "/v1/play" || r.URL.Path == "/v1/control" || r.URL.Path == "/v1/play-album"):
			var body struct {
				SongID  int64  `json:"song_id"`
				AlbumID int64  `json:"album_id"`
				Action  string `json:"action"`
			}
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
			dec.DisallowUnknownFields()
			if err = dec.Decode(&body); err != nil {
				fail(w, 400, "播放请求无效")
				return
			}
			if dec.Decode(new(any)) != io.EOF {
				fail(w, 400, "播放请求无效")
				return
			}
			if r.URL.Path == "/v1/play-album" {
				if body.AlbumID <= 0 {
					fail(w, 400, "专辑编号无效")
					return
				}
				if b.PlayAlbum == nil {
					fail(w, 404, "整专播放不可用，请重开更新后的 Cloudy")
					return
				}
				result, err = b.PlayAlbum(ctx, body.AlbumID)
			} else if r.URL.Path == "/v1/play" {
				if body.SongID <= 0 {
					fail(w, 400, "歌曲编号无效")
					return
				}
				result, err = b.Play(ctx, body.SongID)
			} else {
				switch body.Action {
				case "pause", "resume", "next", "previous":
					result, err = b.Control(ctx, body.Action)
				default:
					fail(w, 400, "播放操作无效")
					return
				}
			}
		default:
			fail(w, 404, "接口不存在")
			return
		}
		if err != nil {
			code := 502
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				code = 504
			}
			fail(w, code, err.Error())
			return
		}
		json.NewEncoder(w).Encode(result)
	})
}
func fail(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
