# haijiao-web

监控 haijiao 作者（`/homepage/<uid>`）的新视频，Web 界面管理。**默认不自动下载**：检查到的新视频进入「发现」页，按发布时间筛选后手动选择下载；也可在设置中开启自动下载（可限定发布时间下限）。

技术栈：**Go 后端**（单二进制，前端 embed 内嵌）+ **React 18 + Vite + TS** 前端 + **Docker**。

## 功能

### 监控与下载
- **多作者监控**：左侧「作者」页统一管理——添加（只填 UID，名字自动从站点获取）、启用/停用开关、删除（不影响已下载视频）；全站统一显示作者真实昵称（任务队列、视频库、下载记录、发现页、日志）
- **发现页只展示真实带视频的帖子**：站点列表的 `hasVideo` 标记不可靠（部分帖子视频附件为空），登记前会拉取帖子详情核验，无视频的直接剔除；历史遗留的未核验记录在下次检查时自动复核清理
- **视频命名与归档**：`videos/<作者名>/<视频标题-作者名>.mp4`（重名自动追加帖子 ID）；启动时自动把旧版本下载在根目录的视频整理进作者文件夹
- 任务队列：解析中 / 下载中（实时段进度）/ 完成 / 失败
- **视频库（独立页面）**：按发布时间排序，卡片显示视频真实首帧缩略图（本地文件直读，不依赖站点在线），点击在线播放（支持拖进度条）；支持**标题搜索**与**作者多选筛选**（悬浮筛选窗），卡片作者与日期分两行显示，头部徽标随筛选联动
- **自动下载详细设置**：开启后可限定「仅下载该日期后发布的视频」（日期选择 + 今天/昨天/近7天/近30天快捷选项）；早于下限的帖子仍登记到「发现」页可手动下载，发现页手动下载不受此限制
- **发现页筛选**：发布时间筛选（预设 + 自定义时间点）+ 作者多选悬浮筛选窗（搜索/全选/清空/已选标签），多选为并集；头部徽标显示 筛选数/总数

### 记录与维护
- **下载记录**：每次成功/失败/跳过流水，作者显示真实名字（非 UID）；支持**清空**、**按状态清理**、**单条删除**——清理成功记录时会同步清除对应的下载去重表（这些帖子之后可重新发现、重新下载），已下载的视频文件不受影响
- 实时日志：SSE 推送

### 安全与运维
- **鉴权**：首次部署打开网页会强制引导设置访问密码（向导）；之后所有访问需登录，**会话持久化到 `sessions.json`**（服务重启后登录态保留，页面刷新不跳登录），30 天有效；修改密码会使全部旧会话失效；可在设置页修改或停用鉴权
- **乱码防护**：按非 UTF-8 编码（如 Windows curl 直接传中文）提交的作者昵称/配置字段会被拒绝，避免有损解码损坏 `config.json` 里的中文字段
- **ffmpeg 自动管理**：启动时自动检测（系统 PATH 与数据目录 `bin/`），缺失时按平台/架构自动下载安装（Windows amd64/arm64、Linux amd64/arm64/arm、macOS Intel）；设置页可查看状态并手动触发
- 全部配置在网页端设置，持久化 `config.json`
- 解析逻辑与浏览器版 `source/index.html` 一致：三重 base64 解密 → 帖子附件 preview m3u8 → 段名推导完整 m3u8 → companion jpg XOR 还原 AES key → 并发下载解密 → ffmpeg 封装 MP4（`+faststart`，缩略图与拖动秒开）

## 目录

```
haijiao-web/
├─ backend/
│  ├─ cmd/server/main.go        # 入口（环境变量配置；启动时自动检测/安装 ffmpeg）
│  ├─ internal/site/            # 站点 API + 三重 base64 解密 + m3u8 解析
│  ├─ internal/downloader/      # 并发段下载 + AES 解密 + ffmpeg 封装
│  ├─ internal/monitor/         # 多作者轮询监控 + 视频核验 + 任务队列 + 状态持久化 + 旧视频迁移
│  ├─ internal/config/          # 配置存储（config.json，网页端可改，含访问密码）
│  ├─ internal/history/         # 下载记录（downloads.json）
│  ├─ internal/ffmpeg/          # ffmpeg 检测 / 按平台架构下载安装
│  ├─ internal/api/             # REST + SSE + 鉴权（会话持久化 sessions.json）+ 静态文件/视频流
│  └─ web/                      # embed 前端构建产物（vite 输出到 web/dist）
├─ frontend/                    # React + Vite + TS
├─ Dockerfile                   # 多阶段：node → go → alpine+ffmpeg（~50MB）
└─ docker-compose.yml
```

