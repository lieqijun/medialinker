# medialinker

只做两件事的媒体整理工具：**硬链接搬运** + **TMDB 刮削**。没有站点、没有资源搜索、没有订阅。

Go 写的单文件 exe，双击就运行，内存占用几十 MB。

## 它做什么

1. 扫描你指定的下载目录
2. 从文件名解析出标题、年份、季、集
3. 到 TMDB 查详情（中文元数据）
4. 按模板硬链接到媒体库并重命名，**后缀保持源文件不变**
5. 生成 Emby / Jellyfin / Kodi 能读的 `.nfo`，下载 `poster.jpg`、`fanart.jpg`
6. 记录已处理文件，下次不重复处理

电影、电视剧、动漫自动分流到 `media_dir` 下的 `Movies` / `TVShows` / `Anime`，**源目录只要一个**，分类由程序判断。

## 下载

到 [Releases](../../releases) 下载 `medialinker-windows-amd64.zip`，解压后只有两个文件：

| 文件 | 用途 |
|---|---|
| `medialinker.exe` | 主程序，双击运行 |
| `config.yaml` | 配置文件，改三处就能用 |

只发布 Windows x86_64 版本。

## 改配置

只改三处：

```yaml
tmdb:
  api_key: "你的 TMDB Key"     # https://www.themoviedb.org/settings/api
sources:
  - path: D:\Downloads         # 下载目录，一个就够
    type: ""                   # 留空 = 自动分类
transfer:
  media_dir: D:\Media          # 媒体库根目录，只填到 media 这层
```

**路径的两种斜杠都认**：`D:\Media` 和 `D:/Media` 等价。可以直接在资源管理器里对着文件夹「复制路径」粘进来，带不带双引号都行——`"D:\work\media-linker\dist"` 这种原样粘贴也能正确识别。

`media_dir` 不用自己建子目录，启动时会自动创建 `Movies`、`TVShows`、`Anime`。

`type` 留空即可：程序先按文件名判断电影还是剧集，查到 TMDB 后**剧集**类型含「动画」就进 `Anime`，否则进 `TVShows`；**电影一律进 `Movies`**，动画电影也算电影。

## 运行

双击 `medialinker.exe`，它会自己找同目录的 `config.yaml`。

第一次建议先只看结果不写文件 —— 在文件夹地址栏输入 `cmd` 回车，然后：

```bash
medialinker.exe -dry-run -once
```

它会联网查 TMDB 并打印每个文件将要放到哪，一个字节都不写。确认没错再双击正式跑。

黑窗口别关，最小化即可。默认每 300 秒扫一次，Ctrl+C 停止。

启动时窗口会打印配置和三个媒体库目录，出错也会停住等回车，不会一闪就没。

## 参数

| 参数 | 作用 |
|---|---|
| `-config <路径>` | 配置文件，默认用 exe 同目录的 `config.yaml` |
| `-once` | 只扫一次就退出 |
| `-dry-run` | 只打印，不写文件 |
| `-parse-only` | 只解析文件名并打印结果，不联网、不写文件（没填 Key 也能跑） |
| `-interval <秒>` | 覆盖配置里的扫描间隔 |
| `-version` | 打印版本 |

不带任何参数双击运行时，退出前会停住等回车，方便看错误信息。

## 目录与后缀

**后缀**：一律用源文件原后缀，模板里 `{ext}` 写不写都行。

```
The.Matrix.1999.1080p.mkv  ->  The Matrix (1999).mkv     （模板写 .mp4 也还是 .mkv）
Some.Show.S02E05.MKV       ->  Some Show - S02E05.MKV    （大小写原样保留）
```

**目录**：

| 判定 | 目标目录 | 模板 |
|---|---|---|
| 电影（含动画电影） | `<media_dir>/Movies` | `movie_*` |
| 剧集，类型里没有「动画」 | `<media_dir>/TVShows` | `tv_*` |
| 剧集，类型里有「动画」 | `<media_dir>/Anime` | `anime_*`，留空沿用 `tv_*` |
| 源目录写了 `type: anime` 且是剧集 | 强制 `Anime` | 同上 |

判定顺序：源目录显式指定 > TMDB 类型 > 文件名特征。**TMDB 类型是最终依据**，文件名只是 `-parse-only` 预览时的估算。**动画电影按电影处理**，进 `Movies`，只有剧集形态的动画才进 `Anime`。

## 文件名识别

```
流浪地球2.2023.2160p.WEB-DL.H265.mkv          -> 流浪地球2 (2023)
The.Matrix.1999.1080p.BluRay.x264.mkv         -> The Matrix (1999)
[VCB-Studio] Title [01][1080p][Ma10p].mkv     -> Title S01E01
进击的巨人 第三季 第十二集.mkv                  -> 进击的巨人 S03E12
Some.Show.S02E05.1080p.WEB-DL.mkv             -> Some Show S02E05
律政剧.第08集.mkv                              -> 律政剧 S01E08
```

识别不准时，给源目录加 `type: movie`、`tv` 或 `anime` 强制类型。

## 硬链接

硬链接不复制数据，同一份文件在下载目录和媒体库各有一个名字，只占一份空间。

硬性前提：**源目录和媒体库必须在同一个分区**。跨分区会失败并提示，这时把 `transfer.mode` 改成 `copy`。Windows 下 NTFS 支持硬链接但不能跨盘符，`D:\Downloads` 到 `D:\Media` 可以，到 `E:\Media` 不行。

## 连不上 TMDB

API 域名固定是 `https://api.tmdb.org/3`，不需要代理，配置里也没有代理项。连不上就是这台机器的网络出不去，需要自己在系统层面解决（改 hosts、走路由器代理）。

## 已知限制

- 只处理电影和剧集，不管音乐、图片
- 多集合并的文件（`S01E01-E03`）只取第一个集数
- 纯命令行，没有 Web 界面，没有自动更新
