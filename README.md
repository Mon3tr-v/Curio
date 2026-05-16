# Curio

Curio 是一个面向家庭媒体库的整理与 STRM 播放辅助工具。它可以整理本地文件，也可以通过 CloudDrive2 处理云端文件；可以接入 TMDB 识别电影、剧集和合集；可以按分类 YAML 和命名模板生成归档路径；也可以接入 115 生成 STRM，并提供 302 播放入口给 Emby 或播放器使用。

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
- 115 STRM：支持 CID 媒体库、目录树同步、操作记录增量同步、定时同步、STRM 清理。
- 115 302 播放：播放时使用播放器 User-Agent 获取直链，媒体流量不经过 Curio。
- Emby 反代：提供独立端口用于拦截 Emby 播放请求并返回 302。
- 任务控制：支持开始、停止、单任务锁、任务恢复、分页搜索、批量删除记录、批量重新归档。
- 前端体验：Google 风格布局，设置页顶部 Tab 分组，按钮和图标统一视觉。

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
    image: mon3trd/curio:1.0.1
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
- 网络代理：例如 `http://192.168.31.251:7890`。

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
- Curio 外部地址：播放器访问 Curio 的地址，例如 `http://192.168.31.251:8080`。
- 同步间隔：开启定时增量同步时使用。

推荐使用 Cookies 方式同步目录树，API 请求更少，也更适合大目录。Open Token 可以保留为直链和接口兜底。

### Emby

位置：`设置 -> Emby`

- Emby 原始地址：真实 Emby 地址，例如 `http://192.168.31.251:8096`。
- Emby 对外地址：播放器访问的 Curio 反代地址，例如 `http://192.168.31.251:18097`。
- 反代端口：容器内默认 `8097`，compose 默认映射为宿主机 `18097`。
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
2. 优先使用 Cookies 下载 115 导出的目录树。
3. 如果没有可用 Cookies，则使用 Open API 递归扫描目录。
4. 过滤媒体文件并生成 STRM。
5. 将 STRM 记录写入数据库。
6. 与上一次快照对比，新增、更新或删除本地 STRM。

点击 `同步操作记录` 后：

1. Curio 读取 115 操作记录事件流。
2. 根据新增、删除、移动、重命名事件更新节点表。
3. 只处理配置 CID 范围内的数据。
4. 根据变化增量更新 STRM。

开启定时同步后，Curio 会按设置的间隔自动执行增量同步。增量失败时可以再执行一次完整 STRM 同步修正快照。

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
- Open Token 模式递归扫描大目录。

建议：

- 优先配置 Cookies，使用目录树导出。
- 降低同步频率。
- 不要连续点击完整同步。
- 等待一段时间后重试。
- 大目录优先使用操作记录增量同步。

### 只有 Open Token 时为什么同步慢

Open Token 通常需要递归分页读取目录，目录越大请求越多。Cookies 可以使用 115 的目录树导出，通常更稳也更快。

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
