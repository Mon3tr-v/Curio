# Curio 部署指南

本文是 Curio 的独立部署说明页

## 部署前准备

你需要准备：

- Docker
- Docker Compose
- 可写的数据目录
- 可选：TMDB API Key
- 可选：115 Cookies 或 Open Token
- 可选：CloudDrive2 服务
- 可选：Emby 服务和 Emby API Key

推荐目录结构：

```text
Curio/
├── docker-compose.yml
├── data/
│   ├── postgres/
│   ├── redis/
│   └── Curio/
└── config/
```

## 快速部署

1. 下载项目：

```bash
git clone https://github.com/Mon3tr-v/Curio.git
cd Curio
```

2. 修改 `docker-compose.yml` 里的播放签名密钥：

```yaml
CURIO_PLAY_SECRET: "change-me"
```

请改成一段足够长的随机字符串。它用于签名 115 播放链接，生产环境不要使用默认值。

3. 启动：

```bash
docker compose up -d
```

4. 打开 Curio：

```text
http://localhost:8080
```

默认端口：

- Web：`8080`
- Emby 反代：`18097` 映射到容器内 `8097`

## Docker Compose 示例

仓库根目录已经提供 `docker-compose.yml`。如果你需要自己创建，可以参考：

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
    image: mon3trd/curio:1.0.4
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

说明：

- TMDB、115、CloudDrive2、Emby 等配置推荐进入前端设置页填写。
- 如果使用 NAS 面板部署，确保 `./data/Curio`、`./config`、`./data/postgres` 有读写权限。
- 如果 Curio 需要访问宿主机代理，前端设置页可以填写 `http://host.docker.internal:7890`。部分 NAS 不支持时，改填宿主机实际 IP。

## 使用导出的镜像包

如果你拿到的是 `.tar.gz` 镜像包，例如：

```text
curio-1.0.4-arm64-zspace.tar.gz
```

先导入镜像：

```bash
docker load -i curio-1.0.4-arm64-zspace.tar.gz
```

确认镜像标签：

```bash
docker images | grep curio
```

然后使用上面的 `docker-compose.yml` 启动即可。

## 首次配置

### 基础设置

位置：`设置 -> 基础`

- 入库目录：本地扫描源目录。
- 整理目录：识别成功后的归档目录。
- 失败目录：识别或移动失败后的归档目录。
- 缺失合集目录：合集未完整时的归档目录。
- TMDB API Key：用于识别电影、剧集和合集。
- 网络代理：例如 `http://192.168.1.10:7890`。
- AI 文件名识别：本地解析失败或低置信度时使用 AI 兜底。
- 强制使用 AI：扫描完目录后先批量交给 AI 初分析，再由 Curio 搜索 TMDB。
- AI 接口地址、API Key、模型、提示词：兼容 OpenAI Chat Completions 写法。

目录建议：

- 入库、整理、失败、缺失合集目录不要互相嵌套。
- Curio 会自动创建目录并检查读写权限。
- 非媒体文件不会进入整理状态机。

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
- 媒体库 CID：只填写一个 115 目录 CID。
- STRM 输出目录：生成 `.strm` 文件的位置。
- STRM 生成地址：写入 STRM 文件的 Curio 地址，例如 `http://192.168.1.10:8080`。
- 同步间隔：开启定时同步时使用。

推荐：

- 使用 Cookies 同步目录树，API 请求更少，也更适合大目录。
- Open Token 可以保留为直链和接口兜底。
- CID 指向 `media` 时，Curio 会剥离该顶层目录，不会额外生成 `/strm/media/...`。

### Emby

位置：`设置 -> Emby`

- Emby 原始地址：真实 Emby 地址，例如 `http://192.168.1.10:8096` 或 `http://emby:8096`。
- 反代端口：容器内默认 `8097`，compose 默认映射为宿主机 `18097`。
- API Key：用于同步后刷新 Emby 媒体库和播放记录纠偏。

播放器里填写 Curio 的 Emby 反代地址：

```text
http://你的NAS地址:18097
```

Emby 挂载建议：

```yaml
services:
  emby:
    volumes:
      - ./data/Curio:/data/Curio
```

如果 Curio 的 STRM 输出目录是 `/data/Curio/strm`，Emby 媒体库也应指向同一个容器路径 `/data/Curio/strm`。

## 整理目录层级

Curio 固定使用一级媒体类型目录，再使用分类策略生成二级目录：

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

