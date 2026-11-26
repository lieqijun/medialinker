package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// 版本号，编译时可用 -ldflags "-X main.appVersion=x.y.z" 覆盖
var appVersion = "0.1.0"

func main() {
	var (
		configPath = flag.String("config", "config.yaml", "配置文件路径")
		once       = flag.Bool("once", false, "只扫描一次后退出")
		dryRun     = flag.Bool("dry-run", false, "只打印将要执行的操作，不实际写入")
		interval   = flag.Int("interval", 0, "扫描间隔（秒），覆盖配置里的 watch.interval")
		parseOnly  = flag.Bool("parse-only", false, "只解析文件名并打印识别结果，不联网、不写文件")
		showVer    = flag.Bool("version", false, "打印版本")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("media-linker %s\n", appVersion)
		return
	}

	cfg, err := LoadConfig(*configPath, !*parseOnly)
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		os.Exit(1)
	}
	if *interval > 0 {
		cfg.Watch.Interval = *interval
	}

	log := NewLogger(cfg.LogLevel)
	log.Infof("media-linker %s 启动", appVersion)
	log.Infof("媒体库: %s", cfg.Transfer.MediaDir)
	log.Infof("  ├─ 电影   %s", cfg.Transfer.MovieRoot())
	log.Infof("  ├─ 电视剧 %s", cfg.Transfer.TVRoot())
	log.Infof("  └─ 动漫   %s", cfg.Transfer.AnimeRoot())
	log.Infof("整理模式: %s | 源目录: %d 个 | 扫描间隔: %ds", cfg.Transfer.Mode, len(cfg.Sources), cfg.Watch.Interval)
	if *dryRun {
		log.Warnf("dry-run 模式：不会写入任何文件")
	}
	if *parseOnly {
		log.Warnf("parse-only 模式：只解析文件名，不联网、不写文件")
	}

	tmdb, err := NewTMDB(cfg.TMDB)
	if err != nil {
		log.Errorf("初始化 TMDB 失败: %v", err)
		os.Exit(1)
	}

	// 先探连通性：连不上就直接报错退出，别让每个文件都白等一次超时
	if !*parseOnly {
		if err := tmdb.Ping(); err != nil {
			log.Errorf("TMDB 连不上: %v", err)
			log.Errorf("默认域名是 %s（国内 api.themoviedb.org 被污染，不要用它）", defaultTMDBAPI)
			log.Errorf("如确实访问不了，可在 config.yaml 的 tmdb.base_url 换成你自己能访问的镜像")
			os.Exit(1)
		}
		log.Infof("TMDB 连接正常，使用域名 %s", tmdb.BaseURL())
	}

	state, err := LoadState(cfg.StateFile)
	if err != nil {
		log.Warnf("读取状态文件失败，将从空状态开始: %v", err)
		state = &State{path: cfg.StateFile, data: map[string]Record{}}
	}

	app := &App{cfg: cfg, log: log, tmdb: tmdb, state: state, dryRun: *dryRun, parseOnly: *parseOnly}

	// 首次立即扫描
	app.scanAll()

	if *once || !cfg.Watch.Enabled {
		log.Infof("扫描完成，退出")
		return
	}

	// 优雅退出
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(cfg.Watch.Interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			app.scanAll()
		case <-sig:
			log.Infof("收到退出信号，停止扫描")
			return
		}
	}
}

// App 运行上下文
type App struct {
	cfg       *Config
	log       *Logger
	tmdb      *TMDB
	state     *State
	dryRun    bool
	parseOnly bool
}

type stats struct {
	total     int
	processed int
	skipped   int
	failed    int
}

func (a *App) scanAll() {
	start := time.Now()
	var total stats

	for _, src := range a.cfg.Sources {
		files, err := collectVideoFiles(src)
		if err != nil {
			a.log.Errorf("扫描 %s 失败: %v", src.Path, err)
			continue
		}
		a.log.Infof("发现 %d 个视频文件: %s", len(files), src.Path)

		for i, f := range files {
			st := a.handleFile(src, f, i+1, len(files))
			total.total++
			total.processed += st.processed
			total.skipped += st.skipped
			total.failed += st.failed
		}
	}

	a.log.Infof("本轮完成：共 %d，成功 %d，跳过 %d，失败 %d，耗时 %.1fs",
		total.total, total.processed, total.skipped, total.failed, time.Since(start).Seconds())
}

