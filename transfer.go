package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var reTplVar = regexp.MustCompile(`\{(\w+)(?::([^}]+))?\}`)

// renderTemplate 渲染 {var} / {var:02} 形式的模板
func renderTemplate(tpl string, vars map[string]string) string {
	return reTplVar.ReplaceAllStringFunc(tpl, func(m string) string {
		sub := reTplVar.FindStringSubmatch(m)
		key := sub[1]
		val, ok := vars[key]
		if !ok {
			return ""
		}
		// 扩展名自带前导点，不能走 sanitizeName（它会 Trim 掉点）
		if key == "ext" {
			return val
		}
		if sub[2] != "" && val != "" {
			if n, err := strconv.Atoi(val); err == nil {
				// 支持 02 / 2 这类宽度写法
				width := strings.TrimPrefix(sub[2], "0")
				if w, err := strconv.Atoi(width); err == nil && w > 0 {
					return fmt.Sprintf("%0*d", w, n)
				}
			}
		}
		return sanitizeName(val)
	})
}

// sanitizeName 去掉文件名里非法字符
func sanitizeName(s string) string {
	s = strings.NewReplacer(
		"/", " ", "\\", " ", ":", " ", "*", " ", "?", " ",
		"\"", " ", "<", " ", ">", " ", "|", " ",
	).Replace(s)
	s = collapseSpaces(s)
	return strings.Trim(s, " .")
}

// 渲染结果里可能留下空括号，例如年份缺失时 "{title} ({year})" -> "Title ()"
var reEmptyParen = regexp.MustCompile(`\(\s*\)|\[\s*\]|（\s*）|【\s*】`)

// tidyPart 清理渲染后残留的空括号与多余分隔符
func tidyPart(s string) string {
	s = reEmptyParen.ReplaceAllString(s, "")
	s = collapseSpaces(s)
	return strings.Trim(s, " .-_")
}

// TargetPaths 整理后的落盘位置
type TargetPaths struct {
	Root string // Movies / TVShows / Anime 之一
	Show string // 作品目录，NFO 与海报放这里
	Dir  string // 文件所在目录（剧集含 Season 子目录）
	File string // 文件名
}

// Name 完整文件路径
func (t TargetPaths) Name() string { return filepath.Join(t.Dir, t.File) }

// targetPaths 计算目标目录与文件名
func targetPaths(p *ParsedMedia, m *MediaInfo, cfg *Config) (TargetPaths, error) {
	var tp TargetPaths

	srcExt := p.ext()
	vars := map[string]string{
		"title":      m.Title,
		"original":   m.OriginalTitle,
		"year":       intOrEmpty(m.Year),
		"season":     strconv.Itoa(p.Season),
		"episode":    strconv.Itoa(p.Episode),
		"resolution": p.Resolution,
		"ext":        srcExt,
		"tmdbid":     strconv.Itoa(m.TMDBID),
		"group":      p.Group,
	}

	tc := cfg.Transfer
	var rootDir, folderTpl, fileTpl, seasonTpl string

	// 三个根目录都由 media_dir 派生：<media_dir>/Movies | TVShows | Anime
	// 动画电影按电影处理（进 Movies），只有剧集形态的动画才进 Anime
	switch {
	case p.Kind == KindMovie:
		rootDir, folderTpl, fileTpl = tc.MovieRoot(), tc.MovieFolder, tc.MovieFile
	case m.Anime:
		rootDir = tc.AnimeRoot()
		folderTpl = firstNonEmpty(tc.AnimeFolder, tc.TVFolder)
		fileTpl = firstNonEmpty(tc.AnimeFile, tc.TVFile)
		seasonTpl = tc.SeasonFolder
	default:
		rootDir, folderTpl, fileTpl = tc.TVRoot(), tc.TVFolder, tc.TVFile
		seasonTpl = tc.SeasonFolder
	}

	if rootDir == "" {
		return tp, fmt.Errorf("没有可用的输出目录，请检查 transfer.media_dir 配置")
	}

	tp.Root = rootDir
	tp.Show = filepath.Join(rootDir, tidyPart(renderTemplate(folderTpl, vars)))
	if tp.Show == rootDir {
		// 模板渲染为空时退回根目录，避免文件散落在根上
		tp.Show = filepath.Join(rootDir, tidyPart(m.Title))
	}
	tp.Dir = tp.Show
	if seasonTpl != "" {
		if season := tidyPart(renderTemplate(seasonTpl, vars)); season != "" {
			tp.Dir = filepath.Join(tp.Show, season)
		}
	}
	tp.File = finalizeFileName(renderTemplate(fileTpl, vars), srcExt, m)
	if tp.File == "" {
		return tp, fmt.Errorf("文件名模板渲染结果为空，请检查 transfer 配置")
	}
	return tp, nil
}

// finalizeFileName 收尾：后缀一律取源文件，模板里写什么后缀都不生效。
// 这样即使模板忘了写 {ext}、或者写成 .mp4，落盘也不会出现无后缀或后缀被改的情况。
func finalizeFileName(rendered, srcExt string, m *MediaInfo) string {
	name := strings.TrimSpace(rendered)
	// 先摘掉模板可能带出来的视频后缀，避免 name.mkv.mkv
	if e := filepath.Ext(name); e != "" && videoExts[strings.ToLower(e)] {
		name = strings.TrimSuffix(name, e)
	}
	name = tidyPart(name)
	if name == "" {
		name = tidyPart(fmt.Sprintf("%s (%d)", m.Title, m.Year))
	}
	if name == "" {
		return ""
	}
	return name + srcExt
}

