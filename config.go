package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 全局配置
type Config struct {
	TMDB     TMDBConfig     `yaml:"tmdb"`
	Transfer TransferConfig `yaml:"transfer"`
	Sources  []SourceConfig `yaml:"sources"`
	Watch    WatchConfig    `yaml:"watch"`
	StateFile string        `yaml:"state_file"`
	LogLevel string         `yaml:"log_level"`
}

// TMDBConfig TMDB API 配置
type TMDBConfig struct {
	APIKey   string `yaml:"api_key"`
	Language string `yaml:"language"`
	Proxy    string `yaml:"proxy"`
	// 每次请求间隔，避免触发限流
	QPS int `yaml:"qps"`
}

// TransferConfig 整理规则
type TransferConfig struct {
	Mode     string `yaml:"mode"`      // hardlink | copy | move | symlink
	MovieDir string `yaml:"movie_dir"` // 电影输出根目录
	TVDir    string `yaml:"tv_dir"`    // 剧集输出根目录
	AnimeDir string `yaml:"anime_dir"` // 动漫输出根目录，留空则并入 tv_dir

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

// SourceConfig 源目录
type SourceConfig struct {
	Path string `yaml:"path"`
	// Type 为空时自动判断，可填 movie / tv
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
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if err := cfg.validate(requireAPI); err != nil {
		return nil, err
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
		if strings.TrimSpace(s.Path) == "" {
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
		if s.Type != "" && s.Type != "movie" && s.Type != "tv" && s.Type != "anime" {
			return fmt.Errorf("sources[%d].type 只能是 movie / tv / anime", i)
		}
	}

	if c.Transfer.MovieDir == "" && c.Transfer.TVDir == "" && c.Transfer.AnimeDir == "" {
		return fmt.Errorf("transfer.movie_dir、tv_dir、anime_dir 至少要配置一个")
	}
	// 动漫没单独配目录时并入剧集目录，不影响使用
	if c.Transfer.AnimeDir == "" {
		c.Transfer.AnimeDir = c.Transfer.TVDir
	}
	for _, d := range []string{c.Transfer.MovieDir, c.Transfer.TVDir, c.Transfer.AnimeDir} {
		if d == "" {
			continue
		}
		abs, err := filepath.Abs(d)
		if err != nil {
			return fmt.Errorf("输出目录无效 %s: %w", d, err)
		}
		if err := os.MkdirAll(abs, 0o775); err != nil {
			return fmt.Errorf("无法创建输出目录 %s: %w", abs, err)
		}
	}
	c.Transfer.MovieDir = absOrEmpty(c.Transfer.MovieDir)
	c.Transfer.TVDir = absOrEmpty(c.Transfer.TVDir)
	c.Transfer.AnimeDir = absOrEmpty(c.Transfer.AnimeDir)

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

func absOrEmpty(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// recursive 源目录是否递归
func (s SourceConfig) recursive() bool {
	if s.Recursive == nil {
		return true
	}
	return *s.Recursive
}
