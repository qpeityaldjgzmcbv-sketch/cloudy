package app

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"netease-music-mygo/internal/controlbridge"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

func bridgeSong(t netease.Track) controlbridge.Song {
	return controlbridge.Song{ID: t.ID, Title: t.Name, Artist: t.ArtistNames(), Album: t.Album.Name, DurationMS: t.Duration}
}
func (a *app) bridgeStatus() controlbridge.Status {
	p := a.pl
	s := controlbridge.Status{Playing: p.Playing(), Loading: p.Loading(), Ready: p.Ready(), PositionMS: p.Position().Milliseconds(), DurationMS: p.Duration().Milliseconds(), Error: p.Err()}
	if t := p.Current(); t != nil {
		song := bridgeSong(*t)
		s.Track = &song
	}
	return s
}

// bridgeCall reads and changes player state only on MyGo's main thread. A
// canceled request queued while the window is busy never starts playback later.
func (a *app) bridgeCall(ctx context.Context, fn func() (any, error)) (any, error) {
	type reply struct {
		value any
		err   error
	}
	done := make(chan reply, 1)
	a.post(func() {
		if ctx.Err() != nil {
			return
		}
		if a.quitting {
			done <- reply{err: errors.New("Cloudy 正在退出")}
			return
		}
		v, e := fn()
		done <- reply{v, e}
	})
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Test seams use the same NetEase catalogue functions as Cloudy's own album page.
var bridgeAlbumDetail = netease.AlbumDetail
var bridgeAlbumSearch = func(q string) (netease.SearchResult, error) {
	return netease.SearchPage(q, netease.SearchAlbums, 20, 0)
}

func (a *app) playBridgeAlbum(tracks []netease.Track) (controlbridge.Status, error) {
	if len(tracks) == 0 {
		return controlbridge.Status{}, errors.New("网易云专辑没有可播放曲目")
	}
	a.pl.SetMode(player.ModeOrder)
	a.playList(tracks, false)
	return a.bridgeStatus(), nil
}

func (a *app) startControlBridge() {
	var playMu sync.Mutex

	status := func(ctx context.Context) (controlbridge.Status, error) {
		v, e := a.bridgeCall(ctx, func() (any, error) { return a.bridgeStatus(), nil })
		if e != nil {
			return controlbridge.Status{}, e
		}
		return v.(controlbridge.Status), nil
	}
	server, err := controlbridge.StartDefault(controlbridge.Backend{
		Listening: func(ctx context.Context, after int64, limit int) (any, error) {
			_, err := a.bridgeCall(ctx, func() (any, error) {
				a.sampleListening(false)
				a.saveListening(false)
				return nil, nil
			})
			if err != nil {
				return nil, err
			}
			w := a.listening.writer
			if w == nil {
				return nil, errors.New("聆听记录存储不可用")
			}
			if err := w.flush(); err != nil {
				return nil, err
			}
			return w.store.Read(after, limit)
		},
		Diagnostics: func(ctx context.Context) (any, error) {
			return a.bridgeCall(ctx, func() (any, error) {
				a.loadHistory()
				return map[string]any{"revision": "close-quits-20261113", "history_count": len(a.recent.played), "history_files": historyFileDiagnostics()}, nil
			})
		},
		SearchAlbums: func(ctx context.Context, q string) ([]controlbridge.Album, error) {
			page, err := bridgeAlbumSearch(q)
			if err != nil {
				return nil, err
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			albums := make([]controlbridge.Album, 0, len(page.Albums))
			for _, al := range page.Albums {
				year := 0
				if al.PublishTime > 0 {
					year = time.UnixMilli(al.PublishTime).Year()
				}
				albums = append(albums, controlbridge.Album{ID: al.ID, Title: al.Name, Artist: al.ArtistNames(), Year: year, TrackCount: al.Size})
			}
			return albums, nil
		},
		PlayAlbum: func(ctx context.Context, id int64) (controlbridge.Status, error) {
			playMu.Lock()
			defer playMu.Unlock()
			generation, err := a.bridgeCall(ctx, func() (any, error) { return a.accountGen, nil })
			if err != nil {
				return controlbridge.Status{}, err
			}
			al, err := bridgeAlbumDetail(id)
			if err != nil {
				return controlbridge.Status{}, err
			}
			if al == nil || al.ID != id {
				return controlbridge.Status{}, errors.New("网易云未返回这张专辑")
			}
			value, err := a.bridgeCall(ctx, func() (any, error) {
				if a.accountGen != generation.(int) {
					return nil, errors.New("网易云账号已改变，请重试")
				}
				return a.playBridgeAlbum(al.Songs)
			})
			if err != nil {
				return controlbridge.Status{}, err
			}
			return value.(controlbridge.Status), nil
		},
		Status: status,
		Search: func(ctx context.Context, q string) ([]controlbridge.Song, error) {
			tracks, err := netease.Search(q, 20)
			if err != nil {
				return nil, err
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			songs := make([]controlbridge.Song, 0, len(tracks))
			for _, t := range tracks {
				songs = append(songs, bridgeSong(t))
			}
			return songs, nil
		},
		Play: func(ctx context.Context, id int64) (controlbridge.Status, error) {
			playMu.Lock()
			defer playMu.Unlock()
			if ctx.Err() != nil {
				return controlbridge.Status{}, ctx.Err()
			}
			generation, err := a.bridgeCall(ctx, func() (any, error) { return a.accountGen, nil })
			if err != nil {
				return controlbridge.Status{}, err
			}
			tracks, err := netease.SongDetails([]int64{id})
			if err != nil {
				return controlbridge.Status{}, err
			}
			if len(tracks) != 1 || tracks[0].ID != id {
				return controlbridge.Status{}, errors.New("网易云未返回这首歌曲")
			}
			v, err := a.bridgeCall(ctx, func() (any, error) {
				if a.accountGen != generation.(int) {
					return nil, errors.New("网易云账号已改变，请重试")
				}
				a.pl.PlayQueue(tracks, 0)
				return a.bridgeStatus(), nil
			})
			if err != nil {
				return controlbridge.Status{}, err
			}
			return v.(controlbridge.Status), nil
		},
		Control: func(ctx context.Context, action string) (controlbridge.Status, error) {
			v, e := a.bridgeCall(ctx, func() (any, error) {
				if a.pl.Current() == nil {
					return nil, errors.New("没有正在播放的歌曲")
				}
				switch action {
				case "pause":
					if a.pl.Playing() {
						a.pl.TogglePlay()
					}
				case "resume":
					if !a.pl.Playing() {
						a.pl.TogglePlay()
					}
				case "next":
					a.pl.Next()
				case "previous":
					a.pl.Prev()
				}
				return a.bridgeStatus(), nil
			})
			if e != nil {
				return controlbridge.Status{}, e
			}
			return v.(controlbridge.Status), nil
		},
	})
	if err != nil {
		log.Printf("Album Shelf bridge: %v", err)
		return
	}
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { server.Close() })
}