// MediaInfo 从 TMDB 得到的最终信息（电影与剧集共用）
type MediaInfo struct {
	Kind          MediaKind
	Title         string
	OriginalTitle string
	Year          int
	TMDBID        int
	Anime         bool // 是否归入动漫目录
}

// Transfer 把文件按规则硬链接/复制到媒体库，返回最终文件路径
func Transfer(src string, tp TargetPaths, cfg *Config, log *Logger) (string, error) {
	if err := os.MkdirAll(tp.Dir, 0o775); err != nil {
		return "", fmt.Errorf("创建目录失败 %s: %w", tp.Dir, err)
	}

	dst := tp.Name()

	// 已经存在同一硬链接（同 inode）视为已完成
	if sameFile(src, dst) {
		return dst, nil
	}

	finalDst, err := resolveConflict(dst, cfg.Transfer.Overwrite)
	if err != nil {
		return "", err
	}
	if finalDst == "" {
		return dst, nil // skip
	}

	switch cfg.Transfer.Mode {
	case "hardlink":
		if err := os.Link(src, finalDst); err != nil {
			if isCrossDevice(err) {
				return "", fmt.Errorf("硬链接失败（源和目标不在同一分区）: %w\n请把下载目录和媒体库放在同一存储空间，或改用 copy 模式", err)
			}
			if isExists(err) {
				return finalDst, nil
			}
			return "", fmt.Errorf("硬链接失败: %w", err)
		}
	case "symlink":
		if err := os.Symlink(src, finalDst); err != nil {
			return "", fmt.Errorf("软链接失败: %w", err)
		}
	case "copy":
		if err := copyFile(src, finalDst); err != nil {
			return "", fmt.Errorf("复制失败: %w", err)
		}
	case "move":
		if err := moveFile(src, finalDst); err != nil {
			return "", fmt.Errorf("移动失败: %w", err)
		}
	default:
		return "", fmt.Errorf("未知的 transfer.mode: %s", cfg.Transfer.Mode)
	}

	log.Infof("已整理: %s -> %s", filepath.Base(src), finalDst)

	// 顺带把同名字幕一起带过去
	copySideSubtitles(src, tp.Dir, finalDst, cfg.Transfer.Mode, log)

	return finalDst, nil
}

// resolveConflict 处理目标已存在的情况。返回空字符串表示跳过。
func resolveConflict(dst, policy string) (string, error) {
	st, err := os.Stat(dst)
	if err != nil {
		return dst, nil
	}
	if st.IsDir() {
		return "", fmt.Errorf("目标路径是目录，无法写入文件: %s", dst)
	}

	switch policy {
	case "skip":
		return "", nil
	case "replace":
		if err := os.Remove(dst); err != nil {
			return "", fmt.Errorf("删除已存在文件失败: %w", err)
		}
		return dst, nil
	case "suffix":
		ext := filepath.Ext(dst)
		base := strings.TrimSuffix(dst, ext)
		for i := 1; i < 100; i++ {
			cand := fmt.Sprintf("%s (%d)%s", base, i, ext)
			if _, err := os.Stat(cand); os.IsNotExist(err) {
				return cand, nil
			}
		}
		return "", fmt.Errorf("同名文件过多: %s", dst)
	}
	return dst, nil
}

// sameFile 判断两个路径是否指向同一个 inode（已硬链接过）
func sameFile(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(sa, sb)
}

func isCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "cross-device") ||
		strings.Contains(s, "invalid cross-device link") ||
		strings.Contains(s, "different disk")
}

func isExists(err error) bool {
	return err != nil && os.IsExist(err)
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o664)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		// 复制中断时清理掉半个文件，避免下次被当成"已存在"
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Sync()
}

// moveFile 先尝试 rename，失败则复制后删除
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// copySideSubtitles 把同名外挂字幕一并带过去
func copySideSubtitles(srcFile, dstDir, dstFile, mode string, log *Logger) {
	base := strings.TrimSuffix(filepath.Base(srcFile), filepath.Ext(srcFile))
	dir := filepath.Dir(srcFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".srt" && ext != ".ass" && ext != ".ssa" && ext != ".sub" {
			continue
		}
		if !strings.HasPrefix(strings.TrimSuffix(name, filepath.Ext(name)), base) {
			continue
		}
		src := filepath.Join(dir, name)
		dstBase := strings.TrimSuffix(filepath.Base(dstFile), filepath.Ext(dstFile))
		dst := filepath.Join(dstDir, dstBase+filepath.Ext(name))
		if mode == "move" {
			_ = os.Rename(src, dst)
		} else if _, err := os.Stat(dst); os.IsNotExist(err) {
			if err := copyFile(src, dst); err != nil {
				log.Warnf("字幕复制失败 %s: %v", name, err)
			}
		}
	}
}

// createFile 创建用于写出的文件
func createFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o775); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o664)
}

func intOrEmpty(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}
