package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TMDB 的 API 域名。国内 api.themoviedb.org 常被 DNS 污染（解析到 Facebook 的 IP），
// 而 api.tmdb.org 走 CloudFront，实测可直连，所以固定用它，不做配置项。
// 注意：nas-tools 里的 tmdb.nastool.cn 已域名失效，tmdb.nastool.workers.dev 也被污染。
const defaultTMDBAPI = "https://api.tmdb.org/3"

const tmdbImageBase = "https://image.tmdb.org/t/p/original"

// TMDB 客户端，带 QPS 限流和内存缓存
type TMDB struct {
	apiKey    string
	lang      string
	baseURL   string
	imageBase string
	client    *http.Client
	limiter   *time.Ticker

	mu       sync.Mutex
	movies   map[int]*MovieDetail
	tvs      map[int]*TVDetail
	searches map[string]*SearchResult
}

// BaseURL 返回实际使用的 API 地址，便于启动日志显示
func (t *TMDB) BaseURL() string { return t.baseURL }

// SearchResult 搜索结果
type SearchResult struct {
	ID           int
	Title        string
	OriginalName string
	Year         int
	Popularity   float64
	VoteAverage  float64
	PosterPath   string
	Overview     string
}

// MovieDetail 电影详情
type MovieDetail struct {
	ID            int
	Title         string
	OriginalTitle string
	Overview      string
	Tagline       string
	Year          int
	ReleaseDate   string
	Runtime       int
	VoteAverage   float64
	Genres        []string
	Studios       []string
	Countries     []string
	Director      string
	Writers       []string
	Cast          []CastMember
	PosterPath    string
	BackdropPath  string
	IMDBID        string
}

// TVDetail 剧集详情
type TVDetail struct {
	ID           int
	Name         string
	OriginalName string
	Overview     string
	Year         int
	FirstAirDate string
	VoteAverage  float64
	Genres       []string
	Studios      []string
	Countries    []string
	Cast         []CastMember
	PosterPath   string
	BackdropPath string
	IMDBID       string
	Seasons      map[int]*SeasonDetail
}

// SeasonDetail 季
type SeasonDetail struct {
	SeasonNumber int
	Name         string
	Overview     string
	PosterPath   string
	Episodes     map[int]*EpisodeDetail
}

// EpisodeDetail 单集
type EpisodeDetail struct {
	EpisodeNumber int
	Name          string
	Overview      string
	AirDate       string
	StillPath     string
	VoteAverage   float64
}

// CastMember 演员
type CastMember struct {
	Name      string
	Character string
	Profile   string
}

