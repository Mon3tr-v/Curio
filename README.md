# Curio

Curio 是一个面向家庭媒体库的整理与 STRM 播放辅助工具。它可以整理本地文件，也可以通过 CloudDrive2 处理云端文件；可以接入 TMDB 识别电影、剧集和合集；可以按分类 YAML 和命名模板生成归档路径；也可以接入 115 生成 STRM，并提供 302 播放入口给 Emby 或播放器使用。

## 项目摘要

- 媒体整理：本地和 CloudDrive2 文件扫描、识别、分类、重命名、归档和字幕处理。
- 元数据识别：通过本地解析、可选 AI 初分析和 TMDB 搜索识别电影、剧集、合集，并按 YAML 分类策略输出目录结构。
- 115 STRM：按 115 CID 同步媒体库，以 115 导出目录树为唯一事实源，支持定时同步和孤儿 STRM 清理。
- 302 播放：播放器请求 Curio 后，由 Curio 使用播放器 User-Agent 获取 115 直链并返回 302，媒体流量不经过 Curio。
- Emby 反代：播放器连接 Curio 的 Emby 反代端口，Curio 拦截 PlaybackInfo、stream、download 和播放状态上报，改写为 115 302 播放并保护 STRM 播放记录。
- 运维方式：推荐 Docker Compose 部署，主要运行配置在前端设置页维护。

## 功能概览

- 本地媒体整理：扫描、识别、分类、重命名、归档、字幕同步、空目录清理。
- CloudDrive2 整理：通过 CloudDrive2 处理云端文件，支持云端移动和字幕同步。
- TMDB 识别：优先 `zh-CN`，其次 `zh-SG`，最后回退英文。
- 剧集识别：支持季、集、季偏移、集偏移，统计缺失季和缺失集。
- 合集识别：统计已有电影、缺失电影和未上映电影，未上映电影不计入缺失。
- 分类策略：通过 YAML 配置电影和剧集的二级分类。
- 命名模板：支持电影、剧集、完整合集、缺失合集模板。
- 真实媒体参数：按需通过 `ffprobe` 获取 `resolution`、`video_codec`、`audio_codec`、`audio_channels`、`hdr_format`。
- 字幕处理：识别简体和繁体字幕，并生成 `.chs`、`.cht` 后缀。
- 115 STRM：支持 CID 媒体库、目录树差异同步、定时同步、STRM 清理。
- 115 302 播放：播放时使用播放器 User-Agent 获取直链，媒体流量不经过 Curio。
- Emby 反代：提供独立端口用于拦截 Emby PlaybackInfo、stream 和 download 请求并返回 302。
- 播放诊断：记录播放器 UA、直链解析来源、302 跳转耗时、预热命中情况，便于排查兼容性。
- 任务控制：支持开始、停止、单任务锁、任务恢复、分页搜索、批量删除记录、批量重新归档。

## 快速部署

1. 准备 Docker 和 Docker Compose。

2. 下载项目：

```bash
git clone https://github.com/Mon3tr-v/Curio.git
cd Curio
```

3. 修改 `docker-compose.yml` 中的 `CURIO_PLAY_SECRET`：

```yaml
CURIO_PLAY_SECRET: "change-me"
```

建议改成一段足够长的随机字符串。它用于签名 115 播放链接。

4. 启动：

```bash
docker compose up -d
```

5. 打开 Curio：

```text
http://localhost:8080
```

默认端口：

- Web：`8080`
- Emby 反代播放入口：`18097` 映射到容器内 `8097`

## Docker Compose

仓库内的 `docker-compose.yml` 已尽量保持简单，适合 Linux、Windows Docker Desktop、极空间、绿联、飞牛 NAS 等环境。