func (a *App) handleFile(src SourceConfig, path string, idx, total int) stats {
	var st stats

	name := filepath.Base(path)
	a.log.Infof("[%d/%d] %s", idx, total, name)

	// 已处理过
	if a.state.Done(path) {
		a.log.Debugf("已处理过，跳过")
		st.skipped = 1
		return st
	}

	info, err := os.Stat(path)
	if err != nil {
		a.log.Warnf("无法读取文件信息: %v", err)
		st.failed = 1
		return st
	}

	// 体积过滤（排除广告、样片）
	minSize := a.cfg.Transfer.MinSizeMB * 1024 * 1024
	if !a.parseOnly && minSize > 0 && info.Size() < minSize {
		a.log.Debugf("体积 %.1fMB 小于阈值，跳过", float64(info.Size())/1024/1024)
		st.skipped = 1
		return st
	}

	// 正在写入的文件先不动
	stable := time.Duration(a.cfg.Watch.StableSeconds) * time.Second
	if !a.parseOnly && stable > 0 && time.Since(info.ModTime()) < stable {
		a.log.Infof("文件仍在变化（%s 内修改过），本轮跳过", stable)
		st.skipped = 1
		return st
	}

	// 解析文件名
	p := ParseFileName(name)
	if src.Type == "movie" {
		p.Kind = KindMovie
	} else if src.Type == "tv" && p.Kind == KindMovie {
		p.Kind = KindTV
		if p.Season == 0 {
			p.Season = 1
		}
	}
	// type: anime 不改变电影/剧集判定，只影响最终进哪个目录
	a.log.Infof("  解析: %s", p.String())

	if p.Title == "" {
		a.log.Warnf("  无法从文件名解析出标题，跳过")
		st.failed = 1
		return st
	}

	// parse-only：打印识别结果与预估目标，到此为止
	if a.parseOnly {
		anime := a.animeByHint(src.Type, p)
		m := &MediaInfo{Kind: p.Kind, Title: p.Title, OriginalTitle: p.Title, Year: p.Year, Anime: anime}
		if dir, file, err := targetPaths(p, m, a.cfg); err == nil {
			if anime {
				a.log.Infof("  归类: 动漫（按文件名特征判断，正式跑会以 TMDB 类型复核）")
			}
			a.log.Infof("  预估目标: %s", filepath.Join(dir, file))
		}
		st.processed = 1
		return st
	}

	// 剧集必须有集数，否则会生成 S01E00 这种没法入库的名字
	if p.Kind == KindTV && p.Episode <= 0 {
		a.log.Warnf("  识别为剧集但没解析出集数，跳过（可给源目录加 type: movie 强制按电影处理）")
		st.failed = 1
		return st
	}

	// 查 TMDB
	sr, err := a.tmdb.Search(p.Kind, p.Title, p.Year)
	if err != nil {
		a.log.Errorf("  TMDB 搜索失败: %v", err)
		st.failed = 1
		return st
	}
	a.log.Infof("  TMDB: %s (%d) id=%d", sr.Title, sr.Year, sr.ID)

	if a.dryRun {
		m, err := a.buildMediaInfo(src.Type, p, sr.ID)
		if err != nil {
			a.log.Errorf("  %v", err)
			st.failed = 1
			return st
		}
		dir, file, err := targetPaths(p, m, a.cfg)
		if err != nil {
			a.log.Errorf("  计算目标路径失败: %v", err)
			st.failed = 1
			return st
		}
		a.log.Infof("  归类: %s", kindLabel(m))
		a.log.Infof("  目标: %s", filepath.Join(dir, file))
		st.processed = 1
		return st
	}

	// 按类型抓详情并整理
	dst, err := a.handleItem(src.Type, path, p, sr.ID)
	if err != nil {
		a.log.Errorf("  整理失败: %v", err)
		st.failed = 1
		return st
	}

	if err := a.state.Mark(path, Record{
		Source: path,
		Target: dst,
		TMDbID: sr.ID,
		Title:  sr.Title,
	}); err != nil {
		a.log.Warnf("  状态写入失败: %v", err)
	}

	a.log.Infof("  完成 -> %s", dst)
	st.processed = 1
	return st
}

// buildMediaInfo 查详情并组装 MediaInfo，dry-run 与正式流程共用同一套判定
func (a *App) buildMediaInfo(srcType string, p *ParsedMedia, tmdbID int) (*MediaInfo, error) {
	if p.Kind == KindMovie {
		d, err := a.tmdb.Movie(tmdbID)
		if err != nil {
			return nil, fmt.Errorf("获取电影详情失败: %w", err)
		}
		return &MediaInfo{
			Kind:          KindMovie,
			Title:         d.Title,
			OriginalTitle: d.OriginalTitle,
			Year:          d.Year,
			TMDBID:        d.ID,
			Anime:         decideAnime(srcType, p.AnimeHint, d.Genres),
		}, nil
	}
	d, err := a.tmdb.TV(tmdbID)
	if err != nil {
		return nil, fmt.Errorf("获取剧集详情失败: %w", err)
	}
	return &MediaInfo{
		Kind:          KindTV,
		Title:         d.Name,
		OriginalTitle: d.OriginalName,
		Year:          d.Year,
		TMDBID:        d.ID,
		Anime:         decideAnime(srcType, p.AnimeHint, d.Genres),
	}, nil
}