// NewTMDB 创建客户端
func NewTMDB(cfg TMDBConfig) (*TMDB, error) {
	qps := cfg.QPS
	if qps <= 0 {
		qps = 4
	}
	transport := &http.Transport{
		MaxIdleConns:        20,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		// 连接阶段单独限时：连不上时快速失败，不要每个文件都卡 20 秒
		DialContext: (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	}
	return &TMDB{
		apiKey:    strings.TrimSpace(cfg.APIKey),
		lang:      cfg.Language,
		baseURL:   defaultTMDBAPI,
		imageBase: tmdbImageBase,
		client:    &http.Client{Timeout: 30 * time.Second, Transport: transport},
		limiter:   time.NewTicker(time.Second / time.Duration(qps)),
		movies:    map[int]*MovieDetail{},
		tvs:       map[int]*TVDetail{},
		searches:  map[string]*SearchResult{},
	}, nil
}

// get 发起一次带限流的 GET
func (t *TMDB) get(path string, params map[string]string, out any) error {
	<-t.limiter.C
	return t.doGet(t.baseURL, path, params, out)
}

// doGet 用指定域名发一次请求
func (t *TMDB) doGet(base, path string, params map[string]string, out any) error {
	u, err := url.Parse(base + path)
	if err != nil {
		return err
	}
	// v4 的 token 以 eyJ 开头（JWT），用 Bearer；v3 的 32 位 key 走 query
	useBearer := strings.HasPrefix(t.apiKey, "eyJ")

	q := u.Query()
	q.Set("language", t.lang)
	if !useBearer {
		q.Set("api_key", t.apiKey)
	}
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if useBearer {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求 TMDB 失败（域名 %s）: %w", base, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("TMDB 鉴权失败，请检查 api_key（HTTP 401）")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		time.Sleep(2 * time.Second)
		return fmt.Errorf("TMDB 限流（HTTP 429），稍后重试")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB 返回 HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return json.Unmarshal(body, out)
}

// Ping 启动时探一次连通性。连不上就别一个一个文件去等超时了。
func (t *TMDB) Ping() error {
	var out struct {
		Images struct {
			BaseURL string `json:"base_url"`
		} `json:"images"`
	}
	return t.get("/configuration", nil, &out)
}

// Search 搜索电影或剧集，返回最可能的一条
func (t *TMDB) Search(kind MediaKind, title string, year int) (*SearchResult, error) {
	key := fmt.Sprintf("%d|%s|%d", kind, strings.ToLower(title), year)
	t.mu.Lock()
	if r, ok := t.searches[key]; ok {
		t.mu.Unlock()
		return r, nil
	}
	t.mu.Unlock()

	path := "/search/movie"
	params := map[string]string{"query": title}
	if kind == KindTV {
		path = "/search/tv"
	}
	if year > 0 {
		if kind == KindTV {
			params["first_air_date_year"] = strconv.Itoa(year)
		} else {
			params["year"] = strconv.Itoa(year)
		}
	}

	var raw struct {
		Results []struct {
			ID            int     `json:"id"`
			Title         string  `json:"title"`
			Name          string  `json:"name"`
			OriginalTitle string  `json:"original_title"`
			OriginalName  string  `json:"original_name"`
			ReleaseDate   string  `json:"release_date"`
			FirstAirDate  string  `json:"first_air_date"`
			Popularity    float64 `json:"popularity"`
			VoteAverage   float64 `json:"vote_average"`
			PosterPath    string  `json:"poster_path"`
			Overview      string  `json:"overview"`
		} `json:"results"`
	}
	if err := t.get(path, params, &raw); err != nil {
		return nil, err
	}
	if len(raw.Results) == 0 && year > 0 {
		// 带年份没结果时，去掉年份再搜一次
		delete(params, "year")
		delete(params, "first_air_date_year")
		if err := t.get(path, params, &raw); err != nil {
			return nil, err
		}
	}
	if len(raw.Results) == 0 {
		return nil, fmt.Errorf("TMDB 未找到「%s」", title)
	}

	// 打分挑选：标题（含原名）完全一致 > 年份吻合 > 热度高
	// 直接取 results[0] 会选到衍生剧/短片（例如 Sword Art Online 的 Sword Art Offline），
	// 而文件名没年份时又只能靠热度兜底，所以两项都要比。
	best := raw.Results[0]
	bestScore := -1.0
	want := strings.TrimSpace(title)
	for _, it := range raw.Results {
		name := strings.TrimSpace(firstNonEmpty(it.Title, it.Name))
		orig := strings.TrimSpace(firstNonEmpty(it.OriginalTitle, it.OriginalName))
		score := 0.0
		if want != "" && (strings.EqualFold(name, want) || strings.EqualFold(orig, want)) {
			score += 100
		}
		gotYear := 0
		if d := firstNonEmpty(it.ReleaseDate, it.FirstAirDate); len(d) >= 4 {
			gotYear, _ = strconv.Atoi(d[:4])
		}
		if year > 0 && gotYear > 0 {
			switch {
			case gotYear == year:
				score += 50
			case gotYear == year-1 || gotYear == year+1:
				score += 20
			}
		}
		// 热度只做同级细分，最多 10 分，压不过标题与年份
		if it.Popularity > 0 {
			p := it.Popularity
			if p > 1000 {
				p = 1000
			}
			score += p / 100
		}
		if score > bestScore {
			best, bestScore = it, score
		}
	}

	r := &SearchResult{
		ID:           best.ID,
		Title:        firstNonEmpty(best.Title, best.Name),
		OriginalName: firstNonEmpty(best.OriginalTitle, best.OriginalName),
		Popularity:   best.Popularity,
		VoteAverage:  best.VoteAverage,
		PosterPath:   best.PosterPath,
		Overview:     best.Overview,
	}
	d := firstNonEmpty(best.ReleaseDate, best.FirstAirDate)
	if len(d) >= 4 {
		r.Year, _ = strconv.Atoi(d[:4])
	}

	t.mu.Lock()
	t.searches[key] = r
	t.mu.Unlock()
	return r, nil
}

// Movie 获取电影详情
func (t *TMDB) Movie(id int) (*MovieDetail, error) {
	t.mu.Lock()
	if d, ok := t.movies[id]; ok {
		t.mu.Unlock()
		return d, nil
	}
	t.mu.Unlock()

	var raw movieRaw
	if err := t.get(fmt.Sprintf("/movie/%d", id), map[string]string{"append_to_response": "credits,external_ids"}, &raw); err != nil {
		return nil, err
	}
	d := &MovieDetail{
		ID:            raw.ID,
		Title:         raw.Title,
		OriginalTitle: raw.OriginalTitle,
		Overview:      raw.Overview,
		Tagline:       raw.Tagline,
		ReleaseDate:   raw.ReleaseDate,
		Runtime:       raw.Runtime,
		VoteAverage:   raw.VoteAverage,
		PosterPath:    raw.PosterPath,
		BackdropPath:  raw.BackdropPath,
		IMDBID:        raw.ExternalIDs.IMDBID,
	}
	if len(raw.ReleaseDate) >= 4 {
		d.Year, _ = strconv.Atoi(raw.ReleaseDate[:4])
	}
	for _, g := range raw.Genres {
		d.Genres = append(d.Genres, g.Name)
	}
	for _, s := range raw.ProductionCompanies {
		d.Studios = append(d.Studios, s.Name)
	}
	for _, c := range raw.ProductionCountries {
		d.Countries = append(d.Countries, c.Name)
	}
	for _, c := range raw.Credits.Crew {
		switch c.Job {
		case "Director":
			if d.Director == "" {
				d.Director = c.Name
			}
		case "Writer", "Screenplay", "Story":
			d.Writers = append(d.Writers, c.Name)
		}
	}
	for _, c := range raw.Credits.Cast {
		if len(d.Cast) >= 20 {
			break
		}
		d.Cast = append(d.Cast, CastMember{Name: c.Name, Character: c.Character, Profile: c.ProfilePath})
	}

	t.mu.Lock()
	t.movies[id] = d
	t.mu.Unlock()
	return d, nil
}

// TV 获取剧集详情
func (t *TMDB) TV(id int) (*TVDetail, error) {
	t.mu.Lock()
	if d, ok := t.tvs[id]; ok {
		t.mu.Unlock()
		return d, nil
	}
	t.mu.Unlock()

	var raw tvRaw
	if err := t.get(fmt.Sprintf("/tv/%d", id), map[string]string{"append_to_response": "credits,external_ids"}, &raw); err != nil {
		return nil, err
	}
	d := &TVDetail{
		ID:           raw.ID,
		Name:         raw.Name,
		OriginalName: raw.OriginalName,
		Overview:     raw.Overview,
		FirstAirDate: raw.FirstAirDate,
		VoteAverage:  raw.VoteAverage,
		PosterPath:   raw.PosterPath,
		BackdropPath: raw.BackdropPath,
		IMDBID:       raw.ExternalIDs.IMDBID,
		Seasons:      map[int]*SeasonDetail{},
	}
	if len(raw.FirstAirDate) >= 4 {
		d.Year, _ = strconv.Atoi(raw.FirstAirDate[:4])
	}
	for _, g := range raw.Genres {
		d.Genres = append(d.Genres, g.Name)
	}
	for _, s := range raw.ProductionCompanies {
		d.Studios = append(d.Studios, s.Name)
	}
	for _, c := range raw.ProductionCountries {
		d.Countries = append(d.Countries, c.Name)
	}
	for _, c := range raw.Credits.Cast {
		if len(d.Cast) >= 20 {
			break
		}
		d.Cast = append(d.Cast, CastMember{Name: c.Name, Character: c.Character, Profile: c.ProfilePath})
	}

	t.mu.Lock()
	t.tvs[id] = d
	t.mu.Unlock()
	return d, nil
}

// Episode 获取单集信息
func (t *TMDB) Episode(tvID, season, episode int) (*EpisodeDetail, error) {
	var raw struct {
		EpisodeNumber int     `json:"episode_number"`
		Name          string  `json:"name"`
		Overview      string  `json:"overview"`
		AirDate       string  `json:"air_date"`
		StillPath     string  `json:"still_path"`
		VoteAverage   float64 `json:"vote_average"`
	}
	path := fmt.Sprintf("/tv/%d/season/%d/episode/%d", tvID, season, episode)
	if err := t.get(path, nil, &raw); err != nil {
		return nil, err
	}
	return &EpisodeDetail{
		EpisodeNumber: raw.EpisodeNumber,
		Name:          raw.Name,
		Overview:      raw.Overview,
		AirDate:       raw.AirDate,
		StillPath:     raw.StillPath,
		VoteAverage:   raw.VoteAverage,
	}, nil
}

// DownloadImage 下载图片到本地
func (t *TMDB) DownloadImage(imagePath, dest string) error {
	if strings.TrimSpace(imagePath) == "" {
		return fmt.Errorf("图片路径为空")
	}
	<-t.limiter.C
	resp, err := t.client.Get(t.imageBase + imagePath)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载图片 HTTP %d", resp.StatusCode)
	}
	f, err := createFile(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(dest) // 别留下半个图片，下次会被当成已下载
		return err
	}
	return f.Close()
}

// ---- 内部原始结构 ----

type movieRaw struct {
	ID                  int     `json:"id"`
	Title               string  `json:"title"`
	OriginalTitle       string  `json:"original_title"`
	Overview            string  `json:"overview"`
	Tagline             string  `json:"tagline"`
	ReleaseDate         string  `json:"release_date"`
	Runtime             int     `json:"runtime"`
	VoteAverage         float64 `json:"vote_average"`
	PosterPath          string  `json:"poster_path"`
	BackdropPath        string  `json:"backdrop_path"`
	Genres              []struct{ Name string `json:"name"` } `json:"genres"`
	ProductionCompanies []struct{ Name string `json:"name"` } `json:"production_companies"`
	ProductionCountries []struct{ Name string `json:"name"` } `json:"production_countries"`
	ExternalIDs         struct{ IMDBID string `json:"imdb_id"` } `json:"external_ids"`
	Credits             struct {
		Cast []struct {
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
		} `json:"cast"`
		Crew []struct {
			Name string `json:"name"`
			Job  string `json:"job"`
		} `json:"crew"`
	} `json:"credits"`
}

type tvRaw struct {
	ID                  int     `json:"id"`
	Name                string  `json:"name"`
	OriginalName        string  `json:"original_name"`
	Overview            string  `json:"overview"`
	FirstAirDate        string  `json:"first_air_date"`
	VoteAverage         float64 `json:"vote_average"`
	PosterPath          string  `json:"poster_path"`
	BackdropPath        string  `json:"backdrop_path"`
	Genres              []struct{ Name string `json:"name"` } `json:"genres"`
	ProductionCompanies []struct{ Name string `json:"name"` } `json:"production_companies"`
	ProductionCountries []struct{ Name string `json:"name"` } `json:"production_countries"`
	ExternalIDs         struct{ IMDBID string `json:"imdb_id"` } `json:"external_ids"`
	Credits             struct {
		Cast []struct {
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
		} `json:"cast"`
	} `json:"credits"`
}

func firstNonEmpty(items ...string) string {
	for _, s := range items {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