```yaml
services:
  db:
    image: postgres:17-alpine
    container_name: curio-db
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      POSTGRES_DB: curio
      POSTGRES_USER: curio
      POSTGRES_PASSWORD: curio
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U curio -d curio"]
      interval: 5s
      timeout: 5s
      retries: 20

  redis:
    image: redis:7-alpine
    container_name: curio-redis
    command:
      - redis-server
      - --appendonly
      - "yes"
      - --maxmemory
      - "192mb"
      - --maxmemory-policy
      - allkeys-lru
    restart: unless-stopped
    volumes:
      - ./data/redis:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 5s
      retries: 20

  curio:
    image: mon3trd/curio:1.0.3
    container_name: curio
    user: "0:0"
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      CURIO_PLAY_SECRET: "change-me"
    ports:
      - "8080:8080"
      - "18097:8097"
    volumes:
      - ./data/Curio:/data/Curio
      - ./config:/config
    extra_hosts:
      - "host.docker.internal:host-gateway"
    depends_on:
      db:
        condition: service_healthy
      redis:
        condition: service_healthy
```

注意：

- 不需要在 compose 里配置 CloudDrive2 地址，进入前端设置页填写即可。
- 不需要在 compose 里配置 TMDB、115、Emby，大部分运行配置都在前端设置页维护。
- 如果你已经用旧 compose 跑过数据库，迁移到 bind mount 前先备份 PostgreSQL 数据。
- 如果 Curio 需要访问宿主机代理，通常可以在前端设置页填写 `http://host.docker.internal:7890`。Linux 或部分 NAS 不支持时，改填宿主机实际 IP。

## 首次配置

### 基础设置

位置：`设置 -> 基础`

- 入库目录：本地扫描源目录。
- 整理目录：识别成功后的归档目录。
- 失败目录：识别或移动失败后的归档目录。
- 缺失合集目录：合集未完整时的归档目录。
- TMDB API Key：用于识别电影、剧集和合集。
- 网络代理：例如 `http://192.168.1.10:7890`。
- AI 文件名识别：开启后，本地解析失败或低置信度时使用 AI 兜底；同时开启“强制使用 AI”时，扫描完目录后会先批量交给 AI 初分析。
- AI 接口地址、API Key、模型：使用 OpenAI Chat Completions 兼容写法，支持 OpenAI 官方接口和常见中转站。
- AI 文件名识别提示词：默认提示词只要求 AI 输出结构化文件名分析结果，不允许返回 TMDB ID，后续仍由 Curio 搜索 TMDB。

目录要求：

- 入库、整理、失败、缺失合集目录不要互相嵌套。
- Curio 会自动创建目录并检查读写权限。
- 非媒体文件不会进入状态机。

### CloudDrive2

位置：`设置 -> 云端`

- 服务地址：CloudDrive2 HTTP/gRPC 地址。
- 用户名、密码、Token：按 CloudDrive2 实际登录方式填写。
- 扫描根目录：云端入库目录。
- 整理目录、失败目录、缺失合集目录：云端目标目录。

点击 `测试连接` 检查连通性，点击 `整理云端` 启动云端整理任务。

### 115

位置：`设置 -> 115`

- Cookies：用于 115 Web 接口，优先用于目录树导出和部分播放兜底。
- Open Token：可以从 OpenList 导入，用于 Open API 和直链获取兜底。
- 媒体库 CID：只填写一个 115 目录 CID，Curio 只同步这个目录下的媒体。
- STRM 输出目录：生成 `.strm` 文件的位置。
- STRM 生成地址：写入 STRM 文件的 Curio 地址，例如 `http://192.168.1.10:8080`，也可以填写 Emby 或容器可访问的内网地址。
- 同步间隔：开启定时增量同步时使用。

推荐使用 Cookies 方式同步目录树，API 请求更少，也更适合大目录。Open Token 可以保留为直链和接口兜底。
目录树导出会自动剥离所选 CID 自身的顶层目录：如果 CID 指向 `media`，STRM 仍会生成到 `tv`、`movies`、`collections` 等原有层级，不会额外生成 `/strm/media/...`。

### Emby

位置：`设置 -> Emby`

- Emby 原始地址：真实 Emby 地址，例如 `http://192.168.1.10:8096` 或 `http://emby:8096`。
- 反代端口：容器内默认 `8097`，compose 默认映射为宿主机 `18097`。播放器里填写 Curio 的反代地址，例如 `http://192.168.1.10:18097`。
- API Key：可用于同步后刷新 Emby 媒体库。