// handleItem 抓详情 -> 判断类型 -> 转移 -> 写 NFO/图片
func (a *App) handleItem(srcType, path string, p *ParsedMedia, tmdbID int) (string, error) {
	m, err := a.buildMediaInfo(srcType, p, tmdbID)
	if err != nil {
		return "", err
	}
	a.log.Infof("  归类: %s", kindLabel(m))

	if p.Kind == KindMovie {
		d, err := a.tmdb.Movie(tmdbID)
		if err != nil {
			return "", fmt.Errorf("获取电影详情失败: %w", err)
		}
		dst, err := Transfer(path, p, m, a.cfg, a.log)
		if err != nil {
			return "", err
		}
		dir := filepath.Dir(dst)
		if a.cfg.Transfer.WriteMovieNFO {
			if err := WriteMovieNFO(dir, d, a.log); err != nil {
				a.log.Warnf("  %v", err)
			}
		}
		if a.cfg.Transfer.DownloadImages {
			a.saveImage(d.PosterPath, filepath.Join(dir, "poster.jpg"))
			a.saveImage(d.BackdropPath, filepath.Join(dir, "fanart.jpg"))
		}
		return dst, nil
	}

	d, err := a.tmdb.TV(tmdbID)
	if err != nil {
		return "", fmt.Errorf("获取剧集详情失败: %w", err)
	}
	dst, err := Transfer(path, p, m, a.cfg, a.log)
	if err != nil {
		return "", err
	}

	seasonDir := filepath.Dir(dst)
	showDir := filepath.Dir(seasonDir)

	if a.cfg.Transfer.WriteTVNFO {
		if err := WriteTVShowNFO(showDir, d, a.log); err != nil {
			a.log.Warnf("  %v", err)
		}
	}
	if a.cfg.Transfer.DownloadImages {
		a.saveImage(d.PosterPath, filepath.Join(showDir, "poster.jpg"))
		a.saveImage(d.BackdropPath, filepath.Join(showDir, "fanart.jpg"))
	}

	if a.cfg.Transfer.WriteEpisodeNFO {
		ep, err := a.tmdb.Episode(d.ID, p.Season, p.Episode)
		if err != nil {
			a.log.Warnf("  获取第 %d 集信息失败: %v", p.Episode, err)
		} else {
			if err := WriteEpisodeNFO(dst, ep, d.Name, p.Season, p.Episode); err != nil {
				a.log.Warnf("  %v", err)
			}
			if ep.StillPath != "" {
				a.saveImage(ep.StillPath, strings.TrimSuffix(dst, filepath.Ext(dst))+"-thumb.jpg")
			}
		}
	}
	return dst, nil
}

// animeByHint 不联网时的动漫判断（parse-only 预览用）
func (a *App) animeByHint(srcType string, p *ParsedMedia) bool {
	return decideAnime(srcType, p.AnimeHint, nil)
}

// decideAnime 动漫归类的唯一判定入口。
// 优先级：源目录显式指定 anime > TMDB 类型里有动画 > 文件名特征（仅在拿不到类型时兜底）。
func decideAnime(srcType string, hint bool, genres []string) bool {
	if strings.EqualFold(srcType, "anime") {
		return true
	}
	if len(genres) > 0 {
		for _, g := range genres {
			if isAnimationGenre(g) {
				return true
			}
		}
		// 有类型信息且不含动画 -> 就按普通电影/剧集处理，不信文件名
		return false
	}
	return hint
}

func isAnimationGenre(g string) bool {
	switch strings.ToLower(strings.TrimSpace(g)) {
	case "动画", "動畫", "animation", "anime", "アニメ":
		return true
	}
	return false
}

func kindLabel(m *MediaInfo) string {
	if m.Anime {
		if m.Kind == KindMovie {
			return "动漫电影"
		}
		return "动漫"
	}
	if m.Kind == KindMovie {
		return "电影"
	}
	return "电视剧"
}

func (a *App) saveImage(imagePath, dest string) {
	if imagePath == "" {
		return
	}
	if _, err := os.Stat(dest); err == nil {
		return
	}
	if err := a.tmdb.DownloadImage(imagePath, dest); err != nil {
		a.log.Warnf("  图片下载失败 %s: %v", filepath.Base(dest), err)
	}
}

// collectVideoFiles 收集源目录下的视频文件
func collectVideoFiles(src SourceConfig) ([]string, error) {
	var files []string

	skipDir := func(name string) bool {
		low := strings.ToLower(name)
		return low == "@eadir" || low == "#recycle" || low == ".@__thumb" ||
			low == "$recycle.bin" || low == "system volume information" ||
			strings.HasPrefix(low, ".trash")
	}

	if !src.recursive() {
		entries, err := os.ReadDir(src.Path)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || skipDir(e.Name()) {
				continue
			}
			if !isVideoFile(e.Name()) || isSample(e.Name()) {
				continue
			}
			files = append(files, filepath.Join(src.Path, e.Name()))
		}
		return files, nil
	}

	err := filepath.WalkDir(src.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 单个目录不可读时继续
		}
		if d.IsDir() {
			if path != src.Path && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if !isVideoFile(name) || isSample(name) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

var reSample = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])sample(?:[\s._\-\]\),]|$)`)

func isSample(name string) bool {
	return reSample.MatchString(strings.TrimSuffix(name, filepath.Ext(name)))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
