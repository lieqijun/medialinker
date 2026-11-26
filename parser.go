package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// MediaKind 媒体类型
type MediaKind int

const (
	KindUnknown MediaKind = iota
	KindMovie
	KindTV
)

func (k MediaKind) String() string {
	switch k {
	case KindMovie:
		return "电影"
	case KindTV:
		return "剧集"
	}
	return "未知"
}

// ParsedMedia 从文件名解析出的信息
type ParsedMedia struct {
	Kind       MediaKind
	Title      string // 清理后的标题，用于 TMDB 搜索
	Year       int
	Season     int
	Episode    int
	Resolution string
	Source     string
	VideoCodec string
	AudioCodec string
	Group      string
	Ext        string
	Raw        string // 原始文件名
}

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".ts": true, ".m2ts": true,
	".avi": true, ".wmv": true, ".mov": true, ".flv": true,
	".rmvb": true, ".webm": true, ".iso": true, ".m4v": true,
}

func isVideoFile(name string) bool {
	return videoExts[strings.ToLower(filepath.Ext(name))]
}

var (
	reYearParen = regexp.MustCompile(`[\(\[]((?:19|20)\d{2})[\)\]]`)

	reSeasonEp  = regexp.MustCompile(`(?i)S(\d{1,2})[\s._\-]?E(\d{1,4})`)
	reSeasonEp2 = regexp.MustCompile(`(?i)(?:^|[\s._\-])(\d{1,2})x(\d{2,4})(?:[\s._\-]|$)`)
	reCnSeason  = regexp.MustCompile(`第\s*([0-9零〇一二三四五六七八九十百千两]+)\s*季`)
	reCnEpisode = regexp.MustCompile(`第\s*([0-9零〇一二三四五六七八九十百千两]+)\s*[集话話期]`)
	reEPNum     = regexp.MustCompile(`(?i)(?:^|[\s._\-])(?:EP|E)[\s._\-]?(\d{1,4})(?:[\s._\-]|$)`)
	reDashEp    = regexp.MustCompile(`[\s._\-]+(\d{1,4})(?:v\d)?\s*$`)

	reResolution = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])(2160p|1080p|1080i|720p|576p|480p|4k|8k)(?:[\s._\-\]\),]|$)`)
	reSourceTag  = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])(BluRay|Blu-ray|BDRip|REMUX|WEB-DL|WEBDL|WEBRip|WEB|HDTV|HDRip|DVDRip|DVD|UHD)(?:[\s._\-\]\),]|$)`)
	reVCodecTag  = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])(x264|x265|H\.?264|H\.?265|HEVC|AVC|AV1|VP9)(?:[\s._\-\]\),]|$)`)
	reACodecTag  = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])(DTS[\- ]?HD|DTS|TrueHD|Atmos|EAC3|AC3|DDP?5\.1|AAC|FLAC|LPCM|MP3)(?:[\s._\-\]\),]|$)`)
	reHDRTag     = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])(HDR10\+?|HDR|DoVi|DV|SDR)(?:[\s._\-\]\),]|$)`)

	reBracket   = regexp.MustCompile(`\[[^\]]*\]|【[^】]*】|（[^）]*）`)
	reGroupTail = regexp.MustCompile(`[\s._\-]+([A-Za-z][A-Za-z0-9@._\-]{1,30})$`)
	// 方括号里的纯数字，动漫常见写法：[01] [12]
	reBracketEp = regexp.MustCompile(`\[(\d{1,4})\]|【(\d{1,4})】`)
)

// ParseFileName 解析文件名
func ParseFileName(name string) *ParsedMedia {
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))

	p := &ParsedMedia{Ext: ext, Raw: name}
	work := base

	// 1. 抓取技术标记（抓完即从工作串移除，避免干扰标题）
	p.Resolution = grabAndRemove(&work, reResolution, true)
	p.Source = grabAndRemove(&work, reSourceTag, true)
	p.VideoCodec = strings.ReplaceAll(grabAndRemove(&work, reVCodecTag, true), ".", "")
	p.AudioCodec = grabAndRemove(&work, reACodecTag, false)
	removeAll(&work, reHDRTag)

	// 2. 年份（必须在删括号之前提取，否则 "(2019)" 会被当成分组一起删掉）
	p.Year, work = lastYear(work)

	// 2.1 方括号里的纯数字多半是集数（[01]），删括号前先记下
	bracketEp := 0
	if m := reBracketEp.FindStringSubmatch(work); m != nil {
		if n, _ := strconv.Atoi(firstNonEmpty(m[1], m[2])); n > 0 && n < 1000 {
			bracketEp = n
		}
	}

	// 3. 移除方括号分组（字幕组 / 语言 / 来源），第一个当作压制组
	if m := reBracket.FindString(work); m != "" {
		inner := strings.Trim(m, "[]【】（）")
		if p.Group == "" && len([]rune(inner)) >= 2 {
			p.Group = inner
		}
	}
	removeAll(&work, reBracket)

	// 4. 季集
	epFound := false
	if m := reSeasonEp.FindStringSubmatch(work); m != nil {
		p.Season, _ = strconv.Atoi(m[1])
		p.Episode, _ = strconv.Atoi(m[2])
		epFound = true
		removeAll(&work, reSeasonEp)
	}
	if !epFound {
		if m := reSeasonEp2.FindStringSubmatch(work); m != nil {
			p.Season, _ = strconv.Atoi(m[1])
			p.Episode, _ = strconv.Atoi(m[2])
			epFound = true
			removeAll(&work, reSeasonEp2)
		}
	}
	if m := reCnSeason.FindStringSubmatch(work); m != nil {
		if n := chineseToNumber(m[1]); n > 0 {
			p.Season = n
		}
		removeAll(&work, reCnSeason)
	}
	if m := reCnEpisode.FindStringSubmatch(work); m != nil {
		if n := chineseToNumber(m[1]); n > 0 {
			p.Episode = n
			epFound = true
		}
		removeAll(&work, reCnEpisode)
	}
	if !epFound {
		if m := reEPNum.FindStringSubmatch(work); m != nil {
			p.Episode, _ = strconv.Atoi(m[1])
			epFound = true
			removeAll(&work, reEPNum)
		}
	}
	if !epFound {
		// "标题 - 01" 形式（番剧）
		trimmed := strings.TrimSpace(work)
		if m := reDashEp.FindStringSubmatch(trimmed); m != nil {
			n, _ := strconv.Atoi(m[1])
			// 排除 "1917" 这种年份感数字
			if n > 0 && n < 1900 {
				p.Episode = n
				epFound = true
				work = trimmed[:len(trimmed)-len(m[0])]
			}
		}
	}

	// 方括号集数兜底（SxxExx / 第N集 都没匹配到时）
	if !epFound && bracketEp > 0 {
		p.Episode = bracketEp
		epFound = true
	}

	// 5. 尾部压制组
	if m := reGroupTail.FindStringSubmatch(work); m != nil {
		group := m[1]
		if p.Group == "" && looksLikeReleaseGroup(group) {
			p.Group = group
		}
		// 无论像不像组名，只要匹配到“-XXX”结尾就先不删，
		// 交给标题清理时按组名规则决定
		if looksLikeReleaseGroup(group) {
			work = work[:len(work)-len(m[0])]
		}
	}

	// 6. 清洗
	work = strings.NewReplacer(".", " ", "_", " ", "　", " ").Replace(work)
	work = collapseSpaces(work)
	work = strings.Trim(work, " -—–·")

	if work == "" {
		work = collapseSpaces(strings.NewReplacer(".", " ", "_", " ").Replace(base))
	}
	p.Title = work

	// 7. 类型
	if epFound || p.Episode > 0 {
		p.Kind = KindTV
		if p.Season == 0 {
			p.Season = 1
		}
	} else {
		p.Kind = KindMovie
	}
	return p
}

// grabAndRemove 抓取第一个匹配并把它从串里移除
func grabAndRemove(work *string, re *regexp.Regexp, upper bool) string {
	m := re.FindStringSubmatch(*work)
	if m == nil {
		return ""
	}
	val := strings.TrimSpace(m[1])
	if upper {
		val = strings.ToUpper(val)
	}
	removeAll(work, re)
	return val
}

func removeAll(work *string, re *regexp.Regexp) {
	*work = re.ReplaceAllString(*work, " ")
}

// lastYear 取文本中最后一个像年份的独立数字，并把该数字抹掉。
// 用逐字符扫描而不是正则：正则里的分隔符会被匹配消耗掉，
// 像 "1917.2019" 这种相邻年份只有前一个能匹配上。
func lastYear(s string) (int, string) {
	// 括号里的年份优先
	if ms := reYearParen.FindAllStringSubmatchIndex(s, -1); len(ms) > 0 {
		last := ms[len(ms)-1]
		y, _ := strconv.Atoi(s[last[2]:last[3]])
		if plausibleYear(y) {
			return y, s[:last[0]] + " " + s[last[1]:]
		}
	}

	r := []rune(s)
	bestPos, bestYear := -1, 0
	for i := 0; i+4 <= len(r); i++ {
		digits := true
		for k := 0; k < 4; k++ {
			if r[i+k] < '0' || r[i+k] > '9' {
				digits = false
				break
			}
		}
		if !digits {
			continue
		}
		if i > 0 && !isYearSep(r[i-1]) {
			continue
		}
		if i+4 < len(r) && !isYearSep(r[i+4]) {
			continue
		}
		y := 0
		for k := 0; k < 4; k++ {
			y = y*10 + int(r[i+k]-'0')
		}
		if plausibleYear(y) {
			bestPos, bestYear = i, y
		}
	}
	if bestPos >= 0 {
		return bestYear, string(r[:bestPos]) + " " + string(r[bestPos+4:])
	}
	return 0, s
}

func isYearSep(r rune) bool {
	switch r {
	case ' ', '.', '_', '-', '[', ']', '(', ')', '【', '】', '（', '）':
		return true
	}
	return false
}

func plausibleYear(y int) bool { return y >= 1900 && y <= 2100 }

// looksLikeReleaseGroup 判断尾部片段像不像压制组名。
// 规则：至少两个大写字母，或含数字且含大写，避免把 "Spider-Man"、"Batman" 里的词当组名删掉。
func looksLikeReleaseGroup(s string) bool {
	if s == "" {
		return false
	}
	upper, digit := 0, 0
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= '0' && r <= '9':
			digit++
		}
	}
	if upper >= 2 {
		return true
	}
	return digit > 0 && upper >= 1
}

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// String 便于日志输出
func (p *ParsedMedia) String() string {
	if p.Kind == KindTV {
		return fmt.Sprintf("[剧集] %s S%02dE%02d 年份=%d %s", p.Title, p.Season, p.Episode, p.Year, p.Resolution)
	}
	return fmt.Sprintf("[电影] %s 年份=%d %s", p.Title, p.Year, p.Resolution)
}