Emby 挂载建议：

```yaml
services:
  emby:
    volumes:
      - ./data/Curio:/data/Curio
```

如果 Curio 的 STRM 输出目录是 `/data/Curio/strm`，Emby 媒体库也应指向同一个容器路径 `/data/Curio/strm`，这样 STRM 内的相对路径和媒体库扫描更稳定。

## 整理层级

Curio 固定使用一级媒体类型目录，再使用分类策略生成二级目录。

```text
movies / 二级分类 / 电影名 / 文件
tv / 二级分类 / 剧名 / Season xx / 文件
collections / 二级分类 / 合集名 / 电影名 / 文件
```

示例：

```text
movies/欧美电影/Inception (2010)/Inception (2010) - 2160p HEVC TrueHD 7.1.mkv
tv/日本剧集/Dark (2017)/Season 01/Dark - S01E01 - 1080p AVC EAC3 5.1.mkv
collections/欧美电影/John Wick Collection/John Wick (2014)/John Wick (2014) - 2160p HEVC.mkv
```

## 分类 YAML

位置：`分类`

配置为空或不配置时，不启用对应媒体类型的分类。分类名也是二级目录名，按配置顺序匹配，命中后停止。

```yaml
movie:
  纪录片:
    genre_ids: "99,-10402"
  演唱会:
    genre_ids: "10402"
  动画电影:
    genre_ids: "16"
  华语电影:
    original_language: "zh,cn,bo,za"
  日韩电影:
    original_language: "ja,ko,th"
  欧美电影:

tv:
  国漫:
    genre_ids: "16"
    origin_country: "CN,TW,HK"
  日漫:
    genre_ids: "16"
    origin_country: "JP"
  纪录片:
    genre_ids: "99"
  国产剧集:
    origin_country: "CN,SG"
  日本剧集:
    origin_country: "JP"
  欧美剧集:
    origin_country: "US,FR,GB,DE,ES,IT,NL,PT,RU,UK,CO"
  未分类:
```

支持字段：

- `genre_ids`：TMDB 类型 ID。
- `original_language`：原始语言。
- `origin_country`：剧集国家或地区。
- `production_countries`：电影制片国家或地区。
- `keywords`：关键词，配置后需要同时命中。

匹配规则：

- 多个条件需要同时满足。
- 逗号表示多个可选值。
- 负号表示排除，例如 `99,-10402`。
- 空分类表示兜底分类。

## 命名模板

位置：`命名`

支持四类模板：

- 电影模板
- 剧集模板
- 完整合集模板
- 缺失合集模板

常用字段：

```text
{title}
{year}
{category}
{resolution}
{source}
{video_codec}
{audio_codec}
{audio_channels}
{hdr_format}
{extension}
{show_title}
{show_year}
{season}
{season_2}
{episode}
{episode_2}
{episode_title}
{collection_name}
{collection_id}
```

真实媒体字段：

- `{resolution}`
- `{video_codec}`
- `{audio_codec}`
- `{audio_channels}`
- `{hdr_format}`

这些字段优先来自 `ffprobe`，不再依赖文件名猜测。模板没有使用技术字段时，Curio 会跳过不必要的 `ffprobe`，减少耗时。

编码规范化：

- `H265`、`H.265`、`x265` 会输出为 `HEVC`。
- `H264`、`H.264`、`x264` 会输出为 `AVC`。
- 其他编码会尽量输出标准名称。

## 115 STRM 和 302 播放

### 同步逻辑

点击 `同步 STRM` 后：

1. Curio 读取配置的 115 媒体库 CID。
2. 使用 Cookies 创建并下载 115 导出的目录树。
3. 过滤目录树里的媒体文件，计算目标 STRM 列表。
4. 对比目录树、数据库记录和本地 `.strm` 文件。
5. 只创建缺失 STRM、更新内容变化的 STRM、恢复本地缺失的 STRM，并在开启“删除缺失 STRM”时删除目录树中已不存在的本地 STRM。

开启定时同步后，Curio 会按设置的间隔自动执行同一套目录树差异同步。

### 播放逻辑

