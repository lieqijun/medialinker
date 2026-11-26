package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------- 电影 NFO ----------------

type xmlRating struct {
	Name    string  `xml:"name,attr"`
	Max     string  `xml:"max,attr"`
	Default string  `xml:"default,attr"`
	Value   float64 `xml:"value"`
	Votes   int     `xml:"votes"`
}

type xmlRatings struct {
	Rating []xmlRating `xml:"rating"`
}

type xmlUniqueID struct {
	Type    string `xml:"type,attr"`
	Default string `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

type xmlActor struct {
	Name  string `xml:"name"`
	Role  string `xml:"role,omitempty"`
	Order int    `xml:"order"`
	Thumb string `xml:"thumb,omitempty"`
}

type xmlFanart struct {
	Thumb string `xml:"thumb"`
}

type xmlThumb struct {
	Aspect string `xml:"aspect,attr,omitempty"`
	Value  string `xml:",chardata"`
}

type movieNFO struct {
	XMLName   xml.Name `xml:"movie"`
	Title     string   `xml:"title"`
	Original  string   `xml:"originaltitle"`
	SortTitle string     `xml:"sorttitle,omitempty"`
	Ratings   xmlRatings `xml:"ratings"`
	Plot      string     `xml:"plot"`
	Outline   string     `xml:"outline,omitempty"`
	Tagline   string     `xml:"tagline,omitempty"`
	Runtime   int        `xml:"runtime,omitempty"`

	Thumb   xmlThumb  `xml:"thumb"`
	Fanart  xmlFanart `xml:"fanart"`
	Year    int       `xml:"year"`
	Premier string    `xml:"premiered,omitempty"`

	Genre     []string      `xml:"genre"`
	Country   []string      `xml:"country"`
	Studio    []string      `xml:"studio"`
	Director  []string      `xml:"director"`
	Credits   []string      `xml:"credits"`
	UniqueID  []xmlUniqueID `xml:"uniqueid"`
	Actors    []xmlActor    `xml:"actor"`
	DateAdded string        `xml:"dateadded,omitempty"`
}

// ---------------- 剧集 NFO ----------------

type tvshowNFO struct {
	XMLName  xml.Name `xml:"tvshow"`
	Title    string   `xml:"title"`
	ShowTitle string  `xml:"showtitle"`
	Original string   `xml:"originaltitle"`
	SortTitle string  `xml:"sorttitle,omitempty"`
	Ratings  xmlRatings `xml:"ratings"`
	Plot     string `xml:"plot"`
	Outline  string `xml:"outline,omitempty"`
	Season   int    `xml:"season"`
	Episode  int    `xml:"episode"`
	Status   string `xml:"status,omitempty"`
	Premier  string `xml:"premiered,omitempty"`
	Year     int    `xml:"year"`

	Thumb     xmlThumb      `xml:"thumb"`
	Fanart    xmlFanart     `xml:"fanart"`
	Genre     []string      `xml:"genre"`
	Country   []string      `xml:"country"`
	Studio    []string      `xml:"studio"`
	UniqueID  []xmlUniqueID `xml:"uniqueid"`
	Actors    []xmlActor    `xml:"actor"`
	DateAdded string        `xml:"dateadded,omitempty"`
}

// ---------------- 单集 NFO ----------------

type episodeNFO struct {
	XMLName   xml.Name `xml:"episodedetails"`
	Title     string   `xml:"title"`
	ShowTitle string   `xml:"showtitle"`
	Season    int      `xml:"season"`
	Episode   int      `xml:"episode"`
	Plot      string     `xml:"plot"`
	Ratings   xmlRatings `xml:"ratings"`
	Aired     string     `xml:"aired,omitempty"`
	Thumb     string `xml:"thumb,omitempty"`
}

func writeXML(path string, v any) error {
	f, err := createFile(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"); err != nil {
		return err
	}
	enc := xml.NewEncoder(f)
	enc.Indent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return f.Sync()
}

func buildRatings(v float64) xmlRatings {
	return xmlRatings{Rating: []xmlRating{{
		Name: "themoviedb", Max: "10", Default: "true",
		Value: round1(v), Votes: 0,
	}}}
}

func buildActors(cast []CastMember) []xmlActor {
	out := make([]xmlActor, 0, len(cast))
	for i, c := range cast {
		thumb := ""
		if c.Profile != "" {
			thumb = fmt.Sprintf("actor_%02d.jpg", i+1)
		}
		out = append(out, xmlActor{Name: c.Name, Role: c.Character, Order: i, Thumb: thumb})
	}
	return out
}

// WriteMovieNFO 写 movie.nfo
func WriteMovieNFO(dir string, d *MovieDetail, log *Logger) error {
	n := &movieNFO{
		Title:     d.Title,
		Original:  d.OriginalTitle,
		SortTitle: d.Title,
		Ratings:   buildRatings(d.VoteAverage),
		Plot:      d.Overview,
		Outline:   truncate(d.Overview, 200),
		Tagline:   d.Tagline,
		Runtime:   d.Runtime,
		Thumb:     xmlThumb{Aspect: "poster", Value: "poster.jpg"},
		Fanart:    xmlFanart{Thumb: "fanart.jpg"},
		Year:      d.Year,
		Premier:   d.ReleaseDate,
		Genre:     d.Genres,
		Country:   d.Countries,
		Studio:    d.Studios,
		Credits:   d.Writers,
		Actors:    buildActors(d.Cast),
		DateAdded: time.Now().Format("2006-01-02 15:04:05"),
	}
	if d.Director != "" {
		n.Director = []string{d.Director}
	}
	if d.IMDBID != "" {
		n.UniqueID = append(n.UniqueID, xmlUniqueID{Type: "imdb", Default: "true", Value: d.IMDBID})
	}
	n.UniqueID = append(n.UniqueID, xmlUniqueID{Type: "tmdb", Value: fmt.Sprint(d.ID)})

	path := filepath.Join(dir, "movie.nfo")
	if err := writeXML(path, n); err != nil {
		return fmt.Errorf("写入 movie.nfo 失败: %w", err)
	}
	log.Debugf("已生成 %s", path)
	return nil
}

// WriteTVShowNFO 写 tvshow.nfo
func WriteTVShowNFO(dir string, d *TVDetail, log *Logger) error {
	n := &tvshowNFO{
		Title:     d.Name,
		ShowTitle: d.Name,
		Original:  d.OriginalName,
		SortTitle: d.Name,
		Ratings:   buildRatings(d.VoteAverage),
		Plot:      d.Overview,
		Outline:   truncate(d.Overview, 200),
		Season:    -1,
		Episode:   -1,
		Premier:   d.FirstAirDate,
		Year:      d.Year,
		Thumb:     xmlThumb{Aspect: "poster", Value: "poster.jpg"},
		Fanart:    xmlFanart{Thumb: "fanart.jpg"},
		Genre:     d.Genres,
		Country:   d.Countries,
		Studio:    d.Studios,
		Actors:    buildActors(d.Cast),
		DateAdded: time.Now().Format("2006-01-02 15:04:05"),
	}
	if d.IMDBID != "" {
		n.UniqueID = append(n.UniqueID, xmlUniqueID{Type: "imdb", Default: "true", Value: d.IMDBID})
	}
	n.UniqueID = append(n.UniqueID, xmlUniqueID{Type: "tmdb", Value: fmt.Sprint(d.ID)})

	path := filepath.Join(dir, "tvshow.nfo")
	if err := writeXML(path, n); err != nil {
		return fmt.Errorf("写入 tvshow.nfo 失败: %w", err)
	}
	log.Debugf("已生成 %s", path)
	return nil
}

// WriteEpisodeNFO 写单集 NFO（与视频同名）
func WriteEpisodeNFO(videoPath string, ep *EpisodeDetail, showName string, season, episode int) error {
	n := &episodeNFO{
		Title:     firstNonEmpty(ep.Name, fmt.Sprintf("第 %d 集", episode)),
		ShowTitle: showName,
		Season:    season,
		Episode:   episode,
		Plot:      ep.Overview,
		Ratings:   buildRatings(ep.VoteAverage),
		Aired:     ep.AirDate,
		Thumb:     "episode.jpg",
	}
	path := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".nfo"
	return writeXML(path, n)
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

// ensureDir 确保目录存在
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o775)
}
