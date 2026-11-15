package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The 百科 pane of Now Playing: what NetEase's song wiki says of the song
// playing, as its app shows it, with the songs and playlists like it.

var fetchSongWiki = netease.SongWikiOf

// wikiState is the wiki of one song, as it loads.
type wikiState struct {
	track   int64
	wiki    *netease.SongWiki
	loading bool
	err     string
	scroll  ui.ScrollState
	opening int64 // the similar song being looked up to play
}

// wikiShown reports whether tr has a wiki: a song of NetEase's.
func wikiShown(tr *netease.Track) bool {
	return tr != nil && tr.ID > 0 && tr.LocalPath == "" && tr.ProgramID == 0
}

// wikiFor returns the wiki of tr, loading it the first time.
func (a *app) wikiFor(tr *netease.Track) *wikiState {
	ws := &a.more.wiki
	if ws.track != tr.ID {
		*ws = wikiState{track: tr.ID}
		a.loadWiki()
	}
	return ws
}

func (a *app) loadWiki() {
	ws := &a.more.wiki
	if ws.loading {
		return
	}
	id := ws.track
	ws.loading, ws.err = true, ""
	go func() {
		w, err := fetchSongWiki(id)
		a.post(func() {
			if ws.track != id {
				return
			}
			ws.loading = false
			if err != nil {
				ws.err = "百科加载失败"
				return
			}
			ws.wiki = w
		})
	}()
}

// playSimilar plays a song the wiki says is like the one playing, once
// NetEase has said what it is.
func (a *app) playSimilar(s netease.WikiSong) {
	ws := &a.more.wiki
	if ws.opening != 0 {
		return
	}
	ws.opening = s.ID
	go func() {
		tracks, err := trackDetails([]int64{s.ID})
		a.post(func() {
			ws.opening = 0
			if err != nil || len(tracks) == 0 {
				a.say("wiki", "没能播放「"+s.Name+"」")
				return
			}
			a.pl.PlayNext(tracks[:1])
			a.pl.Next()
		})
	}()
}

func (a *app) wikiPane(c *ui.Context, tr *netease.Track) {
	if !wikiShown(tr) {
		wikiNote(c, "本地歌曲和节目没有百科")
		return
	}
	ws := a.wikiFor(tr)
	w := ws.wiki
	if w == nil {
		if ws.err != "" {
			ui.Column(c).Grow(1).Center().Gap(14).Children(func() {
				ui.Text(c, ws.err).FontSize(15).TextColor(white.Alpha(0.5))
				if pillButton(c.Key("retry"), iconInfo, "重试", white.Alpha(0.14), white).Clicked() {
					a.loadWiki()
				}
			})
			return
		}
		ui.Column(c).Grow(1).Padding(4, 8, 0, 0).Gap(10).Children(func() {
			s := npShimmer(c)
			s.text(c, 13, 1.3).Width(72).Margin(18, 12, 6, 12)
			for i := 0; i < 5; i++ {
				s.text(c, 14, 1.4).Width(float32(220+40*(i%3))).Margin(0, 12, 0, 12)
			}
		})
		return
	}
	if w.Empty() {
		wikiNote(c, "这首歌还没有百科")
		return
	}
	scroll := ui.Scroll(c.Key("wiki")).Grow(1).TrackScroll(&ws.scroll).Padding(4, 8, 48, 0)
	scroll.Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			if len(w.Facts) > 0 || len(w.Shows) > 0 || w.Sheets > 0 {
				commentHeading(c, "音乐百科")
				for _, f := range w.Facts {
					wikiFact(c, f.Label, f.Values)
				}
				if len(w.Shows) > 0 {
					wikiFact(c, "影视节目", w.Shows)
				}
			}
			if len(w.Awards) > 0 {
				commentHeading(c, "获奖成就")
				for i, l := range w.Awards {
					wikiLine(c.Key(i), l.Title, l.Detail)
				}
			}
			if w.Review != nil {
				commentHeading(c, "乐评")
				ui.Column(c).Padding(12).Margin(0, 0, 0, 0).Gap(8).Radius(14).Background(white.Alpha(0.06)).Children(func() {
					ui.Text(c, w.Review.Text).FontSize(14).LineHeight(1.6).TextColor(white.Alpha(0.86))
					if w.Review.By != "" {
						ui.Text(c, "—— "+w.Review.By).FontSize(12).TextColor(white.Alpha(0.5)).AlignSelf(ui.End)
					}
				})
			}
			if len(w.Memory) > 0 {
				commentHeading(c, "回忆坐标")
				for i, l := range w.Memory {
					wikiLine(c.Key(i), l.Title, l.Detail)
				}
			}
			if len(w.Similar) > 0 {
				commentHeading(c, "相似歌曲")
				for _, s := range w.Similar {
					a.wikiSong(c.Key(s.ID), s, ws.opening == s.ID)
				}
			}
			if len(w.Playlists) > 0 {
				commentHeading(c, "相关歌单")
				for _, p := range w.Playlists {
					a.wikiPlaylist(c.Key(p.ID), p)
				}
			}
		})
	})
}