STRM 内容会指向 Curio：

```text
http://你的Curio地址/play/115/媒体文件名?token=签名
```

播放时：

1. 播放器请求 Curio。
2. Curio 校验 token。
3. Curio 使用播放器的 User-Agent 向 115 获取直链。
4. Curio 返回 302。
5. 播放器直接连接 115 播放。

媒体流量不经过 Curio 本机。

### Emby 反代播放

播放器使用 Curio 的 Emby 反代端口连接媒体库，例如：

```text
http://你的NAS地址:18097
```

反代会把 Emby 的媒体源改写为原生 `/Videos/{id}/stream` 路径，并在播放器真正起播时返回 115 直链。Curio 会保存 Emby Item 和 STRM 链接的映射，让 Hills、VidHub、Yamby、爆米花等播放器能继续走 Emby 播放记录，同时避免媒体流量经过 Curio。

为了降低首播等待，Curio 会在播放器请求详情页或 PlaybackInfo 时预热当前集直链，并额外预热同一 STRM 目录下排序后的下一集 1 条链接。预热受去重和并发限制保护，不会批量扫整季。

STRM 条目在 Emby 数据库里经常只有 `strm` 容器和空时长。Curio 会在反代里补充真实媒体时长、音轨、字幕和大小，并监听 `/Sessions/Playing`、`/Sessions/Playing/Progress`、`/Sessions/Playing/Stopped`。如果播放器退出时上报了负数、0 或过短进度，Curio 会自动取消错误的已观看状态；如果有有效进度但未达到已观看阈值，会写回续播点。

## 页面说明

- 总览：查看服务状态、当前任务、最近批次和统计。
- 扫描：启动本地整理、云端整理和停止任务。
- 处理：查看处理中或计划中的媒体。
- 完成：查看已归档媒体，支持搜索、详情、删除记录和重新归档。
- 失败：查看失败记录，支持删除数据库记录和重新归档。
- 剧集：按 TMDB 剧集聚合，查看季、集、缺失集和未播出集。
- 合集：按 TMDB 合集聚合，查看已有电影、缺失电影和未上映电影。
- 分类：编辑分类 YAML。
- 命名：编辑命名模板，查看和复制可用字段。
- 设置：按顶部 Tab 管理基础、云端、115、Emby 等设置。

## 环境变量

大多数配置都建议在前端设置页维护。compose 里通常只需要 `TZ` 和 `CURIO_PLAY_SECRET`。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `TZ` | 无 | 容器时区，推荐 `Asia/Shanghai`。 |
| `SERVER_ADDR` | `:8080` | Curio 后端监听地址。镜像内默认不需要修改。 |
| `DATABASE_URL` | `postgres://curio:curio@db:5432/curio?sslmode=disable` | PostgreSQL 连接串。使用仓库 compose 时不需要修改。 |
| `REDIS_ADDR` | `redis:6379` | Redis 地址。使用仓库 compose 时不需要修改。 |
| `REDIS_PASSWORD` | 空 | Redis 密码。 |
| `CURIO_ADMIN_TOKEN` | 空 | 后台访问 Token。配置后前端需要登录，适合公网或 FRP 暴露场景。 |
| `CURIO_PLAY_SECRET` | `curio-change-me` | 115 播放链接签名密钥，强烈建议修改。 |
| `CURIO_DATA_ROOT` | `/data/Curio` | Curio 数据根目录。 |
| `FRONTEND_DIR` | `/app/public` | 前端静态文件目录。镜像内默认不需要修改。 |
| `FRONTEND_ORIGIN` | `*` | CORS 来源。 |
| `TMDB_API_KEY` | 空 | 初始 TMDB API Key，也可以在前端设置页配置。 |
| `NETWORK_PROXY` | 空 | 初始网络代理，也可以在前端设置页配置。 |
| `AI_BASE_URL` | `https://api.openai.com/v1` | 初始 OpenAI 兼容接口地址，也可以在前端设置页配置。 |
| `AI_API_KEY` | 空 | 初始 AI API Key，保存后前端只回显隐藏值。 |
| `AI_MODEL` | `gpt-5.5` | 初始 AI 文件名识别模型名，中转站可填写自己的模型名。 |
| `TMDB_PROXY` | 空 | 兼容旧配置，优先级低于 `NETWORK_PROXY`。 |
| `HTTPS_PROXY` | 空 | 兼容系统代理，优先级低于 `NETWORK_PROXY` 和 `TMDB_PROXY`。 |
| `HTTP_PROXY` | 空 | 兼容系统代理，优先级最低。 |
| `CLOUDDRIVE_ADDR` | `http://localhost:19798` | CloudDrive2 默认地址。现在推荐在前端设置页配置。 |
| `CURIO_CD2_PROBE_MODE` | `auto` | CloudDrive2 技术参数探测模式，可选 `auto`、`direct`、`proxy`。 |
| `CURIO_CD2_PREFETCH` | 自动 | 控制 CloudDrive2 ISO 采样预取提示。 |
| `POSTGRES_DB` | 无 | PostgreSQL 初始化数据库名。compose 默认 `curio`。 |
| `POSTGRES_USER` | 无 | PostgreSQL 初始化用户名。compose 默认 `curio`。 |
| `POSTGRES_PASSWORD` | 无 | PostgreSQL 初始化密码。compose 默认 `curio`，生产环境建议修改。 |

