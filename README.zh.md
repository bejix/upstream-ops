# UpstreamOps

[English](README.md) | [简体中文](README.zh.md)

> 本项目基于 [worryzyy/upstream-hub](https://github.com/worryzyy/upstream-hub) 二次开发，感谢原作者 [@worryzyy](https://github.com/worryzyy) 的开源工作。

> UpstreamOps 是一个面向 NewAPI / Sub2API 上游站点的集中监控与运维面板，用来统一管理上游账号、查看余额与消费、同步模型倍率、追踪倍率变化、维护上游 API Key、发起充值/兑换，并通过多种通知渠道推送余额告警、倍率变更、登录异常、监控异常和上游公告。
同时内置 OpenAI / Claude / Responses 兼容的请求转发网关：可创建网关密钥、绑定同步渠道或直连上游、按倍率与权重调度、协议互转、故障自动切换，并记录每次请求的用量与费用参考。


## ❤️赞助商
<details open>
<summary>点击折叠</summary>

<table>
<tr>
<td width="180"><a href="https://cmzi.com/aff/CHTVTQWE"><img src="https://zhenxiansheng-1251032746.file.myqcloud.com/Markdown/2020/12/29/zi-yuan-32.png" alt="cmzi.com" width="150"></a></td>
<td>感谢 触摸云 赞助了本项目！触摸云 是一家专注海外云计算服务的品牌，提供香港云服务器、美国高防云服务器、物理服务器、防御与加速 CDN、自研 CDN 系统等产品。触摸云为本软件用户提供了特别优惠，使用
<a href="https://cmzi.com/aff/CHTVTQWE">此链接</a></td>
</tr>


</table>

</details>

## 为什么使用？

当你同时维护多个 NewAPI / Sub2API 上游时，余额、消费、倍率、公告、API Key、订阅、充值入口和下游同步配置通常分散在不同后台。人工逐个登录检查不仅重复，而且很容易漏掉余额不足、倍率调整、登录失效、订阅即将到期或上游公告。

UpstreamOps 主要解决这些痛点：

- 集中看状态：把多个上游的余额、消费、倍率、公告、订阅和异常状态放到一个面板里。
- 减少人工巡检：定时同步余额、消费、倍率和订阅用量，不需要反复打开不同后台。
- 及时发现风险：余额低、倍率变化、登录失败、监控失败、订阅余量不足和订阅到期都可以推送通知。
- 保留变化记录：倍率变化、余额快照、通知日志和上游公告都会落库，方便回看问题发生时间。
- 简化日常运维：常用的 API Key 管理、充值、兑换、订阅购买和续订可以在同一个入口处理。
- 适配复杂网络环境：支持全局代理，并可按上游渠道、通知渠道、验证码服务分别决定是否走代理。

它适合需要长期维护多个上游账号、关注余额和倍率变化、希望把上游运维从“人工看后台”变成“集中监控和主动告警”的场景。

## 预览

![UpstreamOps 预览 1](docs/images/demo1.png)

![UpstreamOps 预览 2](docs/images/demo2.png)

![UpstreamOps 预览 3](docs/images/demo3.png)

![UpstreamOps 预览 4](docs/images/demo4.png)

![UpstreamOps 预览 5](docs/images/demo5.png)

![UpstreamOps 预览 6](docs/images/demo6.png)

![UpstreamOps 预览 7](docs/images/demo7.png)

## 功能概览

### 请求转发网关

- 提供兼容客户端入口（Bearer / `x-api-key`）：
  - `GET /v1`（无嵌入前端时也可为 `GET /`）返回 endpoints 说明
  - `GET /v1/models`、`GET /v1/usage`
  - `POST /v1/chat/completions`、`POST /v1/completions`
  - `POST /v1/responses`（OpenAI Responses，含 stream / 子路径）
  - `POST /v1/messages`、`POST /v1/messages/count_tokens`（Anthropic）
  - 透传类：`/v1/embeddings`、`/v1/images/*`、`/v1/videos/*` 等（改 model 后按上游 path 转发）
  - 兼容路径：`/chat/completions`、`/responses`、Codex `/backend-api/codex/*`、Gemini `/v1beta/*` 等
- **网关组**为配置单元：组内多把密钥共享路由、映射、模型列表与重试/顺延策略。
- **路由来源**两种：
  - **同步渠道**：绑定已监控的 NewAPI / Sub2API 渠道 + 源分组；可「确保上游密钥」自动创建/复用专用 API Key。
  - **直连渠道（Provider）**：在网关内维护 base URL、API Key、默认计费倍率、鉴权样式与代理开关，无需先做监控渠道。
- 路由调度：源分组倍率换算（raw / ×100 / ÷100 / custom）+ 权重；组级升序/降序；可开启「倍率扫描后自动重排」。
- 可视化模型映射（A→B、`*` 通配）与模型列表（从上游拉取去重 / 自定义 / auto·manual·hybrid）；支持按路由预览、同步与探测。
- **协议互转**（含非流式与 SSE 增量）：
  - OpenAI Chat ↔ Anthropic Messages
  - OpenAI Chat ↔ OpenAI Responses
  - Anthropic ↔ OpenAI Responses
  - 路由 `upstream_protocol`：`auto` / `openai`（Chat）/ `openai_responses` / `anthropic`
- 故障转移：网络错误、429、5xx 临时暂停并顺延；组可开「4xx 顺延」；支持组级重试次数、最大切换次数、冷却秒数。
- **首字超时**（可选）：在仍有可顺延路由时，对首字节等待限时，加速切换到下一条路由。
- **系统提示词注入**（组级）：按规则顺序注入，可作用于全部 Key 或指定 Key；覆盖 Chat `system`、Responses `instructions`、Anthropic `system`（HTTP 与 Responses WebSocket）。
- **`service_tier` 策略**（组级）：匹配 `all` / `default` / `priority` / `flex`（并规范化 `fast` → `priority`）；动作支持透传、过滤、强制 `default` / `priority`、或拦截。
- **Responses WebSocket**：`/v1/responses` 支持 Upgrade；会话中不可切换协议，仅 Responses 能力路由可接入。
- **Responses 工具桥接**：Responses→Chat/Anthropic 降级时保留 custom tools、namespaced tools 与 `tool_search` 往返语义。
- **图片 / 视频计费输入**：从 image/video 接口与 Responses 图片生成工具用量解析请求数、分辨率档位与时长，用于费用估算。
- User-Agent：路由级 `passthrough` / `group` / `custom`；管理侧拉模型、探测会回落默认 UA。
- 使用记录对齐 sub2api 字段：endpoint、协议、tokens（含缓存读写分桶）、费用、延迟、首字延迟、成功/失败与上游错误详情；提供列表、stats、模型筛选项与清理。
- 计费：内置单价表（可覆盖）+ `actual_cost = base_cost × 账号计费倍率`（共用倍率换算规则）。
- 运行时参数（系统设置 `gateway` 段可热更新）：转发超时、模型列表缓存 TTL、默认暂停秒数、批量运维并发、用量错误落库截断等。
- 管理入口：Dock「请求网关」页面（`/gateway`）；管理 API 前缀 `/api/gateway/*`（需后台鉴权）。

### 上游渠道管理

- 支持 NewAPI / Sub2API 两类上游。
- 支持账号密码模式和 Token/Cookie 模式。
- 支持启用或关闭单个渠道监控。
- 支持自定义渠道排序，数值越大越靠前展示并优先参与监控。
- 支持配置余额告警阈值。
- 支持测试登录、手动同步余额、手动同步倍率。
- 支持账号密码模式配置附加登录表单参数，适配需要额外字段的 NewAPI / Sub2API 魔改版登录接口。
- 支持 Cloudflare Turnstile 打码配置，适用于开启 Turnstile 的上游登录场景。
- 支持在渠道卡片中打开上游站点地址。
- 支持在渠道卡片中清空已保存的登录信息。
- 支持按名称、账号、站点地址、备注或标签搜索渠道，按状态（健康 / 低余额 / 登录失败 / 尚未采集 / 已暂停）或标签筛选，并按名称、账号（中文按拼音）、健康状态或余额排序。
- 支持为渠道添加自定义标签和备注，标签可直接用于筛选。
- 渠道卡片显示登录账号；渠道列表每页显示数量会保存在浏览器中，刷新后保持不变。
- 支持「仅显示已创建密钥的分组」开关：开启后渠道卡片、分组对话框、倍率面板和倍率变动通知只关注已创建密钥的分组；网关转发和通知设置仍可看到全部分组，本地倍率快照也始终保留全量。
- 删除上游渠道时会自动清理相关快照、倍率、公告、通知冷却和通知日志。

### 余额与消费监控

- 首页展示总余额、今日消费、累计消费、最低余额渠道、异常渠道数量。
- 今日消费按北京时间跨天：渠道当天还没有采集到消费（例如已暂停监控）时，今日消费显示为 0。
- 支持周期性采集余额和消费。
- 支持余额历史趋势图。
- 支持余额低于阈值时通知推送。
- 支持余额告警冷却，避免同一渠道持续低余额时刷屏。
- 支持按充值倍率换算余额、消费和兑换结果，可跟随上游倍率，也可手动选择除以或乘以倍率。

### 倍率监控

- 支持同步上游分组/模型倍率。
- 支持记录倍率变化历史。
- 支持倍率变化历史分页查询和按渠道过滤。
- 支持倍率变化通知。
- 支持同一次扫描内多条倍率变化合并推送。
- 支持同一次扫描内新增分组和删除分组合并为一条“分组变动通知”，通知标题格式为 `[分组变动通知] 渠道名 · 新增 X / 删除 Y`，正文会分别列出新增分组的倍率和删除分组的原倍率，避免新增、删除分开推送导致同一轮扫描刷屏。
- 支持按变化百分比过滤小幅变动。
- 通知订阅可按上游渠道和倍率分组过滤。
- 支持在监控页查看全部渠道分组，并按渠道或倍率搜索排序。

### 订阅管理与用量监控

针对 Sub2API 类型上游渠道，提供完整的订阅生命周期管理与用量监控能力：

- 支持查询上游订阅计划与支付方式。
- 支持购买或续订订阅，根据上游返回的支付方式自动选择二维码、跳转链接或表单提交。
- 支持查询订阅用量数据，包括日/周/月维度的使用额度上限、已用量、剩余量和剩余百分比。
- 支持按订阅维度展示到期时间、剩余天数、订阅状态（生效中/已过期/已撤销/已停用）。
- 支持订阅用量低余量告警通知：
  - 日剩余百分比低于阈值时触发 `subscription_daily_remaining_low` 事件。
  - 周剩余百分比低于阈值时触发 `subscription_weekly_remaining_low` 事件。
  - 月剩余百分比低于阈值时触发 `subscription_monthly_remaining_low` 事件。
  - 订阅即将到期时触发 `subscription_expiring` 事件。
- 支持订阅告警冷却，避免同一渠道订阅用量持续偏低时刷屏。
- 订阅功能仅对 Sub2API 渠道生效，需在渠道配置中启用 `subscription_enabled` 开关。
- 前端监控页面提供订阅用量摘要卡片和详细弹窗，支持按分组查看各订阅的用量进度条和剩余金额。

### 验证码余额管理

- 支持查询打码平台（CapSolver / 2Captcha / AntiCaptcha / YesCaptcha）的账户余额。
- 支持手动刷新单个打码平台的余额。
- 支持批量刷新全部打码平台的余额。
- 余额信息包括余额数值、余额单位和最后刷新时间，异常时会显示错误信息。
- 验证码配置列表展示各平台余额状态，方便运维人员及时充值。

### 全局代理与上游 HTTP 配置

- 支持 HTTP / HTTPS / SOCKS5 全局代理。
- 支持代理用户名和密码。
- 支持为上游渠道、通知渠道、验证码服务分别开启代理。
- 支持版本检查单独启用代理。
- 支持配置上游请求超时时间和 `User-Agent`。
- 系统设置页支持代理连通性测试。

### 上游公告同步

- 支持从 NewAPI 同步公告：
  - `/api/status` 中的 `announcements`。
  - `/api/notice` 文本公告。
- 支持从 Sub2API 同步用户可见公告：
  - `/api/v1/announcements`。
- 公告同步跟随倍率同步执行，不额外增加独立定时任务。
- 首次采集只建立公告基线，不推送历史公告。
- 后续发现新增公告会写入本地公告表，并通过现有通知渠道推送。
- 首页底部提供“上游公告”卡片。
- 支持公告分页查询与详情查看。
- 公告详情弹窗支持 Markdown 渲染。
- 删除上游渠道时会自动清理该渠道关联公告。
- 支持按保留天数自动清理过期公告。
- 公告推送结果会自然出现在”告警动态”中。
- 支持渠道级公告忽略开关 `ignore_announcements`，关闭后该渠道的上游公告将不会触发通知推送，适用于需要静默特定渠道公告的场景。

### 通知渠道

支持以下通知渠道：

- Telegram
- Webhook
- Email
- 企业微信
- 钉钉
- 飞书
- ServerChan3

通知渠道支持订阅过滤：

- 留空或 `[]`：接收全部事件。
- `mode=all`：接收指定上游的全部事件。
- `mode=groups`：倍率变化只接收指定分组；公告、余额、登录失败、监控失败等事件仍按上游渠道过滤，不受分组过滤影响。

### 上游 API Key 管理

在渠道卡片中可以进入 API Key 管理：

- 查看上游 API Key 列表。
- 按名称或 Key 搜索。
- 按状态筛选。
- 新建 API Key。
- 编辑名称、分组、状态、额度、过期时间、IP 白名单/黑名单、模型限制等字段。
- 删除 API Key。
- 获取并复制完整 Key。
- 在“分组”总览中可直接为指定分组新建 API Key，或从当前渠道选择一个现有 API Key 迁移到该分组。
- 迁移前会重新校验目标分组并显示原分组到目标分组的二次确认；已在目标分组的 Key 仅展示、不可重复选择。

不同上游支持的字段存在差异，前端会按 NewAPI / Sub2API 的接口能力展示对应表单。

### 充值与兑换

在渠道卡片中可以直接处理上游充值和兑换：

- 支持查询上游充值配置。
- 支持支付宝 / 微信支付等上游返回的支付方式。
- 支持二维码、跳转链接、表单提交等支付发起方式。
- 支持桌面端优先二维码，移动端优先跳转。
- 支持兑换码在线兑换。
- 兑换成功后会根据返回内容展示余额、并发、分组订阅等结果。
- 兑换对话框支持输入兑换码后即时兑换，结果展示兑换类型、价值、新余额、新并发、分组名称和有效期等信息。
- Sub2API 渠道额外支持订阅购买与续订，可查询订阅计划（价格、周期、配额、日/周/月额度上限），选择合适的支付方式完成订阅。
- 充值与订阅支付均支持移动端自适应：移动设备优先跳转支付链接，桌面端优先展示支付二维码。

### 系统设置

系统设置页集中管理：

- 后台登录鉴权。
- 管理员账号密码。
- Token 签名密钥。
- 余额同步 cron。
- 倍率同步 cron。
- 并发数量。
- 监控日志、余额快照、通知日志保留天数。
- 上游公告保留天数。
- 倍率变化通知合并策略。
- 倍率变化最小推送百分比。
- 余额低告警冷却时间。
- 订阅用量日/周/月剩余百分比告警阈值。
- 订阅到期告警时长阈值。
- 订阅告警冷却时间。
- 通知最大重试次数。
- 全局代理配置。
- 代理连通性测试。
- 版本检查结果通过页面通知提示。
- 上游请求超时时间和 `User-Agent`。
- 请求网关运行时参数（`gateway` 段：转发超时、模型缓存、故障暂停、批量并发、用量错误截断等）。
- Sub2API 目标上游和同步分组。
- 通知渠道。
- 验证码服务。

保存配置会写入配置文件；应用配置后，鉴权、调度、通知策略、代理、上游 HTTP 与网关运行时配置会立即生效。通知渠道和验证码服务本身是实时写库生效。

## 快速启动

### Docker Compose（SQLite）

默认使用 SQLite 单文件数据库，推荐直接用 Docker Compose：

```bash
cp .env.example .env
```

编辑 `.env`，至少设置：

```env
APP_SECRET=请替换为 32 字节以上随机字符串
```

`APP_SECRET` 用于 AES-GCM 加密敏感字段，包括上游密码、Token、Cookie、通知渠道密钥、验证码平台 API Key 等。修改后既有加密数据将无法解密，请务必妥善保存。

公网访问建议开启后台登录：

```env
AUTH_ENABLED=true
ADMIN_USERNAME=admin
ADMIN_PASSWORD=请替换为强密码
```

Docker 默认拉取 `ghcr.io/bejix/upstream-ops:${IMAGE_TAG:-latest}`，不会在本机编译镜像。配置和数据都会写入宿主机项目目录下的 `data/`。

启动：

```bash
docker compose up -d
```

默认访问地址：

```text
http://localhost:8080
```

默认数据文件在容器内：

```text
/app/data/upstream-ops.db
```

宿主机对应文件是项目根目录下的 `data/upstream-ops.db`。系统设置配置文件会持久化到 `data/config.yaml`。

### 固定镜像版本

默认镜像 Tag 来自 `.env`：

```env
IMAGE_TAG=latest
```

生产环境建议锁定具体版本，例如：

```env
IMAGE_TAG=v0.0.10
```

## MySQL 部署

如果不想使用 SQLite，可以叠加 MySQL 配置：

```bash
docker compose -f docker-compose.yml -f docker-compose.mysql.yml up -d
```

`.env` 至少设置：

```env
APP_SECRET=请替换为 32 字节以上随机字符串
MYSQL_DATABASE=upstreamops
MYSQL_USER=upstreamops
MYSQL_PASSWORD=请替换为数据库密码
MYSQL_ROOT_PASSWORD=请替换为 root 密码
MYSQL_PORT=33069
```

## 环境变量

### 基础配置

```env
HTTP_PORT=8080
IMAGE_TAG=latest
SERVER_MODE=release
LOG_LEVEL=info
```

- `HTTP_PORT`：宿主机暴露端口。
- `IMAGE_TAG`：镜像版本。
- `SERVER_MODE`：Gin 运行模式，通常为 `release`。
- `LOG_LEVEL`：日志等级。

### 数据库配置

SQLite：

```env
DATABASE_DRIVER=sqlite
DATABASE_PATH=/app/data/upstream-ops.db
```

MySQL：

```env
DATABASE_DRIVER=mysql
DATABASE_HOST=mysql
DATABASE_PORT=3306
DATABASE_USER=upstreamops
DATABASE_PASSWORD=change-me
DATABASE_NAME=upstreamops
```

### 安全与登录

```env
APP_SECRET=please-change-me-to-a-long-random-secret-32bytes-min
AUTH_ENABLED=false
ADMIN_USERNAME=admin
ADMIN_PASSWORD=
AUTH_TOKEN_SECRET=
```

- `APP_SECRET`：主密钥，必填。
- `AUTH_ENABLED`：是否启用后台登录。
- `ADMIN_USERNAME`：后台管理员账号。
- `ADMIN_PASSWORD`：后台管理员密码。
- `AUTH_TOKEN_SECRET`：登录 Token 签名密钥；留空时使用 `APP_SECRET`。

## 本地开发

### 后端

```bash
go run ./cmd/server
```

默认后端端口：

```text
8418
```

### 前端

```bash
cd frontend
pnpm install
pnpm dev
```

默认前端开发地址：

```text
http://127.0.0.1:3010
```

### 验证命令

```bash
go test ./...
```

```bash
cd frontend
pnpm lint
pnpm exec tsc --noEmit --incremental false
pnpm build
```

## 代理与上游 HTTP 配置

系统设置页可以配置全局代理和上游请求参数。代理默认关闭，协议默认 `http`；上游请求超时默认 `30` 秒，`User-Agent` 默认 `upstream-ops/0.1`。

配置文件字段：

```yaml
proxy:
  enabled: false
  versionCheckEnabled: false
  protocol: http
  host: 127.0.0.1
  port: 7890
  username: ""
  password: ""

upstream:
  timeoutSeconds: 30
  userAgent: upstream-ops/0.1

gateway:
  tempPauseSeconds: 30
  forwardTimeoutSeconds: 600
  modelsCacheTTLSeconds: 60
  maxFailoverSwitches: 8
  routeBatchConcurrency: 8
  usageErrorBodyBytes: 32768
  usageErrorMsgRunes: 500
  usageErrorHeaderValueRunes: 8192
  usageErrorHeadersJSONBytes: 65536
```

- `proxy.enabled`：是否启用全局代理。
- `proxy.versionCheckEnabled`：版本检查是否走代理。
- `proxy.protocol`：代理协议，支持 `http`、`https`、`socks5`。
- `proxy.host` / `proxy.port`：代理地址和端口。
- `proxy.username` / `proxy.password`：代理认证信息，可留空。
- `upstream.timeoutSeconds`：访问上游站点的请求超时时间。
- `upstream.userAgent`：访问上游站点时使用的 `User-Agent`（网关管理侧拉模型/探测的默认 UA 回落值）。
- `gateway.*`：请求转发网关运行时参数（转发超时、模型列表缓存、临时暂停、批量运维并发、用量错误落库截断等），可在系统设置中热更新。
- `proxy.enabled=false` 时，即使上游渠道、通知渠道或验证码服务开启 `proxy_enabled`，也不会走代理。

代理测试接口：

```text
POST /api/settings/proxy/test
```

请求体使用同一份 `proxy` 配置结构。成功时返回 `ok`、`latency_ms`、`ip`、`provider`；失败时返回 `ok=false` 和 `error`。

## 上游渠道配置

上游渠道支持单独开启 `proxy_enabled`。只有全局 `proxy.enabled=true` 且该渠道开启 `proxy_enabled` 时，上游登录、余额同步、倍率同步、公告同步、API Key 管理、充值兑换和订阅接口才会走代理。

### NewAPI

支持两种凭据方式：

#### 账号密码模式

填写上游站点地址、用户名、密码。若上游登录接口需要额外字段，可以在“附加表单参数”中填写 JSON 对象；若上游开启 Turnstile，需要先在“验证码服务”中配置打码平台，然后在渠道中启用 Turnstile。

新版 NewAPI（QuantumNous/new-api 及其分叉）登录后改用短期 `access_token`（Bearer JWT，默认 15 分钟）鉴权，并随响应下发 `new_api_refresh` 刷新令牌。UpstreamOps 会在令牌临近过期时自动调用 `/api/user/auth/refresh` 续期并轮换刷新令牌，无需频繁重新登录；老版本 NewAPI 仍走 `Set-Cookie: session=...` + `New-Api-User` 头鉴权，由程序自动识别兼容。

登录与续期请求会携带由站点地址推导的 `Origin` / `Referer`。只有上游明确返回 401（刷新令牌失效）时才回退到账号密码重新登录；403、409、429、5xx、网络错误等其它失败会保留现有会话并记录错误，避免反复登录占满上游的会话数上限。同一渠道的会话刷新与登录会串行执行。

#### Token/Cookie 模式

适合不希望程序自动登录，或上游登录存在额外验证的场景。需要提供：

```json
{
	"cookie": "session=xxx; other=yyy",
	"user_id": "123"
}
```

- `cookie`：浏览器开发者工具中复制的 Cookie。
- `user_id`：NewAPI 个人设置页中的用户 ID。

NewAPI token 模式同样支持使用系统访问令牌（`user.access_token`，即 NewAPI 个人设置页生成的「系统访问令牌」，32 位字符串）鉴权。改用 `access_token` 字段代替 `cookie`，二者互斥；`user_id` 仍必填：

```json
{
	"access_token": "your-system-access-token",
	"user_id": "123"
}
```

编辑 NewAPI Token/Cookie 渠道时，表单会回显已保存的 `user_id` 便于复用，但不会回显已保存的 Cookie 或访问令牌。

### Sub2API

支持账号密码模式和 Token 模式。

Token 模式凭据：

```json
{
	"access_token": "your-access-token",
	"refresh_token": "your-refresh-token"
}
```

`refresh_token` 可选但建议填写。填写后，Sub2API 会话和 Token 模式凭据可在 access token 失效后自动刷新；不填写时，Token 失效后需要重新粘贴凭据。

### 清空登录信息

渠道卡片的“更多”菜单支持清空登录信息：

- 账号密码模式：只清空缓存会话。
- Token 模式：清空缓存会话，并清空已保存的 token/cookie 凭据 JSON。

## 通知渠道配置

通知渠道的密钥、Webhook、SMTP 密码等敏感配置会加密保存。新增或编辑通知渠道时，按渠道类型填写对应 JSON。

通知渠道支持单独开启 `proxy_enabled`。只有全局 `proxy.enabled=true` 且该通知渠道开启 `proxy_enabled` 时，Telegram、Webhook、企业微信、钉钉、飞书、ServerChan3 等外部推送请求才会走代理。

### Telegram

```json
{
	"bot_token": "1234567890:AAEh...",
	"chat_id": "-1001234567890"
}
```

- `bot_token`：从 Telegram 的 `@BotFather` 创建机器人后获取。
- `chat_id`：接收消息的私聊、群组或频道 ID。

### Webhook

```json
{
	"url": "https://example.com/hook",
	"method": "POST",
	"headers": {
		"Authorization": "Bearer xxx"
	}
}
```

- `url` 必填。
- `method` 默认 `POST`，也可以填 `PUT` 或 `GET`。
- `headers` 可选，用于自定义请求头。

Webhook 请求体示例：

```json
{
	"event": "announcement",
	"subject": "[UpstreamOps] xxx",
	"body": "通知正文",
	"extra": {}
}
```

### Email

```json
{
	"host": "smtp.example.com",
	"port": 465,
	"use_tls": true,
	"username": "alert@example.com",
	"password": "smtp-password-or-app-password",
	"from": "alert@example.com",
	"to": ["ops@example.com"]
}
```

- `host`、`port`、`from`、`to` 必填。
- `username`、`password` 取决于 SMTP 服务商是否要求鉴权。
- 常见端口：`465` 通常配合 `use_tls=true`，`587` 通常配合 STARTTLS。

### 企业微信

```json
{
	"webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxx"
}
```

填写群机器人的完整 Webhook URL。

### 钉钉

```json
{
	"webhook_url": "https://oapi.dingtalk.com/robot/send?access_token=xxx",
	"secret": "SEC..."
}
```

- `webhook_url` 必填。
- `secret` 可选，启用机器人“加签”时填写。

### 飞书

```json
{
	"webhook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/xxxx",
	"secret": "..."
}
```

- `webhook_url` 必填。
- `secret` 可选，启用“签名校验”时填写。

### ServerChan3

```json
{
  "uid": "你的 UID",
  "sendkey": "sctp_xxx"
}
```

- `uid`：ServerChan3 用户 UID。
- `sendkey`：ServerChan3 的 SendKey。

消息将通过 `https://{uid}.push.ft07.com/send/{sendkey}.send` 发送，标题为通知主题，正文为通知内容。

## 订阅规则

通知渠道可以限制只接收指定上游、指定事件或指定倍率分组的事件。留空、空字符串、`null` 或 `[]` 表示接收全部上游的全部事件。

```json
[
	{ "channel_ids": [1, 2], "mode": "all" },
	{ "channel_ids": [3], "mode": "groups", "groups": ["default", "pro"], "events": ["rate_changed"] },
	{ "channel_ids": [4], "mode": "all", "events": ["announcement", "monitor_failed"] }
]
```

- `channel_ids`：上游渠道 ID 列表。历史 `channel_id` 单值规则仍兼容。
- `events`：事件类型列表。缺省、`null` 或 `[]` 表示接收该上游全部事件；非空时只接收指定事件。
- `mode=all`：倍率类事件接收该上游所有分组。
- `mode=groups`：倍率类事件只接收 `groups` 中指定的模型或分组。
- 前端事件选择会把 `rate_structure_changed` / `rate_added` / `rate_removed` 合并显示为“分组变动”，把订阅余量和到期相关事件合并显示为“订阅通知”；保存时仍写入具体事件值。

倍率相关事件的过滤规则：

- 订阅规则会先按 `channel_id` 匹配上游，再按 `events` 匹配事件类型。
- `rate_changed` 会按当前倍率变化的分组名匹配 `groups`。
- `rate_structure_changed` 是同一次扫描内新增分组和删除分组合并后的结构变动通知，也会按分组名匹配 `groups`。
- 对于 `rate_structure_changed`，每个通知渠道会先按自己的订阅规则裁剪新增/删除分组列表，再生成该通知渠道看到的合并通知；因此订阅了不同分组的通知渠道不会看到自己未订阅的分组。
- 如果某个通知渠道在本轮新增/删除分组中没有任何订阅命中的分组，则不会收到这条 `rate_structure_changed` 通知。

公告事件的过滤规则：

- 公告按 `channel_id` 匹配上游渠道。
- 分组过滤不影响公告事件。
- 只要订阅命中该上游且 `events` 为空或包含 `announcement`，公告就会推送。

通知日志支持分页加载，告警动态卡片默认展示最近记录，详情弹窗可继续加载历史记录。所有通知标题统一使用 `[AI 聚合监控]` 前缀，方便在 Telegram、Webhook、邮件等外部通知工具中做过滤和归档。

## 通知事件类型

当前事件类型包括：

- `balance_low`：余额低于阈值。
- `rate_changed`：倍率变化。
- `rate_structure_changed`：分组结构变动。用于同一次扫描内发现新增分组或删除分组时的合并通知，标题使用 `[分组变动通知] 渠道名 · 新增 X / 删除 Y`，正文分别列出新增分组和删除分组。
- `rate_added`：分组新增。该事件类型仍保留在系统事件枚举中，用于兼容历史通知日志或旧版本数据；当前新增分组推送会优先合并进 `rate_structure_changed`。
- `rate_removed`：分组删除。该事件类型仍保留在系统事件枚举中，用于兼容历史通知日志或旧版本数据；当前删除分组推送会优先合并进 `rate_structure_changed`。
- `announcement`：上游公告新增。
- `login_failed`：登录失败。
- `captcha_failed`：验证码求解失败。
- `monitor_failed`：余额、消费或倍率采集失败。
- `subscription_daily_remaining_low`：订阅日剩余用量低于阈值。
- `subscription_weekly_remaining_low`：订阅周剩余用量低于阈值。
- `subscription_monthly_remaining_low`：订阅月剩余用量低于阈值。
- `subscription_expiring`：订阅即将到期。

## 上游公告同步说明

公告同步跟随倍率同步执行：

- 定时倍率同步会同步公告。
- 手动同步倍率也会同步公告。
- 不新增独立公告 cron。

首次同步逻辑：

- 如果某个上游渠道本地还没有公告记录，第一次拉到的公告只入库。
- 首次入库不发送通知，避免历史公告刷屏。

后续同步逻辑：

- 根据 `channel_id + source_key` 去重。
- NewAPI 优先使用公告 `id`。
- NewAPI 没有 `id` 时使用内容、发布时间、类型等字段生成哈希。
- NewAPI `/api/notice` 文本公告按文本内容生成哈希；文本变化会被视为新公告。
- Sub2API 使用公告 `id`。
- 新公告入库后触发 `announcement` 通知。

前端展示：

- 首页底部“上游公告”卡片展示最近公告。
- 点击公告可打开详情弹窗。
- “查看更多”会打开公告列表弹窗，按页加载历史公告。
- 公告正文按 Markdown 渲染。
- 告警动态里会显示公告通知发送成功或失败的日志。

公告列表接口：

```text
GET /api/announcements?page=1&page_size=20
```

返回分页结构：

```json
{
	"items": [],
	"total": 0,
	"page": 1,
	"page_size": 20,
	"pages": 1
}
```

渠道删除逻辑：

- 删除上游渠道时，会自动删除该渠道关联的公告记录。
- 公告也可以通过保留策略按 `first_seen_at` 定期清理。

## 日志与分页接口

通知日志、倍率变化日志、公告列表都使用分页接口，避免首页一次性拉取大量历史数据。

通知日志：

```text
GET /api/notifications/logs?page=1&page_size=20
```

返回内容会附带通知渠道名称、通知渠道类型，以及关联事件对应的上游渠道 ID，便于前端展示。

倍率变化日志：

```text
GET /api/rate-changes?page=1&page_size=20
GET /api/rate-changes?channel_id=1&page=1&page_size=20
```

公告列表：

```text
GET /api/announcements?page=1&page_size=20
```

渠道列表分页：

```text
GET /api/channels?page=1&page_size=20
GET /api/channels?page=1&page_size=-1  (返回全部)
GET /api/channels?page=1&page_size=20&q=vip&status=healthy&tag=备用&sort=balance&order=desc
POST /api/channels/:id/clear-login-info
```

分页模式（带 `page` / `page_size`）支持以下可选参数，不带分页参数的全量列表会忽略它们：

- `q`：按名称、账号、站点地址、备注、标签做子串搜索（忽略英文大小写），最多 200 个字符。
- `status`：`healthy`（健康）、`low`（低余额）、`failed`（登录失败）、`idle`（尚未采集）、`paused`（已暂停）。
- `tag`：单个标签精确匹配（忽略英文大小写），不能含逗号，最多 32 个字符。
- `sort`：`default`（默认，按渠道排序值）、`name`、`username`、`health`、`balance`；名称和账号依次按数字符号、中文（按拼音）、英文字母（忽略大小写）排序，与浏览器 `zh-CN` 的排序一致（个别多音字可能不同）；空账号和未采集余额的渠道总是排在最后。
- `order`：`asc`（默认）或 `desc`，`sort=default` 时忽略。

取值不合法或超长时返回 400；`total` / `pages` 为筛选后的数量。

使用 MySQL 时，`q` / `tag` 的匹配遵循数据库的排序规则：MySQL 8 默认的 `utf8mb4_0900_ai_ci` 还会忽略重音、非英文字母的大小写和全角 / 半角差异（例如 `tag=Ärger` 也会命中 `ärger`，`tag=vip` 也会命中 `ＶＩＰ`）。

返回统一分页结构：

```json
{
  "items": [],
  "total": 0,
  "page": 1,
  "page_size": 20,
  "pages": 1
}
```

## 请求网关使用说明

请求网关把多个上游（已监控的 NewAPI/Sub2API 渠道，或网关内维护的直连 Provider）聚合成统一的 OpenAI / Anthropic / Responses 兼容入口。客户端只持有一把**网关密钥**；真实上游密钥、倍率、协议与故障切换都由服务端处理。

### 概念一览

| 概念 | 说明 |
|------|------|
| **网关组 (Group)** | 配置单元：路由表、组级模型映射、模型列表、重试/顺延/冷却/首字超时、组级 UA |
| **网关密钥 (Key)** | 客户端鉴权用；绑定到组；支持额度、IP 黑白名单；明文只在创建/揭示时可见 |
| **路由 (Route)** | 一条可调度的上游目标：同步渠道+源分组，或直连 Provider；含权重、倍率换算、协议、UA 策略 |
| **直连渠道 (Provider)** | 网关内维护的 base URL + API Key + 默认计费倍率等，无需先做成监控渠道 |
| **模型映射** | 客户端模型名 → 上游模型名；组映射与路由映射可叠加（先路由后组）；支持 `"*"` 通配 |
| **模型列表** | 暴露给 `/v1/models` 的清单；`auto` / `manual` / `hybrid` 三种维护模式 |

### 推荐上手流程

1. **准备上游**
   - 方式 A：在监控里已有 NewAPI/Sub2API 渠道，并知道要用的源分组。
   - 方式 B：在网关页「直连渠道」新建 Provider（填 base URL、密钥、协议与默认计费倍率）。
2. **创建网关组**
   设置排序方向（倍率升/降）、是否扫描后重排、重试与顺延、可选首字超时、组级 UA。
3. **添加路由**
   选 monitor 或 provider；配置权重、倍率换算、`upstream_protocol`、UA 模式；保存后可点「确保上游密钥」（仅 monitor 路由）。
4. **模型**
   配置组/路由映射；用「预览 / 同步 / 探测」维护模型列表。
5. **创建网关密钥**
   把明文 `sk-...` 交给客户端（只显示一次，之后需用「揭示」）。
6. **客户端**
   Base URL 指向本服务（如 `http://host:8080`），路径用 `/v1/...`，鉴权见下。

### 客户端鉴权

```http
Authorization: Bearer sk-...
```

或：

```http
x-api-key: sk-...
```

鉴权规则：

- 密钥须为 **active**，所属组须为 **active**。
- 若配置了 IP 白名单，请求 IP 必须命中白名单；若配置了黑名单，命中则拒绝。
- 若设置了额度 `quota > 0`，则 `quota_used >= quota` 时拒绝。
- 鉴权失败时按客户端协议返回错误体（OpenAI / Anthropic 形态）。

### 公开端点（无需后台登录）

下列路径由网关注册，**使用网关密钥**访问（与管理后台 `/api/*` 分离）：

| 类别 | 路径 |
|------|------|
| 发现 | `GET /v1`（无 SPA 时也可 `GET /`）返回 endpoints 列表 |
| 模型 | `GET /v1/models`；Codex `GET /backend-api/codex/models`；Gemini `GET /v1beta/models` |
| Chat | `POST /v1/chat/completions`、`POST /v1/completions`；别名 `POST /chat/completions` |
| Responses | `POST /v1/responses`、`POST /v1/responses/*`；别名 `/responses`、`/backend-api/codex/responses` |
| Anthropic | `POST /v1/messages`、`POST /v1/messages/count_tokens`；Antigravity 前缀同 Messages |
| 透传 | embeddings / images / videos / alpha 等：改 model 后按上游 path 转发，不做 chat↔messages 转换 |
| 用量 | `GET /v1/usage`（依赖网关密钥） |

流式：请求体 `stream: true`（或等价字段）时走 SSE；Chat 流式会尽量强制 `stream_options.include_usage` 以便末帧统计 usage。

### 请求处理流程（简述）

```text
客户端 → 鉴权(密钥/IP/额度) → 读 body → 取 model
       → 按组策略排序可调度路由
       → 对每条路由:
            模型映射 → 解析上游协议(auto/固定)
            → 必要时协议转换 body / path
            → 发起上游 HTTP（可选首字超时）
            → 成功则回写客户端（流式可增量转换）并记 usage
            → 失败且可顺延则临时暂停路由，试下一条
       → 全部失败则返回最后错误
```

### 协议与互转

| 入站（客户端） | 上游（路由） | 行为 |
|----------------|--------------|------|
| Chat | Chat | 同形态；流式可补 include_usage |
| Chat | Anthropic | body + path 转换；SSE 增量或缓冲转换 |
| Chat | Responses | 转 `/v1/responses` |
| Anthropic | Anthropic | 同形态 |
| Anthropic | Chat / Responses | 互转 |
| Responses | Chat / Anthropic / Responses | 互转或同形态 |
| embeddings 等透传 | 任意 | 不转 chat/messages 语义，只改 model |

路由字段 `upstream_protocol`：

- `auto`：模型名像 Claude 系 → Anthropic；否则跟随入站（Chat 入站不会静默升到 Responses）
- `openai` / `openai_chat`：上游 Chat Completions
- `openai_responses`：上游 Responses
- `anthropic`：上游 Messages

### 调度与计费倍率

- 仅 **enabled** 且未在临时暂停期内的路由参与调度。
- 排序：有效倍率（组方向 asc/desc）→ 权重 → position。
- 有效倍率（网关路由倍率换算）：
  1. `custom` → 使用自定义值
  2. 能匹配源分组 → 用实时 ratio 按 raw / ×100 / ÷100 换算
  3. 否则用路由上已落库的「账号计费倍率」
  4. 再回落换算默认值
- 开启组的「倍率扫描后重排」时，定时倍率扫描结束后会重写相关组的路由顺序与计费倍率快照。
- 费用：`base_cost` 按模型单价 × token 桶；`actual_cost = base_cost × 账号计费倍率`（只乘一次）。

### 故障转移与首字超时

- 默认可顺延：无响应、429、5xx；组开启「4xx 顺延」时全部 4xx 也可顺延。
- 失败路由可写入临时不可调度截止时间（冷却秒数，默认来自 `gateway.tempPauseSeconds` / 组配置）。
- 组级：`retry_count`、`failover_max`、`cooldown_seconds`。
- **首字超时**：配置后，仅当「本请求仍可切换到其它路由」时启用；最后一条可试路由会关闭首字掐断，避免无意义超时。
- 流式一旦已向客户端提交有效 SSE，一般不再切换路由（避免半截双流）。

### 模型列表模式

- `auto`：主要来自各路由上游 `/models` 同步结果去重合并。
- `manual`：以手工/自定义清单为准。
- `hybrid`：同步结果与自定义项合并。
管理页可「预览」「同步」「按模型探测」；公开 `GET /v1/models` 有短时缓存（`gateway.modelsCacheTTLSeconds`）。

### User-Agent

路由 `user_agent_mode`：

- `passthrough`：转发路径不重写客户端 UA
- `group`：使用组级 UA（空则不重写）
- `custom`：使用路由自定义 UA

管理侧拉模型 / 探测无客户端 UA 时，空结果会回落到 `upstream.userAgent` 或内置默认。

### 请求 ID 与排障

- 网关为每次请求生成独立的 **X-Upstream-Ops-Request-Id**（24 位 hex），用于用量记录关联；**不会**用客户端自带的 `X-Request-Id` 作为主键，避免重放污染。
- 客户端的 request id 头会原样转上游；响应中附加网关专用头，不覆盖上游/客户端的 `X-Request-Id`。
- 用量页可按 request id、模型、组、成功失败筛选；失败记录含截断后的上游错误摘要与（脱敏）响应头。

### 路由来源：监控渠道 vs 直连 Provider

| | **Monitor 路由** | **Provider 路由** |
|--|------------------|-------------------|
| 上游从哪来 | 已监控的 NewAPI/Sub2API 渠道 + 源分组 | 网关内 Provider（base URL + 密钥） |
| 密钥 | 可「确保上游密钥」创建/复用专用源 Key | Provider 自身密钥；不走 ensure |
| 计费倍率 | 可跟源分组实时 ratio + 换算 | 默认用 Provider 默认计费倍率；也可 custom |
| 适用场景 | 与余额/倍率监控同一套渠道 | 临时上游、未纳入监控的直连 API |

同一组可混用两类路由；调度与故障转移规则相同。

### 用量与计费记录

每次转发（成功或失败）会尽量落一条用量记录，字段风格对齐 sub2api，便于对账：

- 关联：网关 request id、组、密钥、路由、endpoint、入站/上游协议
- Token：prompt / completion / total，以及缓存读写等分桶（上游有则记）
- 费用：`base_cost`（模型单价 × token）、`actual_cost`（再乘账号计费倍率一次）
- 时延：总延迟、首字延迟（流式）
- 结果：成功 / 失败；失败含截断后的上游错误摘要与脱敏响应头

管理页：列表筛选、stats 聚合、按模型筛选项、清理接口。客户端可用 `GET /v1/usage`（网关密钥）查看自身用量（以接口实际返回为准）。

### 兼容路径速查

除标准 `/v1/*` 外还注册了别名与多产品路径，便于把现有客户端 Base URL 指过来：

| 客户端习惯路径 | 网关行为 |
|----------------|----------|
| `/chat/completions`、`/embeddings` | 等同对应 `/v1/...` Chat 系转发 |
| `/responses`、`/backend-api/codex/responses` | Responses |
| `/backend-api/codex/models` | 模型列表 |
| `/v1beta/models` 等 Gemini 风格 | 模型列表兼容 |
| `/antigravity/v1/messages`、`/antigravity/v1/models` | Anthropic Messages / 模型 |

完整列表以运行中 `GET /v1` 的 endpoints 发现响应为准。

### 配置示例

**Chat 客户端 → Claude 上游（路由协议 anthropic）**

```bash
curl -s http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-你的网关密钥" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4",
    "messages": [{"role":"user","content":"hi"}],
    "stream": false
  }'
```

**Anthropic 客户端**

```bash
curl -s http://127.0.0.1:8080/v1/messages \
  -H "x-api-key: sk-你的网关密钥" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4",
    "max_tokens": 256,
    "messages": [{"role":"user","content":"hi"}]
  }'
```

**Responses 客户端（流式）**

```bash
curl -sN http://127.0.0.1:8080/v1/responses \
  -H "Authorization: Bearer sk-你的网关密钥" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "input": "hi",
    "stream": true
  }'
```

**模型映射 JSON 示例（组或路由）**

```json
{
  "gpt-4o": "gpt-4o-2024-11-20",
  "claude-sonnet-4": "claude-sonnet-4-20250514",
  "*": "gpt-4o-mini"
}
```

先精确匹配，再 `"*"`；组映射与路由映射叠加时，先应用路由映射再应用组映射。

### 运行时配置（`gateway` 段）

可在 `config.yaml` 或系统设置中调整，**应用配置后热更新**（无需重启进程）。字段 ≤0 时回落内置默认：

| 配置项 | 默认 | 含义 |
|--------|------|------|
| `tempPauseSeconds` | 30 | 新建组默认路由冷却（临时不可调度）秒数 |
| `forwardTimeoutSeconds` | 600 | 单次上游转发 / 流式 drain 超时（秒） |
| `modelsCacheTTLSeconds` | 60 | 公开 `GET /v1/models` 缓存 TTL |
| `maxFailoverSwitches` | 8 | 新建组默认最大顺延切换次数 |
| `routeBatchConcurrency` | 8（上限 64） | 批量探测 / ensure / 同步模型等运维并发 |
| `usageErrorBodyBytes` | 32768 | 用量错误 body 落库上限（字节） |
| `usageErrorMsgRunes` | 500 | 用量错误摘要字符上限 |
| `usageErrorHeaderValueRunes` | 8192 | 单条错误响应头值截断 |
| `usageErrorHeadersJSONBytes` | 65536 | 错误响应头 JSON 总上限 |

注意：`upstream.timeoutSeconds` 主要影响**监控侧**访问上游站点（登录、同步等）；网关转发超时看 `gateway.forwardTimeoutSeconds`。两者不要混用。

组级策略（`retry_count` / `failover_max` / `cooldown_seconds` / 首字超时等）在组上单独配置；未填时会参考上述网关默认。

### 管理 API（需后台登录 Token）

管理接口在 `/api/gateway/*`，与公开 `/v1/*` 鉴权体系不同（走管理后台 HMAC token）。

```text
GET/POST     /api/gateway/groups
PUT          /api/gateway/groups/reorder
GET/PUT/DELETE /api/gateway/groups/:id
GET/POST     /api/gateway/groups/:id/keys
GET/PUT      /api/gateway/groups/:id/routes
POST         /api/gateway/groups/:id/routes/ensure-keys
GET          /api/gateway/groups/:id/models/preview
POST         /api/gateway/groups/:id/models/sync
POST         /api/gateway/groups/:id/models/test
PUT/DELETE   /api/gateway/keys/:id
POST         /api/gateway/keys/:id/reveal
POST         /api/gateway/routes/:id/clear-pause
GET/POST     /api/gateway/providers
GET          /api/gateway/providers/options
PUT/DELETE   /api/gateway/providers/:id
POST         /api/gateway/providers/:id/reveal
GET          /api/gateway/usage
GET          /api/gateway/usage/stats
GET          /api/gateway/usage/models
POST         /api/gateway/usage/cleanup
GET/PUT      /api/gateway/prices
GET          /api/gateway/prices/defaults
DELETE       /api/gateway/prices/:id
```

### 后端结构（开发者）

网关代码位于 `backend/gateway`：

- `Service`：组装根与对外兼容委托
- `AdminService`：分组 / 密钥 / 路由 / 直连渠道 / 模型同步
- `Runtime`：鉴权、转发、流式、用量落库
- `protocol`：Chat / Messages / Responses 协议互转（含 SSE 状态机）
- 运行时默认值：`backend/config` 的 `GatewayConfig` / `gateway.*`

### 常见问题

- **401 / invalid api key**：确认用的是网关密钥而非上游密钥；密钥与组均为启用。
- **无路由 / 502**：组内是否有 enabled 路由；是否全部在临时暂停中（可「清除暂停」）。
- **模型 404 / 上游 400**：检查映射后的上游模型名、路由 `upstream_protocol` 是否与真实上游一致。
- **费用为 0 或不准**：内置价目是否覆盖该模型；可在「价格」页覆盖单价；账号计费倍率是否与源分组一致。
- **流式中途断了又重试**：若已写出 SSE 则不会换路由；客户端断开与上游错误会分别记在用量里。
- **确保密钥失败**：monitor 路由需要 ChannelAPI 可用、渠道登录有效；直连 Provider 不走 ensure。

## API Key 管理说明

API Key 管理依赖上游自身接口，不同上游字段存在差异。

常见操作：

- 列表分页。
- 搜索名称或 Key。
- 状态筛选。
- 新建 Key。
- 编辑 Key。
- 删除 Key。
- Reveal 完整 Key 并复制。

NewAPI 场景下会适配 NewAPI 的 token 接口。Sub2API 场景下会适配 Sub2API 的 keys 接口与 group 接口。

## 充值与兑换说明

充值能力取决于上游是否暴露对应接口和支付配置。

支持能力：

- 查询充值金额范围、预设金额、可用支付方式。
- 发起支付宝或微信支付。
- 处理二维码支付、跳转支付和表单提交。
- 展示上游返回的帮助文字和帮助图片。

充值接口：

```text
GET  /api/channels/:id/recharge-info         查询充值信息
POST /api/channels/:id/recharge              发起充值
```

兑换能力：

- 输入兑换码后直接调用上游兑换接口。
- 成功后展示上游返回的结果。
- 常见结果包括余额增加、并发增加、分组订阅、有效期等。

兑换接口：

```text
POST /api/channels/:id/redeem               兑换码兑换
```

请求体：

```json
{"code": "your-redeem-code"}
```

返回示例：

```json
{
  "message": "兑换成功",
  "type": "recharge",
  "value": 10.00,
  "new_balance": 50.00,
  "new_concurrency": 5,
  "group_name": "default",
  "validity_days": 30
}
```

## 订阅管理说明

订阅管理仅对 Sub2API 渠道生效，需在渠道配置中启用 `subscription_enabled`。

订阅计划查询：

```text
GET /api/channels/:id/subscription-info
```

返回上游可用的订阅计划（价格、周期、配额、日/周/月额度上限）和支付方式。

订阅购买/续订：

```text
POST /api/channels/:id/subscription
```

请求体：

```json
{
  "plan_id": "plan_xxx",
  "payment_method": "balance",
  "is_mobile": false
}
```

返回支付发起方式（二维码、跳转链接或表单），前端根据设备类型自适应展示。

订阅用量查询：

```text
GET /api/channels/:id/subscription-usage
```

返回每个订阅的用量详情：

```json
{
  "items": [
    {
      "id": 1,
      "group_id": "g1",
      "group_name": "default",
      "status": "active",
      "starts_at": "2026-01-01T00:00:00Z",
      "expires_at": "2026-07-01T00:00:00Z",
      "expires_in_days": 14,
      "daily": {
        "limit_usd": 5.00,
        "used_usd": 2.30,
        "remaining_usd": 2.70,
        "remaining_percent": 54.0,
        "used_percent": 46.0,
        "window_start": "2026-06-17T00:00:00Z",
        "resets_at": "2026-06-18T00:00:00Z",
        "resets_in_seconds": 21600
      },
      "weekly": { "...": "同上结构" },
      "monthly": { "...": "同上结构" }
    }
  ]
}
```

订阅状态说明：

- `active`：生效中
- `expired`：已过期
- `revoked`：已撤销
- `disabled`：已停用
- `unknown`：未知

## 验证码服务

验证码服务用于上游登录时自动求解 Cloudflare Turnstile。

验证码服务支持单独开启 `proxy_enabled`。只有全局 `proxy.enabled=true` 且该验证码服务开启 `proxy_enabled` 时，余额查询和打码平台请求才会走代理。

支持 provider：

- CapSolver
- 2Captcha
- AntiCaptcha
- YesCaptcha

配置步骤：

1. 在”系统设置 -> 验证码服务”新增 provider。
2. 填写 API Key 和可选 endpoint。
3. 在渠道配置中启用 Turnstile，并选择对应验证码服务。
4. 后续登录上游时会自动拉取 site key 并调用 provider 求解。

Token/Cookie 模式不需要验证码服务。

验证码余额管理接口：

```text
GET    /api/captcha-configs                    列出全部验证码配置（含余额信息）
POST   /api/captcha-configs                    新增验证码配置
PUT    /api/captcha-configs/:id                更新验证码配置
POST   /api/captcha-configs/:id/refresh-balance 手动刷新余额
DELETE /api/captcha-configs/:id                删除验证码配置
```

余额刷新会向对应打码平台查询账户余额，更新 `last_balance`（余额数值）、`balance_unit`（货币单位，如 usd/points）、`balance_at`（刷新时间）字段。查询失败时 `balance_error` 字段会记录错误信息。

## SSE 实时进度推送

部分操作耗时较长（如测试登录、批量同步余额和倍率），后端通过 Server-Sent Events（SSE）向前端推送实时进度：

- 测试登录时会推送登录进度、Turnstile 求解状态和最终结果。
- 单渠道同步会串行推送余额同步和倍率同步的进度。
- 全量同步会推送每个渠道的同步进度，附带渠道索引（当前数/总数）。
- 前端通过 `ReadableStream` 消费 SSE 事件流，在 UI 中实时展示进度状态和结果摘要。

SSE 接口：

```text
POST /api/channels/:id/test-login              测试登录（SSE）
POST /api/channels/:id/sync                    单渠道同步（SSE）
POST /api/channels/sync-all                    全量同步（SSE）
```

响应格式为 `text/event-stream`，每行格式：

```text
data: {“event”:”progress”,”message”:”...”,”step”:1,”total”:3,”ok”:true}
```

## 运行时配置热重载

系统设置页支持运行时热重载，无需重启服务即可使配置生效：

- 可热重载的配置模块：`app`（应用设置）、`auth`（登录鉴权）、`scheduler`（调度器）、`notifications`（通知策略）、`retention`（数据保留）、`proxy`（全局代理）、`upstream`（上游 HTTP 配置）、`gateway`（请求转发网关运行时参数）。
- 点击”应用配置”后，调度器会按新的 cron 间隔重启，通知策略（合并策略、最小变化百分比、订阅告警阈值、冷却时间等）会即时更新。
- 验证码服务和通知渠道的增删改本身即写库实时生效，无需额外应用。
- 其他配置（如数据库连接、HTTP 端口、日志等级）需要重启服务。

## 调度与保留策略

默认调度：

- 余额同步：每 15 分钟。
- 倍率同步：每 30 分钟。
- 订阅用量检查：随余额同步执行，对启用订阅的 Sub2API 渠道自动采集用量并触发低余量/到期告警。
- 验证码余额刷新：随调度自动刷新，也可手动刷新。
- 历史清理：每天凌晨执行。

默认保留策略：

- 监控日志保留 30 天。
- 余额快照保留 90 天。
- 通知日志保留 90 天。
- 上游公告按”公告保留天数”清理，0 表示不清理。
- 倍率变化日志默认不清理。

这些配置可以在系统设置页调整，也可以通过配置文件和环境变量管理。点击”应用配置”后，`app`、`auth`、`scheduler`、`notifications`、`retention`、`proxy`、`upstream`、`gateway` 会立即生效，调度器会按新配置重启。

订阅告警阈值配置（在系统设置 -> 通知配置中调整）：

- `subscriptionDailyRemainingThresholdPct`：日剩余百分比告警阈值，默认 20（即日用量超过 80% 时告警）。
- `subscriptionWeeklyRemainingThresholdPct`：周剩余百分比告警阈值，默认 20。
- `subscriptionMonthlyRemainingThresholdPct`：月剩余百分比告警阈值，默认 20。
- `subscriptionExpiryThresholdHours`：订阅到期告警时长阈值（小时），默认 72（即距到期不足 72 小时时告警）。
- `subscriptionAlertCooldownMinutes`：订阅告警冷却时间（分钟），默认 360。

## 数据安全说明

以下敏感字段会使用 `APP_SECRET` 加密保存：

- 上游账号密码。
- NewAPI Cookie。
- Sub2API Access Token。
- Sub2API 目标上游 Admin API Key。
- 登录会话 Cookie / Token。
- 通知渠道密钥。
- SMTP 密码。
- 验证码平台 API Key。

请注意：

- `APP_SECRET` 必须长期固定。
- 修改 `APP_SECRET` 后，数据库中已有密文无法解密。
- 备份数据库时也应备份对应的 `.env` 或配置文件。

## 常见问题

### 页面可以打开，但 API 请求失败

确认后端服务是否启动，反向代理是否正确转发 `/api/*`。

本地开发时，前端默认运行在：

```text
http://127.0.0.1:3010
```

后端默认运行在：

```text
http://127.0.0.1:8418
```

### 上游登录失败

检查：

- 站点地址是否正确。
- 用户名和密码是否正确。
- 是否需要 Turnstile。
- 是否已配置验证码服务。
- Token/Cookie 模式下凭据是否过期。

### 公告没有推送

检查：

- 是否已经完成过一次公告基线采集。
- 倍率同步是否成功执行。
- 通知渠道是否启用。
- 通知渠道订阅规则是否包含该上游。
- 告警动态里是否出现发送失败日志。

### 倍率变化没有通知

检查：

- 系统设置中的最小变化百分比是否过高。
- 通知渠道订阅是否只订阅了其他分组。
- 倍率变化是否已经写入倍率变化历史。

### 分组新增或删除没有通知

检查：

- 新增分组和删除分组不会再分别发送“分组新增提醒”和“分组删除提醒”，同一次扫描内会合并成 `rate_structure_changed` 事件，并以“分组变动通知”发送。
- 通知渠道如果使用 `mode=groups`，系统会先按订阅分组裁剪本轮新增/删除列表；只有订阅命中的新增或删除分组才会出现在该渠道收到的“分组变动通知”里。
- 如果本轮新增/删除的分组都不在该通知渠道订阅的 `groups` 中，该通知渠道不会收到 `rate_structure_changed`。
- 首次倍率同步只建立倍率基线，不会把首次拉到的所有分组当作新增分组推送，避免历史数据刷屏。

### 余额低告警重复太少

检查系统设置中的“余额低告警冷却时间”。冷却状态会写入数据库，重启后仍然生效。

## License

MIT
