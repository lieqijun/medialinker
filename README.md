# medialinker

只做两件事的媒体整理工具：**硬链接搬运** + **TMDB 刮削**。没有站点、没有资源搜索、没有订阅。

Go 编写，单文件可执行，内存占用通常几十 MB。

## 它做什么

1. 扫描你指定的下载目录
2. 从文件名解析出标题、年份、季、集
3. 到 TMDB 查详情（中文元数据）
4. 按模板把文件硬链接到媒体库目录（重命名为规范名称）
5. 生成 Emby / Jellyfin / Kodi 能读的 `.nfo`，下载 `poster.jpg`、`fanart.jpg`
6. 记录已处理文件，下次不会重复处理

## 下载

到 [Releases](../../releases) 下载对应文件：

| 文件 | 用途 |
|---|---|
| `medialinker-windows-amd64.exe` | Windows x86_64 |
| `medialinker-linux-amd64` | Linux NAS / 服务器 x86_64 |
| `medialinker-linux-arm64` | ARM64（如部分群晖、树莓派） |

也可以用 Actions 页面里任意一次构建的 Artifact。

## 使用

**1. 准备配置**

把 `config.example.yaml` 复制成 `config.yaml`，改三处：

- `tmdb.api_key`：到 <https://www.themoviedb.org/settings/api> 申请
- `transfer.movie_dir` / `transfer.tv_dir`：媒体库输出目录
- `sources`：你的下载目录

**2. 先验识别率（不需要 API Key）**

```bash
medialinker.exe -config config.yaml -parse-only -once
```

只解析文件名、打印识别出的标题和季集，不联网也不写文件。识别不准时先调文件名，或给源目录加 `type: movie` / `type: tv` 强制类型。

**3. 再干跑一次看目标路径**

```bash
medialinker.exe -config config.yaml -once -dry-run
```

只打印将要生成的目标路径，不写任何文件。

**4. 确认无误后正式跑**

```bash
medialinker.exe -config config.yaml
```

默认每 300 秒扫一次，Ctrl+C 退出。

## 命令参数

| 参数 | 作用 |
|---|---|
| `-config <路径>` | 配置文件，默认 `config.yaml` |
| `-once` | 只扫一次就退出 |
| `-dry-run` | 只打印，不写文件 |
| `-parse-only` | 只解析文件名并打印识别结果，不联网、不写文件（没填 API Key 也能跑） |
| `-interval <秒>` | 覆盖配置里的扫描间隔 |
| `-version` | 打印版本 |

## 关于硬链接

硬链接不复制数据，同一份文件在下载目录和媒体库各有一个名字，只占一份空间。

硬性前提：**源目录和媒体库必须在同一个分区**。跨分区会失败，程序会提示，这时把 `transfer.mode` 改成 `copy`。

Windows 下 NTFS 支持硬链接，但**不能跨盘符**。`D:\Downloads` 和 `D:\Media` 可以，`D:\Downloads` 到 `E:\Media` 不行。

## 文件名识别

支持这些常见形式：

```
流浪地球2.2023.2160p.WEB-DL.H265.mkv          -> 流浪地球2 (2023)
The.Matrix.1999.1080p.BluRay.x264.mkv         -> The Matrix (1999)
[VCB-Studio] Title [01][1080p][Ma10p].mkv     -> Title S01E01
进击的巨人 第三季 第十二集.mkv                  -> 进击的巨人 S03E12
Some.Show.S02E05.1080p.WEB-DL.mkv             -> Some Show S02E05
律政剧.第08集.mkv                              -> 律政剧 S01E08
```

识别不准时，把目录的 `type` 写成 `movie` 或 `tv` 可以强制类型。

## 已知限制

- 只做电影和剧集，不处理音乐、图片
- 多集合并的文件（如 `S01E01-E03`）只会取第一个集数
- 没有 Web 界面，没有自动更新，纯命令行
- TMDB 在国内需要代理，在 `tmdb.proxy` 里填

## 从源码编译

```bash
go mod tidy
go build -trimpath -ldflags "-s -w" -o medialinker .
```

或者推一个 `v*` tag，GitHub Actions 会自动编译三平台并发布 Release。
