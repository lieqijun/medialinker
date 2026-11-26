package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 全局配置
type Config struct {
	TMDB      TMDBConfig     `yaml:"tmdb"`
	Sources   []SourceConfig `yaml:"sources"`
	Transfer  TransferConfig `yaml:"transfer"`
	Watch     WatchConfig    `yaml:"watch"`
	StateFile string         `yaml:"state_file"`
	LogLevel  string         `yaml:"log_level"`
}

// TMDBConfig TMDB API 配置
// 接口域名与图片域名固定内置（api.tmdb.org 国内可直连），不再提供配置项
type TMDBConfig struct {
	APIKey   string `yaml:"api_key"`
	Language string `yaml:"language"`
	// 每次请求间隔，避免触发限流
	QPS int `yaml:"qps"`
}

// TransferConfig 整理规则
type TransferConfig struct {
	Mode string `yaml:"mode"` // hardlink | copy | move | symlink

	// MediaDir 媒体库根目录，下面自动建 Movies / TVShows / Anime 三个子目录
	MediaDir string `yaml:"media_dir"`

	WriteMovieNFO   bool `yaml:"movie_nfo"`
	WriteTVNFO      bool `yaml:"tvshow_nfo"`
	WriteEpisodeNFO bool `yaml:"episode_nfo"`
	DownloadImages  bool `yaml:"download_images"`

	MinSizeMB int64  `yaml:"min_size_mb"`
	Overwrite string `yaml:"overwrite"` // skip | replace | suffix

	MovieFolder  string `yaml:"movie_folder"`
	MovieFile    string `yaml:"movie_file"`
	TVFolder     string `yaml:"tv_folder"`
	SeasonFolder string `yaml:"season_folder"`
	TVFile       string `yaml:"tv_file"`
	// 动漫可单独一套命名，留空则沿用 tv_* / movie_*
	AnimeFolder string `yaml:"anime_folder"`
	AnimeFile   string `yaml:"anime_file"`
}

// 媒体库的三个固定子目录名
const (
	subDirMovie = "Movies"
	subDirTV    = "TVShows"
	subDirAnime = "Anime"
)

// 三个输出根目录，均由 media_dir 推导
func (t *TransferConfig) MovieRoot() string { return filepath.Join(t.MediaDir, subDirMovie) }
func (t *TransferConfig) TVRoot() string    { return filepath.Join(t.MediaDir, subDirTV) }
func (t *TransferConfig) AnimeRoot() string { return filepath.Join(t.MediaDir, subDirAnime) }

// SourceConfig 源目录
type SourceConfig struct {
	Path string `yaml:"path"`
	// Type 留空即自动分类（推荐），也可强制指定 movie / tv / anime
	Type string `yaml:"type"`
	// 是否递归子目录
	Recursive *bool `yaml:"recursive"`
}

// WatchConfig 扫描与监控
type WatchConfig struct {
	Enabled       bool `yaml:"enabled"`
	Interval      int  `yaml:"interval"`       // 扫描间隔（秒）
	StableSeconds int  `yaml:"stable_seconds"` // 文件大小/修改时间需稳定多久才处理
}

// DefaultConfig 返回带默认值的配置
func DefaultConfig() *Config {
	return &Config{
		TMDB: TMDBConfig{
			Language: "zh-CN",
			QPS:      4,
		},
		Transfer: TransferConfig{
			Mode:            "hardlink",
			WriteMovieNFO:   true,
			WriteTVNFO:      true,
			WriteEpisodeNFO: false,
			DownloadImages:  true,
			MinSizeMB:       50,
			Overwrite:       "skip",
			MovieFolder:     "{title} ({year})",
			MovieFile:       "{title} ({year}){ext}",
			TVFolder:        "{title} ({year})",
			SeasonFolder:    "Season {season:02}",
			TVFile:          "{title} - S{season:02}E{episode:02}{ext}",
			AnimeFolder:     "{title} ({year})",
			AnimeFile:       "{title} - S{season:02}E{episode:02}{ext}",
		},
		Watch: WatchConfig{
			Enabled:       true,
			Interval:      300,
			StableSeconds: 60,
		},
		StateFile: "state.json",
		LogLevel:  "info",
	}
}

// LoadConfig 读取并校验配置。requireAPI 为 false 时不强制要求 tmdb.api_key
// （-parse-only 只解析文件名，不需要联网）。
func LoadConfig(path string, requireAPI bool) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(fixQuotedPaths(raw), cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if err := cfg.validate(requireAPI); err != nil {
		return nil, err
	}
	// state_file 写成相对路径时，以配置文件所在目录为基准（双击运行时不会乱落到别处）
	cfg.StateFile = normalizePath(cfg.StateFile)
	if cfg.StateFile != "" && !filepath.IsAbs(cfg.StateFile) {
		cfg.StateFile = filepath.Join(filepath.Dir(path), cfg.StateFile)
	}
	return cfg, nil
}