这些字段优先来自 `ffprobe`，不再依赖文件名猜测。模板没有使用技术字段时，Curio 会跳过不必要的 `ffprobe`。

## 115 STRM 同步逻辑

点击 `同步 STRM` 后：

1. Curio 读取配置的 115 媒体库 CID。
2. 使用 Cookies 创建并下载 115 导出的目录树。
3. 过滤目录树里的媒体文件，计算目标 STRM 列表。
4. 对比目录树、数据库记录和本地 `.strm` 文件。
5. 只创建缺失 STRM、更新内容变化的 STRM、恢复本地缺失的 STRM。
6. 开启“删除缺失 STRM”时，删除目录树中已不存在的本地 STRM。

开启定时同步后，Curio 会按设置的间隔自动执行同一套目录树差异同步。

## 115 302 播放逻辑

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

## Emby 反代播放逻辑

播放器使用 Curio 的 Emby 反代端口连接媒体库：

```text
http://你的NAS地址:18097
```

反代会把 Emby 的媒体源改写为原生 `/Videos/{id}/stream` 路径，并在播放器真正起播时返回 115 直链。Curio 会保存 Emby Item 和 STRM 链接的映射，让播放器继续走 Emby 播放记录，同时避免媒体流量经过 Curio。

为了降低首播等待，Curio 会在播放器请求详情页或 PlaybackInfo 时预热当前集直链，并额外预热同一 STRM 目录下排序后的下一集 1 条链接。预热受去重和并发限制保护，不会批量扫整季。

STRM 条目在 Emby 数据库里经常只有 `strm` 容器和空时长。Curio 会在反代里补充真实媒体时长、音轨、字幕和大小，并监听 `/Sessions/Playing`、`/Sessions/Playing/Progress`、`/Sessions/Playing/Stopped`。如果播放器退出时上报了负数、0 或过短进度，Curio 会自动取消错误的已观看状态；如果有有效进度但未达到已观看阈值，会写回续播点。

## 环境变量

大多数配置都建议在前端设置页维护。compose 里通常只需要 `TZ` 和 `CURIO_PLAY_SECRET`。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `TZ` | 无 | 容器时区，推荐 `Asia/Shanghai`。 |
| `SERVER_ADDR` | `:8080` | Curio 后端监听地址。 |
| `DATABASE_URL` | `postgres://curio:curio@db:5432/curio?sslmode=disable` | PostgreSQL 连接串。 |
| `REDIS_ADDR` | `redis:6379` | Redis 地址。 |
| `REDIS_PASSWORD` | 空 | Redis 密码。 |
| `CURIO_ADMIN_TOKEN` | 空 | 后台访问 Token。配置后前端需要登录。 |
| `CURIO_PLAY_SECRET` | `curio-change-me` | 115 播放链接签名密钥，强烈建议修改。 |
| `CURIO_DATA_ROOT` | `/data/Curio` | Curio 数据根目录。 |
| `FRONTEND_DIR` | `/app/public` | 前端静态文件目录。 |
| `FRONTEND_ORIGIN` | `*` | CORS 来源。 |
| `TMDB_API_KEY` | 空 | 初始 TMDB API Key，也可以在前端设置页配置。 |
| `NETWORK_PROXY` | 空 | 初始网络代理，也可以在前端设置页配置。 |
| `AI_BASE_URL` | `https://api.openai.com/v1` | 初始 OpenAI 兼容接口地址。 |
| `AI_API_KEY` | 空 | 初始 AI API Key。 |
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

## 升级

1. 备份数据：

```bash
docker compose exec db pg_dump -U curio curio > curio-backup.sql
```

2. 更新镜像标签或导入新的镜像包。

3. 重启服务：

```bash
docker compose pull curio
docker compose up -d
```

使用本地镜像包时，先 `docker load -i`，再 `docker compose up -d`。

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

### Cookies 是否永久有效

不是。扫码获取的 Cookies 通常较稳定，但仍可能因为 115 服务端策略、IP、设备管理或账号安全策略失效。失效后重新扫码即可。

## 安全建议

- 不要公开 TMDB Key、115 Cookies、Open Token、Emby API Key。
- `CURIO_PLAY_SECRET` 必须修改为随机长字符串。
- 如果公网暴露 Curio，请配置 `CURIO_ADMIN_TOKEN`。
- 定期备份 PostgreSQL、`/data/Curio` 和 `/config`。
