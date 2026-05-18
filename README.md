# Curio

Curio 是一个面向家庭媒体库的整理、识别、STRM 同步和 Emby 302 播放辅助工具。它可以整理本地或 CloudDrive2 文件，接入 TMDB 和 OpenAI 兼容接口识别文件名，按分类规则和命名模板归档媒体，并为 115 + Emby 播放链路提供低负载的 STRM 与 302 直链方案。

## 项目定位

Curio 适合已经有 NAS、Emby、115、CloudDrive2 或本地媒体目录的用户，用来解决这些问题：

- 文件名混乱，电影、剧集、合集难以自动整理。
- STRM 同步慢、不稳定，容易误删或生成错误目录层级。
- Emby 读取 STRM 时缺少真实时长、音轨、字幕和大小。
- 多个播放器通过 Emby 播放 115 STRM 时，起播慢、播放记录容易误判。
- 大目录日志和列表一次性加载过多，在低带宽环境下卡顿。

## 核心功能

### 媒体整理

- 扫描本地目录和 CloudDrive2 云端目录。
- 识别电影、剧集、合集、季、集、版本、分辨率、音视频编码和字幕。
- 支持按 TMDB 元数据和 YAML 分类策略生成二级目录。
- 支持电影、剧集、完整合集、缺失合集四类命名模板。
- 支持同目录字幕识别，简体字幕输出 `.chs`，繁体字幕输出 `.cht`。
- 支持失败记录、完成记录、批量重新归档和空目录清理。

### 文件名识别

- 内置 Go 文件名解析器，参考 GuessIt、Anitomy/Anitopy 的解析思路。
- 支持动画、剧集、电影、多版本、合集、特别版、分段 CD、ISO/BDMV 等常见命名。
- 可选接入 OpenAI 兼容接口做 AI 初分析。
- AI 只负责结构化文件名信息，TMDB 搜索和最终匹配仍由 Curio 完成。
- 真实媒体技术字段以 `ffprobe` 探测结果为准，不直接信任文件名或 AI 返回值。

### TMDB 与合集

- 支持 TMDB 电影、剧集、合集识别。
- 语言优先级为 `zh-CN`、`zh-SG`，最后回退英文。
- 剧集页按剧集聚合，显示季、集、缺失集和未播出集。
- 合集页按 TMDB 合集聚合，显示已有、缺失和未上映电影。
- 内置豆瓣电影 Top250 固定合集，可定时刷新并统计本地已有条目。

### 115 STRM

- 115 媒体库配置收敛为单个 CID。
- STRM 同步以 115 导出目录树为唯一事实源。
- 对比目录树、数据库记录和本地 `.strm` 文件后做增删改查。
- 支持定时同步、缺失 STRM 恢复、孤儿 STRM 清理。
- 自动剥离所选 CID 自身的顶层目录，避免生成 `/strm/media/...` 这类错误层级。
- 内置前缀漂移保护，避免目录层级识别错误导致批量误删。

### 115 302 播放

- STRM 指向 Curio 播放入口。
- 播放时 Curio 校验签名，并继承播放器 User-Agent 获取 115 直链。
- Curio 返回 302，媒体流量由播放器直接连接 115，不经过 Curio 本机。
- 直链结果带缓存和 singleflight 合并，降低多播放器同时起播时的重复请求。

### Emby 反代

- 提供独立 Emby 反代端口，兼容 Hills、VidHub、Yamby、爆米花等播放器。
- 拦截并改写 `PlaybackInfo`、`Items`、`stream`、`download` 请求。
- 将 Emby STRM 条目改写为原生 `/Videos/{id}/stream` 路径，再由 Curio 返回 115 302。
- 补充真实 `RunTimeTicks`、`MediaStreams`、`Size` 和容器信息。
- 媒体流探测改为后台异步执行，首次未缓存条目不会被 `ffprobe` 阻塞起播。
- 记录并纠偏 `/Sessions/Playing`、`Progress`、`Stopped`，避免点开即退被错误标记已观看。

### 日志与运维

- 统一日志页覆盖 AI 识别、播放诊断、STRM 同步、整理操作、扫描批次。
- 日志按页加载，AI 请求和响应 JSON 展开单条时再按需获取。
- 播放诊断记录播放器 UA、直链来源、302 耗时、预热、媒体探测和播放状态纠偏。
- 媒体、剧集、合集、日志列表均支持分页，适合低带宽或远程访问。

## 技术栈

### 后端

- Go
- `net/http` + `go-chi/chi`
- PostgreSQL + `pgx`
- Redis
- `ffmpeg` / `ffprobe`
- CloudDrive2 gRPC 客户端
- 115 Web / Open API 兼容播放链路
- TMDB API
- OpenAI Chat Completions 兼容接口

### 前端

- React
- TypeScript
- Vite
- lucide-react
- framer-motion
- 原生 CSS 变量和响应式布局

### 部署

- Docker 多阶段构建
- Alpine 运行时镜像
- Docker Compose
- PostgreSQL 17
- Redis 7
- 支持 Linux、Windows Docker Desktop、极空间、绿联、飞牛等常见 NAS 环境

## 架构简图

```text
播放器 / Emby 客户端
        |
        | Emby API / STRM / stream
        v
Curio Web + Emby 反代
        |
        | 302
        v
115 CDN 直链
```

```text
本地目录 / CloudDrive2 / 115 目录树
        |
        v
Curio 扫描与识别
        |
        +-- TMDB 元数据
        +-- AI 文件名初分析
        +-- ffprobe 真实媒体参数
        v
归档目录 / STRM 输出 / PostgreSQL 记录
```

## 文档

- [部署指南](docs/DEPLOYMENT.md)
- [Docker Compose](docker-compose.yml)
- [环境变量示例](.env.example)

## 快速开始

推荐使用 Docker Compose 部署。完整步骤、NAS 注意事项、升级方式和常见问题请看：

[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)

最小启动流程：

```bash
git clone https://github.com/Mon3tr-v/Curio.git
cd Curio
docker compose up -d
```

启动后访问：

```text
http://localhost:8080
```

默认端口：

- Web：`8080`
- Emby 反代：`18097` 映射到容器内 `8097`

## 配置入口

大多数运行配置都在前端设置页维护：

- `设置 -> 基础`：目录、TMDB、代理、AI 文件名识别。
- `设置 -> 云端`：CloudDrive2 地址、账号、Token、云端目录。
- `设置 -> 115`：Cookies、Open Token、媒体库 CID、STRM 输出目录、定时同步。
- `设置 -> Emby`：Emby 原始地址、反代端口、API Key。
- `分类`：电影和剧集 YAML 分类规则。
- `命名`：电影、剧集、合集命名模板。

## 安全建议

- 不要公开 TMDB Key、115 Cookies、Open Token、Emby API Key。
- 必须修改 `CURIO_PLAY_SECRET`，它用于签名 115 播放链接。
- 公网或 FRP 暴露时建议设置 `CURIO_ADMIN_TOKEN`。
- 数据库、`/data/Curio`、`/config` 建议定期备份。

## 最近更新

### 1.0.3

- 新增豆瓣电影 Top250 固定合集。
- 115 媒体库配置收窄为单个 CID。
- STRM 同步收敛为 115 导出目录树唯一事实源。
- 新增 OpenAI 兼容 AI 文件名识别配置和 AI 识别日志。
- 文件名解析器增强电影、剧集、动画、版本、分集和技术字段识别。
- Emby 反代补充真实媒体信息，并新增 STRM 播放状态纠偏。