func (c *Config) validate(requireAPI bool) error {
	if requireAPI && strings.TrimSpace(c.TMDB.APIKey) == "" {
		return fmt.Errorf("tmdb.api_key 未配置，请到 https://www.themoviedb.org/settings/api 申请")
	}

	switch strings.ToLower(c.Transfer.Mode) {
	case "hardlink", "copy", "move", "symlink":
		c.Transfer.Mode = strings.ToLower(c.Transfer.Mode)
	default:
		return fmt.Errorf("transfer.mode 只能是 hardlink / copy / move / symlink，当前为 %q", c.Transfer.Mode)
	}

	switch strings.ToLower(c.Transfer.Overwrite) {
	case "skip", "replace", "suffix":
		c.Transfer.Overwrite = strings.ToLower(c.Transfer.Overwrite)
	default:
		return fmt.Errorf("transfer.overwrite 只能是 skip / replace / suffix，当前为 %q", c.Transfer.Overwrite)
	}

	if len(c.Sources) == 0 {
		return fmt.Errorf("sources 至少要配置一个源目录")
	}
	for i := range c.Sources {
		s := &c.Sources[i]
		s.Path = normalizePath(s.Path)
		if s.Path == "" {
			return fmt.Errorf("sources[%d].path 不能为空", i)
		}
		abs, err := filepath.Abs(s.Path)
		if err != nil {
			return fmt.Errorf("sources[%d].path 无效: %w", i, err)
		}
		s.Path = abs
		if _, err := os.Stat(s.Path); err != nil {
			return fmt.Errorf("源目录不可访问 %s: %w", s.Path, err)
		}
		s.Type = strings.ToLower(strings.TrimSpace(s.Type))
		if s.Type != "" && s.Type != "movie" && s.Type != "tv" && s.Type != "anime" {
			return fmt.Errorf("sources[%d].type 只能是 movie / tv / anime", i)
		}
	}

	// media_dir 只填到媒体库根目录，Movies / TVShows / Anime 由程序自动创建
	c.Transfer.MediaDir = normalizePath(c.Transfer.MediaDir)
	if c.Transfer.MediaDir == "" {
		return fmt.Errorf("transfer.media_dir 未配置，应填媒体库根目录，例如 D:/Media")
	}
	absMedia, err := filepath.Abs(c.Transfer.MediaDir)
	if err != nil {
		return fmt.Errorf("transfer.media_dir 无效: %w", err)
	}
	c.Transfer.MediaDir = absMedia
	for _, d := range []string{c.Transfer.MovieRoot(), c.Transfer.TVRoot(), c.Transfer.AnimeRoot()} {
		if err := os.MkdirAll(d, 0o775); err != nil {
			return fmt.Errorf("无法创建输出目录 %s: %w", d, err)
		}
	}

	if c.Watch.Interval <= 0 {
		c.Watch.Interval = 300
	}
	if c.TMDB.QPS <= 0 {
		c.TMDB.QPS = 4
	}
	if c.TMDB.Language == "" {
		c.TMDB.Language = "zh-CN"
	}
	return nil
}

// normalizePath 统一路径写法，让 Windows 下从资源管理器直接复制粘贴的路径也能用：
//   - 去掉首尾空白和引号（"复制路径" 会带上双引号）
//   - Windows 下把 \ 换回 /（filepath 两种都认，统一后便于比较与日志）
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"'`)
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = os.ExpandEnv(p) // 支持 $HOME / ${VAR} 写法
	if runtime.GOOS == "windows" {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	return p
}

// fixQuotedPaths 处理 YAML 双引号里的 Windows 路径。
// YAML 双引号字符串里 \ 是转义符，所以 "D:\work" 会直接报"未知转义"，
// 更麻烦的是 "D:\new" 会把 \n 变成换行，路径被悄悄改坏。
// 这里在解析前把「看起来像 Windows 路径」的双引号内容里的 \ 换成 /。
func fixQuotedPaths(raw []byte) []byte {
	if runtime.GOOS != "windows" {
		return raw
	}
	lines := bytes.Split(raw, []byte("\n"))
	for i, line := range lines {
		start := bytes.IndexByte(line, '"')
		if start < 0 {
			continue
		}
		rest := line[start+1:]
		rel := bytes.IndexByte(rest, '"')
		if rel < 0 {
			continue
		}
		inner := rest[:rel]
		if !looksLikeWinPath(inner) {
			continue
		}
		fixed := bytes.ReplaceAll(inner, []byte(`\`), []byte("/"))
		out := make([]byte, 0, len(line))
		out = append(out, line[:start+1]...)
		out = append(out, fixed...)
		out = append(out, line[start+1+rel:]...)
		lines[i] = out
	}
	return bytes.Join(lines, []byte("\n"))
}

// looksLikeWinPath 判断双引号里的内容是不是 Windows 路径（盘符开头或 UNC）
func looksLikeWinPath(b []byte) bool {
	s := strings.TrimSpace(string(b))
	if len(s) >= 3 && s[1] == ':' && s[2] == '/' {
		c := s[0]
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return strings.HasPrefix(s, `\\`) || strings.HasPrefix(s, "//")
}

// recursive 源目录是否递归
func (s SourceConfig) recursive() bool {
	if s.Recursive == nil {
		return true
	}
	return *s.Recursive
}