## 常见问题

### 115 提示限流

常见原因：

- 频繁完整同步 STRM。
- Emby 正在扫描大量 STRM。
- 播放器批量探测媒体。
- 目录树导出后短时间内重复获取下载直链。

建议：

- 优先配置 Cookies，使用目录树导出。
- 降低同步频率。
- 不要连续点击完整同步。
- 等待一段时间后重试。
- 大目录优先使用 Cookies 目录树导出同步。

### 只有 Open Token 时为什么不能同步目录树

STRM 同步只使用 115 Web 的目录树导出能力，因此需要 Cookies 授权。Open Token 可以继续作为播放直链和接口兜底使用，但不再用于递归分页扫描媒体库。

### 页面只显示部分记录

列表默认分页加载，不会一次性加载全部数据库记录。使用搜索和翻页查看完整数据。

### 重新归档会删除真实文件吗

删除记录只删除数据库数据，不删除真实源文件。重新归档会按当前记录和输入参数重新识别或重新移动。

### 字幕如何命名

Curio 会识别同目录字幕，并根据语言生成后缀：

- 简体中文：`.chs`
- 繁体中文：`.cht`

无法识别的字幕会尽量保留原语言标记。

### Cookies 是否永久有效

不是。扫码获取的 Cookies 通常较稳定，但仍可能因为 115 服务端策略、IP、设备管理或账号安全策略失效。失效后重新扫码即可。

## 最近更新

### 1.0.3

