# medialinker

只做两件事的媒体整理工具：**硬链接搬运** + **TMDB 刮削**。没有站点、没有资源搜索、没有订阅。

Go 编写，单文件可执行，内存占用通常几十 MB。

## 它做什么

1. 扫描你指定的下载目录
2. 从文件名解析出标题、年份、季、集
3. 到 TMDB 查详情（中文元数据）
4. 按模板把文件硬链接到媒体库目录（重命名为规范名称，**后缀保持源文件不变**）
5. 生成 Emby / Jellyfin / Kodi 能读的 `.nfo`，下载 `poster.jpg`、`fanart.jpg`
6. 记录已处理文件，下次不会重复处理

电影、电视剧、动漫自动分流到 `media_dir` 下的 `Movies` / `TVShows` / `Anime` 三个子目录，互不混放。**源目录只需要一个**，分类由程序判断。

## 下载

到 [Releases](../../releases) 下载 `medialinker-windows-amd64.zip`，解压后是四个文件：

| 文件 | 用途 |
|---|---|
| `medialinker.exe` | 主程序 |
| `config.yaml` | 配置文件，改三处就能用 |
| `run.bat` | 双击启动 |
| `run-dryrun.bat` | 双击试运行（只查 TMDB、只打印目标路径，不写文件） |

只发布 Windows x86_64 版本。

## 使用

**1. 改配置**

用记事本打开 `config.yaml`，只改三处：

```yaml
tmdb:
  api_key: "你的 TMDB Key"      # 到 https://www.themoviedb.org/settings/api 申请
transfer:
  media_dir: "D:/Media"        # 媒体库根目录，只填到 media 这层
sources:
  - path: "D:/Downloads"       # 你的下载目录，一个就够
    type: ""                   # 留空 = 自动分类
```

`media_dir` 不用自己建子目录，程序启动时会自动创建 `Movies`、`TVShows`、`Anime` 三个。

`type` 留空即可：程序先按文件名判断是电影还是剧集，查到 TMDB 后如果类型含「动画」就进 `Anime`，否则进 `Movies` 或 `TVShows`。也就是说**一个下载目录就能自动分成三类**，不需要预先分好。

**2. 双击 `run-dryrun.bat` 先看结果**

会联网查 TMDB 并打印每个文件将要放到的位置，一个字节都不写。第一次务必先跑这个。

**3. 确认无误后双击 `run.bat`**

黑窗口不要关，最小化即可。默认每 300 秒扫一次，Ctrl+C 停止。

也可以在命令行里跑：

```bash
# 只看识别结果，不联网（连 API Key 都不用填）
medialinker.exe -config config.yaml -parse-only -once

# 连 TMDB 但不写文件
medialinker.exe -config config.yaml -once -dry-run

# 正式跑
medialinker.exe -config config.yaml
```

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

识别不准时，把目录的 `type` 写成 `movie`、`tv` 或 `anime` 可以强制类型。

## 后缀与目录规则

**后缀**：一律使用源文件的原后缀，模板里的 `{ext}` 写不写都行。

```
The.Matrix.1999.1080p.mkv   ->  The Matrix (1999).mkv      （模板写成 .mp4 也还是 .mkv）
Some.Show.S02E05.MKV        ->  Some Show - S02E05.MKV     （原样保留大小写）
```

程序会先摘掉模板渲染出来的视频后缀，再把源后缀拼回去，所以不会出现无后缀或 `name.mkv.mkv`。

**目录**：`media_dir` 下固定三个子目录，按 TMDB 类型自动分流。

| 判定 | 目标目录 | 模板 |
|---|---|---|
| 电影，类型里没有「动画」 | `<media_dir>/Movies` | `movie_*` |
| 剧集，类型里没有「动画」 | `<media_dir>/TVShows` | `tv_*` |
| 电影/剧集，类型里有「动画」 | `<media_dir>/Anime` | `anime_*`，留空则沿用 `movie_*` / `tv_*` |
| 源目录写了 `type: anime` | 强制 `Anime` | 同上 |

判定顺序是：源目录显式指定 > TMDB 类型 > 文件名特征。**TMDB 类型是最终依据**，文件名像动漫（字幕组方括号、`[01]` 集数）只用于 `-parse-only` 预览时估算，不会因为名字像就误丢进动漫目录。

三个子目录名是固定的，不需要也不能在配置里改。

## 连不上 TMDB 怎么办

程序默认用 `https://api.tmdb.org/3`，**不需要代理**。

容易被坑的是另一个官方域名 `api.themoviedb.org`：国内常被 DNS 污染，解析到 `31.13.x.x` 或 `2a03:2880:` 开头（Facebook 的地址段），报错长这样：

```
请求 TMDB 失败（域名 https://api.themoviedb.org/3）: ... dial tcp [2a03:2880:...]:443 ...
```

看到这个就说明域名不对，不是网络封了 TMDB。两个官方域名走不同 CDN：

| 域名 | 国内实测 |
|---|---|
| `api.tmdb.org` | 走 CloudFront，可直连 |
| `api.themoviedb.org` | 被污染，解析到 Facebook 的 IP，超时 |
| `tmdb.nastool.cn` | 域名已失效（NXDOMAIN） |
| `tmdb.nastool.workers.dev` | 被污染，同样解析到 Facebook 的 IP |

后两个是 nas-tools 里内置的镜像，实测都不能用，所以本程序没有采用。

自己验一下（把 KEY 换成你的）：

```bash
curl -s -o nul -w "%{http_code}\n" "https://api.tmdb.org/3/configuration?api_key=你的KEY"
```

返回 `200` 说明通。返回 `000` 或超时才需要想别的办法 —— 在 `tmdb.base_url` 填一个你自己能访问的 TMDB 反代即可，镜像地址经常失效，且会把 API Key 经过第三方，安全性自己评估。

## 已知限制

- 只做电影和剧集，不处理音乐、图片
- 动漫按剧集结构存放（`Season 01/` + `tvshow.nfo`），动画电影按电影结构存放
- 多集合并的文件（如 `S01E01-E03`）只会取第一个集数
- 没有 Web 界面，没有自动更新，纯命令行

## 从源码编译

```bash
go mod tidy
go build -trimpath -ldflags "-s -w" -o medialinker.exe .
```

或者推一个 `v*` tag，GitHub Actions 会自动编译 Windows 版并打包成 `medialinker-windows-amd64.zip` 发布到 Release。
