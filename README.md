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

电影、电视剧、动漫分三个根目录，互不混放。

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
- `transfer.movie_dir` / `transfer.tv_dir` / `transfer.anime_dir`：三个媒体库输出目录
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

识别不准时，把目录的 `type` 写成 `movie`、`tv` 或 `anime` 可以强制类型。

## 后缀与目录规则

**后缀**：一律使用源文件的原后缀，模板里的 `{ext}` 写不写都行。

```
The.Matrix.1999.1080p.mkv   ->  The Matrix (1999).mkv      （模板写成 .mp4 也还是 .mkv）
Some.Show.S02E05.MKV        ->  Some Show - S02E05.MKV     （原样保留大小写）
```

程序会先摘掉模板渲染出来的视频后缀，再把源后缀拼回去，所以不会出现无后缀或 `name.mkv.mkv`。

**目录**：按 TMDB 的类型分三个根目录。

| 判定 | 目标根目录 | 模板 |
|---|---|---|
| 电影，类型里没有「动画」 | `movie_dir` | `movie_*` |
| 剧集，类型里没有「动画」 | `tv_dir` | `tv_*` |
| 电影/剧集，类型里有「动画」 | `anime_dir` | `anime_*`，留空则沿用 `movie_*` / `tv_*` |
| 源目录写了 `type: anime` | 强制 `anime_dir` | 同上 |

判定顺序是：源目录显式指定 > TMDB 类型 > 文件名特征。**TMDB 类型是最终依据**，文件名像动漫（字幕组方括号、`[01]` 集数）只用于 `-parse-only` 预览时估算，不会因为名字像就误丢进动漫目录。

## 连不上 TMDB 怎么办

先别急，多数情况下**不用代理**。

`api.themoviedb.org` 在国内常被 DNS 污染，典型报错是：

```
请求 TMDB 失败: ... dial tcp [2a03:2880:...]:443: connectex: A connection attempt failed ...
```

`2a03:2880:` 开头或 `31.13.x.x` 这类 IP 是污染结果（那是 Facebook 的地址段），不是 TMDB。

**但 TMDB 有两个官方域名，走的是不同 CDN：**

| 域名 | 国内常见情况 |
|---|---|
| `api.themoviedb.org` | 常被污染，解析到 Facebook 的 IP，连不通 |
| `api.tmdb.org` | 走 CloudFront，**多数网络可以直连** |

所以程序默认按下面顺序自动探测，用第一个通的：

```
api.tmdb.org → api.themoviedb.org → tmdb.nastool.cn → tmdb.nastool.workers.dev
```

启动日志会打印实际用的是哪个域名：

```
TMDB 连接正常，使用域名 https://api.tmdb.org/3
```

先自己验一下 `api.tmdb.org` 通不通（把 KEY 换成你的）：

```bash
curl -s -o nul -w "%{http_code}\n" "https://api.tmdb.org/3/configuration?api_key=你的KEY"
```

返回 `200` 就说明能直连，什么都不用配。返回 `000` 或超时，再往下走。

**1. 走本地代理**

```yaml
tmdb:
  proxy: "http://127.0.0.1:7890"   # Clash 7890 / v2rayN 10809 / SSR 1080
```

也可以不写配置，直接设环境变量：`set HTTPS_PROXY=http://127.0.0.1:7890`

**2. 走路由器上的代理**

```yaml
  proxy: "http://192.168.31.1:7890"
```

**3. 用自己的 TMDB 反代镜像**

```yaml
  base_url: "https://your-tmdb-mirror.example.com/3"
  image_base: "https://your-tmdb-mirror.example.com/t/p/original"
```

填了 `base_url` 就只用这一个域名，不再自动探测。镜像地址经常失效，且会把你的 API Key 经过第三方，安全性自己评估。

## 已知限制

- 只做电影和剧集，不处理音乐、图片
- 动漫按剧集结构存放（`Season 01/` + `tvshow.nfo`），动画电影按电影结构存放
- 多集合并的文件（如 `S01E01-E03`）只会取第一个集数
- 没有 Web 界面，没有自动更新，纯命令行
- 极端网络下（四个 TMDB 域名都不通）仍需代理

## 从源码编译

```bash
go mod tidy
go build -trimpath -ldflags "-s -w" -o medialinker .
```

或者推一个 `v*` tag，GitHub Actions 会自动编译三平台并发布 Release。