数据文件（在数据目录下）：`config.json`（配置）、`state.json`（下载去重 + 发现列表）、`downloads.json`（下载流水）、`sessions.json`（登录会话）、`videos/<作者名>/*.mp4`（视频）、`bin/`（自动安装的 ffmpeg）。

## 本地运行

```bash
# 构建前端（输出到 backend/web/dist）
cd frontend && npm install && npm run build

# 构建并启动后端（二进制已内嵌前端）
cd .. && go build -o server.exe ./backend/cmd/server
HJ_ADDR=":8080" ./server.exe
```

打开 http://127.0.0.1:8080 ，首次使用会引导设置访问密码。

前端开发模式（热更新，代理 /api 到 :8080）：

```bash
cd frontend && npm run dev
```

## Docker

```bash
docker compose up -d --build
```

打开 http://127.0.0.1:8080 。视频与状态持久化在宿主机 `./data`。

**自定义端口**：把 `.env.example` 复制为 `.env`，修改 `HJ_PORT`（例如 `HJ_PORT=9090`）后 `docker compose up -d`，即通过 http://127.0.0.1:9090 访问；也可以临时指定 `HJ_PORT=9090 docker compose up -d --build`。容器内部固定监听 8080，仅需调整对外端口时无需改动容器配置。

## 环境变量（仅启动种子，运行时以网页端「设置」为准）

| 变量 | 默认 | 说明 |
|---|---|---|
| `HJ_ADDR` | `:8080` | 监听地址 |
| `HJ_DATA_DIR` | `.` | 数据目录：config.json / state.json / downloads.json / sessions.json / videos/ / bin/ |
| `HJ_INTERVAL` | `600` | 首次启动默认轮询间隔秒 |
| `HJ_API_BASE` | 站点默认 | 首次启动默认站点基址 |
| `HJ_PASSWORD` | 空 | 首次启动种子访问密码（配置里已有密码时不覆盖；不设置则首次打开网页走设置向导） |

网页端设置（持久化 config.json）：作者管理（添加/启停/删除）、站点基址、轮询间隔、列表类型、段下载并发、自动下载（含发布时间下限）、访问密码。

## 鉴权说明

- **首次部署**：未设置过密码且未停用鉴权时，打开网页强制进入「设置访问密码」向导，完成前所有 API 拦截（403）
- **登录**：设置密码后所有 `/api/*` 需要会话 Cookie（30 天）；**会话持久化在 `sessions.json`**——服务重启后已登录的浏览器保持登录态，页面刷新直接进主界面；「退出登录」按钮在侧栏底部
- **停用**：设置页勾选「停用鉴权」即恢复免登录访问，之后再想启用，重新设置一个新密码即可
- 密码不回传前端（接口只返回是否已设置）

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/auth/state` | 鉴权状态（setup=首次待设置密码 / login=需登录 / **authed=会话有效免登录** / open=免鉴权） |
| POST | `/api/auth/setup` | 首次部署设置访问密码（成功后自动登录） |
| POST | `/api/auth/login` / `/api/auth/logout` | 登录 / 注销 |
| GET | `/api/status` | 状态快照（统计 + 作者统计 + 任务队列，任务含作者昵称） |
| GET | `/api/videos` | 已下载视频列表（按发布时间降序，含作者昵称） |
| GET | `/api/videos/file/<作者名>/<文件名>` | 视频文件流（支持 Range；仅限 videos/ 内） |
| GET | `/api/downloads` | 下载记录（含作者名字） |
| POST | `/api/downloads/clear` | 清理下载记录 `{status: all\|done\|failed\|skipped}`，同步清除对应去重 |
| POST | `/api/downloads/delete` | 删除单条记录 `{topicId, status, at}` |
| GET | `/api/discovered` | 发现待下载列表（已核验带视频） |
| POST | `/api/download` | 下载选中的发现记录 `{topicIds: []}` |
| POST | `/api/discovered/dismiss` | 忽略选中的发现记录 |
| GET | `/api/config` | 读取配置（密码不回传，仅 `hasPassword`） |
| POST | `/api/config` | 更新配置（`password` 非空=设新密码；`clearPassword: true`=停用鉴权；`autoDownloadAfter`=自动下载发布时间下限；保存后触发检查；非 UTF-8 乱码输入会被拒绝） |
| POST | `/api/authors/add` / `enable` / `remove` | 添加 / 启停 / 删除作者 |
| GET | `/api/ffmpeg` | ffmpeg 状态（state / path / 平台架构） |
| POST | `/api/ffmpeg/install` | 手动触发 ffmpeg 检测/下载安装（后台执行） |
| GET | `/api/events?stream=1` | SSE 实时日志；不带 stream 返回最近事件 JSON |
| POST | `/api/check` | 手动触发检查 |

以上 `/api/*`（鉴权三接口除外）在启用鉴权时均需登录会话。