- feat: 合集页新增固定“豆瓣电影 Top250”榜单合集；后端每天自动刷新榜单，优先使用豆瓣移动端 JSON 源并直连请求，按豆瓣条目解析并匹配 TMDB，前端复用现有合集列表和详情弹窗统计本地已有、缺失和未解析影片。
- fix: 修复合集已补齐后仍显示缺失、文件仍停留在 `incomplete_collections` 的问题；文件完成入库后会二次复核合集完整性，并把完整合集迁移到正式合集目录。
- feat: 合集页面新增“修复完整合集”，可修复历史数据中已完整但状态或路径仍卡在缺失合集目录的记录。
- change: 115 媒体库配置收窄为单个 CID 输入，移除多媒体库 YAML 配置分支，避免前后端配置语义不一致。
- change: STRM 同步收敛为 115 导出目录树唯一事实源，不再使用操作事件、节点缓存、Open API 分页扫描作为同步兜底。
- fix: 115“测试连接”改为分别校验 Cookies 目录树导出能力和 Open Token 有效性，避免 Open Token 可用但生成 STRM 因 Cookies 失效报 405。
- fix: 115 导出目录树会剥离所选 CID 自身的顶层目录，避免 CID 指向 `media` 时生成 `/strm/media/...` 并把原有 STRM 链接标记为不可播放。
- fix: STRM 同步新增整体前缀漂移保护，发现 `tv/...` 被错误识别成 `media/tv/...` 这类情况时会中止同步，避免批量删除原有 STRM。
- fix: STRM 同步改为对比目录树、数据库记录和本地 `.strm` 文件，只写入内容变化的文件，并在开启删除缺失时清理本地孤儿 STRM。
- change: “重建节点”操作改为复用目录树差异同步，不再触发慢速节点重建扫描。
- fix: 115 目录树文本只作为路径快照，不再清空带 `file_id/pickcode` 的增量节点缓存。
- fix: 115 导出目录树下载增加多路直链兜底和 403/临时错误重试。
- fix: 目录树解析改为按层级识别目录，兼容 `梅根2.0 (2025)` 这类带点号的文件夹。
- feat: 新增 OpenAI 兼容的 AI 文件名识别配置；可选择本地解析失败时兜底，或扫描后强制先批量交给 AI 初分析，再由 Curio 搜索 TMDB。
- feat: 新增 AI 识别日志和统一日志页，可查看请求模型、接口地址、代理、提示词摘要、AI 返回、解析结果和错误原因。
- feat: 文件名解析器参考 GuessIt、Anitomy/Anitopy 思路扩展，增强电影、剧集、动画、版本、分集、技术字段和多版本文件名识别。
- fix: 技术字段以 `ffprobe` 真实媒体信息为准，重命名不再直接信任 AI 或文件名里的分辨率、编码、音轨写法。
- fix: Emby 反代补充真实 `RunTimeTicks`、`MediaStreams`、`Size`，并新增 STRM 播放状态纠偏，避免 Hills、Yamby 等客户端点开即退时被错误标记已观看。

### 1.0.2

- feat: 优化 115 + Emby 302 播放链路，兼容 Hills、VidHub、Yamby、爆米花等播放器的 PlaybackInfo、stream、download 请求。
- feat: 播放时继承真实播放器 User-Agent 获取 115 直链，并补充 DirectStreamUrl、MediaStreams、播放记录映射等 Emby 兼容信息。
- feat: 新增播放诊断日志接口，可查看直链解析来源、UA、302 跳转耗时、预热命中和失败原因。
- feat: 新增当前集和下一集预热，降低连续播放时的起播等待。
- feat: Emby 设置页移除“对外地址”，仅保留原始地址、API Key、反代端口；115 页面使用“STRM 生成地址”控制写入 STRM 的访问地址。
- fix: 修复 toast 从页面中间弹出的问题，恢复右上角提示位置。

### 1.0.1 及以前

- 新增可选后台 Token 鉴权，适合公网或 FRP 暴露场景。
- 敏感设置返回前端时进行脱敏，减少泄露风险。
- 115 操作记录切换到新事件接口，并修复游标、分页、去重和增量同步一致性。
- 恢复并增强 115 302 播放，支持 Cookies、Open Token 和多客户端兜底。
- 新增 115 STRM 定时同步和过期 STRM 清理。
- 优化目录树同步，优先使用 Cookies 导出目录树，减少大目录 API 请求。
- 优化 CloudDrive2 探测和真实媒体参数读取，避免不需要技术字段时执行 `ffprobe`。
- 优化日漫和中文剧集文件名清理，减少把发布组或根目录误识别为片名。
- 优化字幕简繁识别和 `.chs`、`.cht` 后缀生成。
- 优化处理、完成、失败页面的搜索、分页、详情弹窗、批量删除和重新归档。
- 优化剧集和合集详情，补充缺失季、缺失集、未播出集和未上映电影统计。
- 重新整理设置页为顶部 Tab 分组，并统一按钮、图标、圆角和提示风格。

## 安全建议

- 不要公开 TMDB Key、115 Cookies、Open Token、Emby API Key。
- `CURIO_PLAY_SECRET` 必须修改为随机长字符串。
- 如果通过公网或 FRP 暴露 Curio，建议配置 `CURIO_ADMIN_TOKEN`。
- 115 播放 URL 中的 `token` 有播放权限，不要公开分享。
- Emby 反代端口只建议暴露给可信网络。