func wikiNote(c *ui.Context, text string) {
	ui.Column(c).Grow(1).Center().Gap(14).Children(func() {
		ui.Icon(c, iconInfo).Size(40, 40).TextColor(white.Alpha(0.3))
		ui.Text(c, text).FontSize(17).FontWeight(600).TextColor(white.Alpha(0.45))
	})
}

// wikiFact is a label and its values, the values as chips.
func wikiFact(c *ui.Context, label string, values []string) {
	ui.Row(c).Padding(8, 12).Gap(14).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).FontSize(13).TextColor(white.Alpha(0.5)).Width(64).Shrink(0).Margin(4, 0, 0, 0)
		ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Wrap().Gap(6).Children(func() {
			for _, v := range values {
				ui.Text(c, v).FontSize(13).FontWeight(600).TextColor(white.Alpha(0.88)).
					Padding(3, 10).Radius(999).Background(white.Alpha(0.08))
			}
		})
	})
}

func wikiLine(c *ui.Context, title, detail string) {
	ui.Column(c).Padding(8, 12).Gap(3).Children(func() {
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(white.Alpha(0.88))
		if detail != "" {
			ui.Text(c, detail).FontSize(12.5).TextColor(white.Alpha(0.55))
		}
	})
}

func (a *app) wikiSong(c *ui.Context, s netease.WikiSong, opening bool) {
	row := ui.ButtonBase(c).Padding(8, 12).Gap(12).Radius(14).AlignItems(ui.Center).FocusRing(false).
		Label("播放 " + s.Name).Tooltip("播放")
	if row.Hovered() {
		row.Background(white.Alpha(0.06))
	}
	row.Children(func() {
		a.cover(c, s.CoverURL, 96, 40, 8).Shrink(0)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, s.Name).FontSize(14).FontWeight(600).TextColor(white.Alpha(0.9)).SingleLine()
			ui.Text(c, strings.ReplaceAll(s.Artist, "/", " / ")).FontSize(12).TextColor(white.Alpha(0.55)).SingleLine()
		})
		if opening {
			ui.Text(c, "正在打开…").FontSize(12).TextColor(white.Alpha(0.5))
		} else if row.Hovered() {
			ui.Icon(c, iconPlay).Size(16, 16).TextColor(white.Alpha(0.8))
		}
	})
	if row.Clicked() {
		a.playSimilar(s)
	}
}

func (a *app) wikiPlaylist(c *ui.Context, p netease.WikiPlaylist) {
	row := ui.ButtonBase(c).Padding(8, 12).Gap(12).Radius(14).AlignItems(ui.Center).FocusRing(false).Label("打开歌单 " + p.Name)
	if row.Hovered() {
		row.Background(white.Alpha(0.06))
	}
	row.Children(func() {
		a.cover(c, p.CoverURL, 96, 40, 8).Shrink(0)
		ui.Text(c, p.Name).FontSize(14).FontWeight(600).TextColor(white.Alpha(0.9)).SingleLine().Grow(1).Shrink(1).MinWidth(0)
	})
	if row.Clicked() {
		a.open(route{page: pagePlaylist, id: p.ID})
	}
}
