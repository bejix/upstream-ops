# UpstreamOps

[English](README.md) | [简体中文](README.zh.md)

> UpstreamOps is a centralized monitoring and operations dashboard for NewAPI and Sub2API upstream sites. It helps manage upstream accounts, balances, spending, model or group rates, rate changes, upstream API keys, recharge and redeem workflows, subscriptions, announcements, and notification alerts.

It also includes an OpenAI / Claude / Responses compatible request gateway: create gateway API keys, bind monitored channels or direct providers, schedule by rate and weight, convert protocols, fail over on errors, and record per-request usage and cost estimates.

> This project is based on [worryzyy/upstream-hub](https://github.com/worryzyy/upstream-hub). Thanks to [@worryzyy](https://github.com/worryzyy) for the original open-source work.

## Sponsor

<details open>
<summary>Click to expand</summary>

<table>
<tr>
<td width="180"><a href="https://cmzi.com/aff/CHTVTQWE"><img src="https://zhenxiansheng-1251032746.file.myqcloud.com/Markdown/2020/12/29/zi-yuan-32.png" alt="cmzi.com" width="150"></a></td>
<td>Thanks to 触摸云 for sponsoring this project. 触摸云 provides overseas cloud computing services, including Hong Kong cloud servers, US high-defense servers, physical servers, protection services, acceleration CDN, and self-developed CDN systems. UpstreamOps users can use <a href="https://cmzi.com/aff/CHTVTQWE">this link</a>.</td>
</tr>
</table>

</details>

## Why Use UpstreamOps

When you maintain multiple NewAPI or Sub2API upstream accounts, balance, spending, rates, announcements, API keys, subscriptions, and recharge entry points are usually scattered across different admin panels. Manually logging in one by one is repetitive and can easily miss low balances, rate changes, login failures, expiring subscriptions, or upstream announcements.

UpstreamOps focuses on these problems:

- Centralized status view: balances, spending, rates, announcements, subscriptions, and abnormal states across multiple upstreams.
- Less manual checking: scheduled balance, spending, rate, and subscription usage synchronization.
- Faster risk detection: low balances, rate changes, login failures, monitor failures, low subscription quota, and expiring subscriptions can be pushed through notifications.
- Historical tracking: rate changes, balance snapshots, notification logs, and upstream announcements are stored locally.
- Easier operations: API key management, recharge, redeem, subscription purchase, and renewal are available from one entry point.
- Complex network support: global proxy support with per-upstream, per-notification-channel, and per-captcha-provider proxy switches.

## Preview

![UpstreamOps preview 1](docs/images/demo1.png)

![UpstreamOps preview 2](docs/images/demo2.png)

![UpstreamOps preview 3](docs/images/demo3.png)

![UpstreamOps preview 4](docs/images/demo4.png)

![UpstreamOps preview 5](docs/images/demo5.png)

![UpstreamOps preview 6](docs/images/demo6.png)

![UpstreamOps preview 7](docs/images/demo7.png)

## Features

### Request Gateway

- Client endpoints (Bearer / `x-api-key`):
  - `GET /v1` (or `GET /` when the SPA is not mounted) for endpoints discovery
  - `GET /v1/models`, `GET /v1/usage`
  - `POST /v1/chat/completions`, `POST /v1/completions`
  - `POST /v1/responses` (OpenAI Responses, including stream / subpaths)
  - `POST /v1/messages`, `POST /v1/messages/count_tokens` (Anthropic)
  - Passthrough-style: `/v1/embeddings`, `/v1/images/*`, `/v1/videos/*`, and similar (model rewrite, path preserved)
  - Compatibility paths: `/chat/completions`, `/responses`, Codex `/backend-api/codex/*`, Gemini `/v1beta/*`, and more
- **Gateway groups** own routes, mapping, model lists, and retry/failover policy; multiple keys per group share the same config.
- **Two route sources**:
  - **Monitored channel**: NewAPI / Sub2API channel + source group; “ensure upstream keys” creates/reuses dedicated source API keys.
  - **Direct provider**: base URL, API key, default billing rate, auth style, and proxy toggle managed inside the gateway (no monitor channel required).
- Scheduling: source-group rate conversion (raw / ×100 / ÷100 / custom) plus weight; group sort direction; optional re-sort after rate scans.
- Visual model mapping (A→B, `*` wildcard) and model list (pull from upstream with dedupe / custom / auto·manual·hybrid); preview, sync, and probe per route.
- **Protocol conversion** (JSON and incremental SSE):
  - OpenAI Chat ↔ Anthropic Messages
  - OpenAI Chat ↔ OpenAI Responses
  - Anthropic ↔ OpenAI Responses
  - Per-route `upstream_protocol`: `auto` / `openai` (Chat) / `openai_responses` / `anthropic`
- Failover on network errors, 429, and 5xx with temporary pause; optional “failover on 4xx”; group-level retry count, max switches, and cooldown.
- **First-token timeout** (optional): fail fast on the first byte when another route can still be tried.
- **System prompt injection** (per group): ordered rules scoped to all keys or selected keys; injects into Chat `system`, Responses `instructions`, and Anthropic `system` (HTTP and Responses WebSocket).
- **`service_tier` policy** (per group): match `all` / `default` / `priority` / `flex` (and normalize `fast` → `priority`); actions include passthrough, filter, force `default` / `priority`, or block.
- **Responses WebSocket**: upgrades `/v1/responses` when requested; only Responses-capable routes are eligible (protocol cannot change mid-session).
- **Responses tools bridge**: when lowering Responses→Chat/Anthropic, preserves custom tools, namespaced tools, and `tool_search` round-trips.
- **Image / video billing input**: derives request count, size tier, and duration from image/video endpoints and Responses image-generation tool usage for cost estimates.
- User-Agent modes: `passthrough` / `group` / `custom`; admin model pull and probe fall back to the default UA.
- Usage logs aligned with sub2api fields (endpoint, protocol, tokens including cache buckets, cost, latency, first-token latency, success/error detail) with list, stats, model filters, and cleanup.
- Pricing: built-in unit prices (overridable) and `actual_cost = base_cost × account_billing_rate` (via shared rate-conversion rules).
- Runtime knobs (hot-reloadable `gateway` section in system settings): forward timeout, models cache TTL, temp pause, batch concurrency, usage error truncation, and more.
- Admin UI: Dock **Request Gateway** (`/gateway`); management APIs under `/api/gateway/*` (admin auth required).

### Upstream Channel Management

- Supports NewAPI and Sub2API upstreams.
- Supports username/password credentials and token/cookie credentials.
- Enables or disables monitoring per channel.
- Supports custom channel sort order; higher values are displayed and monitored first.
- Configures low-balance alert thresholds.
- Tests login and manually syncs balances and rates.
- Supports extra login form parameters for modified NewAPI or Sub2API login endpoints.
- Supports Cloudflare Turnstile solving for upstream login flows.
- Opens upstream site URLs directly from channel cards.
- Supports clearing saved login information from channel cards.
- Searches channels by name, username, site URL, notes, or tags; filters by status (healthy / low balance / login failed / not yet synced / paused) or tag; sorts by name, username (Chinese by pinyin), health, or balance.
- Supports custom tags and notes per channel; tags can be used directly as filters.
- Channel cards show the login username; the channel list remembers the per-page size in the browser across reloads.
- Supports a "only show groups with created keys" toggle: when enabled, channel cards, group dialogs, rate panels, and rate-change notifications only cover groups that already have API keys; gateway forwarding and notification settings still see all groups, and local rate snapshots always keep the full set.
- Deleting a channel cleans related snapshots, rates, announcements, notification cooldowns, and notification logs.

### Balance and Spending Monitoring

- Shows total balance, today spending, total spending, lowest-balance channel, and abnormal channel count.
- Today spending rolls over at midnight Beijing time: if a channel has no spending sample for the current day (for example, monitoring is paused), its today spending shows 0.
- Periodically collects balance and spending data.
- Displays balance history trends.
- Pushes notifications when balance falls below the configured threshold.
- Supports cooldown for repeated low-balance alerts.
- Supports recharge multiplier conversion for balance, spending, and redeem values, using either the upstream multiplier or a manual divide/multiply mode.

### Rate Monitoring

- Syncs upstream model or group rates.
- Stores current rate snapshots.
- Records rate change history.
- Supports paginated rate change history and channel filters.
- Sends rate change notifications.
- Merges multiple rate changes from the same scan into one notification.
- Merges added and removed groups in the same scan into one structure-change notification.
- Filters small rate changes by minimum percentage.
- Supports notification subscriptions filtered by upstream channel and rate group.
- Provides a full channel group overview with search and sorting by channel or rate.

### Subscription Management and Usage Monitoring

For Sub2API upstream channels, UpstreamOps provides subscription lifecycle management and usage monitoring:

- Queries upstream subscription plans and payment methods.
- Purchases or renews subscriptions.
- Supports QR code, redirect URL, and form-submit payment launch modes.
- Queries daily, weekly, and monthly quota limits, used amount, remaining amount, and remaining percentage.
- Shows subscription expiration time, remaining days, and status.
- Sends low remaining-quota alerts for daily, weekly, and monthly windows.
- Sends expiring-subscription alerts.
- Supports cooldown for repeated subscription alerts.
- Provides summary cards and detail dialogs in the frontend.

### Captcha Provider Balance Management

- Supports CapSolver, 2Captcha, AntiCaptcha, and YesCaptcha.
- Queries captcha provider account balances.
- Refreshes one provider balance manually.
- Refreshes all provider balances in batch.
- Shows balance value, balance unit, refresh time, and error message.

### Global Proxy and Upstream HTTP Settings

- Supports HTTP, HTTPS, and SOCKS5 proxies.
- Supports proxy username and password.
- Allows upstream channels, notification channels, and captcha providers to opt in separately.
- Allows version checks to use the proxy separately.
- Configures upstream request timeout and `User-Agent`.
- Provides proxy connectivity testing in the system settings page.

### Upstream Announcements

- Syncs NewAPI announcements from `/api/status` and `/api/notice`.
- Syncs Sub2API user-visible announcements from `/api/v1/announcements`.
- Announcement sync runs with rate sync and does not require a separate cron task.
- The first sync only creates a baseline and does not push historical announcements.
- New announcements are stored locally and pushed through notification channels.
- Shows recent announcements on the dashboard.
- Supports paginated announcement queries and detail views.
- Renders announcement details as Markdown.
- Cleans up related announcements when an upstream channel is deleted.
- Supports retention-based announcement cleanup.
- Supports channel-level `ignore_announcements`.

### Notification Channels

Supported notification channels:

- Telegram
- Webhook
- Email
- WeCom
- DingTalk
- Feishu
- ServerChan3

Notification channels support subscription filters:

- Empty or `[]`: receive all events.
- `mode=all`: receive all events from selected upstreams.
- `mode=groups`: receive only selected rate groups for rate-related events. Announcement, balance, login failure, and monitor failure events are still filtered by upstream channel.

### Upstream API Key Management

From each channel card, you can manage upstream API keys:

- List API keys.
- Search by name or key.
- Filter by status.
- Create API keys.
- Edit name, group, status, quota, expiration time, IP allowlist or blocklist, model restrictions, and related fields.
- Delete API keys.
- Reveal and copy full keys.
- From the group overview, create an API key directly in a selected group or move one existing key from the same channel into that group.
- Before moving a key, the target group is revalidated and a source-to-target confirmation is shown. Keys already in the target group remain visible but cannot be selected again.

Available fields depend on the upstream type and its API capability.

### Recharge and Redeem

From each channel card, you can handle upstream recharge and redeem workflows:

- Query upstream recharge configuration.
- Supports upstream-provided payment methods such as Alipay and WeChat Pay.
- Supports QR code, redirect URL, and form-submit payment launch modes.
- Prefers QR code on desktop and redirect on mobile.
- Redeems redeem codes online.
- Shows returned balance, concurrency, group subscription, validity period, and related results.
- Sub2API channels additionally support subscription purchase and renewal.

### System Settings

The system settings page manages:

- Admin login authentication.
- Admin username and password.
- Token signing secret.
- Balance sync cron.
- Rate sync cron.
- Scheduler concurrency.
- Monitor log, balance snapshot, notification log, and announcement retention.
- Rate change notification merge policy.
- Minimum rate change percentage for notifications.
- Low-balance alert cooldown.
- Daily, weekly, and monthly subscription remaining percentage thresholds.
- Subscription expiration threshold.
- Subscription alert cooldown.
- Maximum notification retry attempts.
- Global proxy configuration.
- Proxy connectivity test.
- Version check result notification.
- Upstream request timeout and `User-Agent`.
- Request gateway runtime settings (`gateway` section: forward timeout, models cache, temp pause, batch concurrency, usage error truncation, and more).
- Notification channels.
- Captcha providers.

Saving writes the configuration file. Applying settings hot-reloads authentication, scheduler, notification policy, proxy, upstream HTTP, and gateway runtime settings. Notification channels and captcha providers take effect immediately after database writes.

## Quick Start

### Docker Compose with SQLite

SQLite is the default deployment mode.

```bash
cp .env.example .env
```

Edit `.env` and set at least:

```env
APP_SECRET=replace-with-a-random-string-at-least-32-bytes
```

`APP_SECRET` is used to encrypt sensitive fields with AES-GCM, including upstream passwords, tokens, cookies, notification channel secrets, and captcha provider API keys. If you change it later, existing encrypted data cannot be decrypted.

For public access, enable admin login:

```env
AUTH_ENABLED=true
ADMIN_USERNAME=admin
ADMIN_PASSWORD=replace-with-a-strong-password
```

Docker pulls `ghcr.io/bejix/upstream-ops:${IMAGE_TAG:-latest}` by default. Configuration and data are stored in the host `data/` directory.

Start:

```bash
docker compose up -d
```

Default URL:

```text
http://localhost:8080
```

Default database file inside the container:

```text
/app/data/upstream-ops.db
```

The host file is `data/upstream-ops.db`. Runtime system settings are persisted to `data/config.yaml`.

### Pin the Image Version

The default image tag comes from `.env`:

```env
IMAGE_TAG=latest
```

For production, pin a specific version:

```env
IMAGE_TAG=v0.0.10
```

## MySQL Deployment

Use the MySQL compose file together with the base compose file:

```bash
docker compose -f docker-compose.yml -f docker-compose.mysql.yml up -d
```

Required `.env` values:

```env
APP_SECRET=replace-with-a-random-string-at-least-32-bytes
MYSQL_DATABASE=upstreamops
MYSQL_USER=upstreamops
MYSQL_PASSWORD=replace-with-database-password
MYSQL_ROOT_PASSWORD=replace-with-root-password
MYSQL_PORT=33069
```

## Environment Variables

### Basic

```env
HTTP_PORT=8080
IMAGE_TAG=latest
SERVER_MODE=release
LOG_LEVEL=info
```

- `HTTP_PORT`: host port.
- `IMAGE_TAG`: Docker image tag.
- `SERVER_MODE`: Gin mode, usually `release`.
- `LOG_LEVEL`: log level.

### Database

SQLite:

```env
DATABASE_DRIVER=sqlite
DATABASE_PATH=/app/data/upstream-ops.db
```

MySQL:

```env
DATABASE_DRIVER=mysql
DATABASE_HOST=mysql
DATABASE_PORT=3306
DATABASE_USER=upstreamops
DATABASE_PASSWORD=change-me
DATABASE_NAME=upstreamops
```

### Security and Login

```env
APP_SECRET=please-change-me-to-a-long-random-secret-32bytes-min
AUTH_ENABLED=false
ADMIN_USERNAME=admin
ADMIN_PASSWORD=
AUTH_TOKEN_SECRET=
```

- `APP_SECRET`: required master secret.
- `AUTH_ENABLED`: enables admin login.
- `ADMIN_USERNAME`: admin username.
- `ADMIN_PASSWORD`: admin password.
- `AUTH_TOKEN_SECRET`: token signing secret. Falls back to `APP_SECRET` when empty.

## Local Development

Backend:

```bash
go run ./cmd/server
```

Default backend port:

```text
8418
```

Frontend:

```bash
cd frontend
pnpm install
pnpm dev
```

Default frontend development URL:

```text
http://127.0.0.1:3010
```

Checks:

```bash
go test ./...
```

```bash
cd frontend
pnpm lint
pnpm exec tsc --noEmit --incremental false
pnpm build
```

## Proxy and Upstream HTTP Settings

System settings can configure global proxy and upstream request settings. Proxy is disabled by default, protocol defaults to `http`, upstream timeout defaults to `30` seconds, and `User-Agent` defaults to `upstream-ops/0.1`.

Configuration fields:

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

- `proxy.enabled`: enables global proxy.
- `proxy.versionCheckEnabled`: routes version checks through proxy.
- `proxy.protocol`: `http`, `https`, or `socks5`.
- `proxy.host` / `proxy.port`: proxy host and port.
- `proxy.username` / `proxy.password`: optional proxy authentication.
- `upstream.timeoutSeconds`: upstream request timeout.
- `upstream.userAgent`: upstream request `User-Agent` (also the default UA fallback for gateway admin model pull / probe).
- `gateway.*`: request-gateway runtime knobs (forward timeout, models cache, temp pause, batch concurrency, usage error truncation, and more); hot-reloadable from system settings.
- When `proxy.enabled=false`, per-channel `proxy_enabled` settings do not take effect.

Proxy test endpoint:

```text
POST /api/settings/proxy/test
```

## Upstream Channel Configuration

Upstream channels can enable `proxy_enabled` individually. Upstream login, balance sync, rate sync, announcement sync, API key management, recharge, redeem, and subscription APIs use proxy only when both global proxy and channel proxy are enabled.

### NewAPI

NewAPI supports two credential modes.

Username/password mode:

- Provide upstream site URL, username, and password.
- If the login endpoint requires extra fields, provide a JSON object in extra form parameters.
- If Turnstile is enabled, configure a captcha provider first, then enable Turnstile in the channel.

Newer NewAPI builds (QuantumNous/new-api and forks) issue short-lived `access_token` Bearer JWTs (default 15 minutes) at login and return a `new_api_refresh` refresh token. UpstreamOps automatically calls `/api/user/auth/refresh` to renew the access token and rotate the refresh token before it expires, so frequent re-login is not needed. Older NewAPI builds still authenticate via `Set-Cookie: session=...` plus the `New-Api-User` header; the connector auto-detects and supports both.

Login and refresh requests send `Origin` / `Referer` derived from the site URL. Only an explicit 401 (refresh token invalid) falls back to password login; other failures such as 403, 409, 429, 5xx, or network errors keep the stored session and record the error, so repeated logins cannot exhaust the upstream session limit. Session refresh and login are serialized per channel.

Token/cookie mode:

```json
{
  "cookie": "session=xxx; other=yyy",
  "user_id": "123"
}
```

NewAPI token mode also supports the system access token (`user.access_token`, the 32 character token generated from the personal settings page). Use `access_token` instead of `cookie`. Cookie and access token are mutually exclusive, but `user_id` is always required:

```json
{
  "access_token": "your-system-access-token",
  "user_id": "123"
}
```

When editing a NewAPI token/cookie channel, the form shows the saved `user_id` for reuse, while the saved cookie or access token remains hidden.

### Sub2API

Sub2API supports username/password mode and token mode.

Token mode credentials:

```json
{
  "access_token": "your-access-token",
  "refresh_token": "your-refresh-token"
}
```

`refresh_token` is optional but recommended. When present, Sub2API sessions and token-mode credentials can be refreshed automatically after access-token expiration. Without `refresh_token`, paste updated credentials when the token expires.

### Clear Login Information

The channel card menu provides a clear-login action:

- Password mode: clears only cached login sessions.
- Token mode: clears cached sessions and the saved token/cookie credential JSON.

## Notification Channel Configuration

Notification secrets, webhooks, and SMTP passwords are encrypted at rest. Add or edit a notification channel with the JSON configuration matching its type.

Notification channels can enable `proxy_enabled` individually. Telegram, Webhook, WeCom, DingTalk, Feishu, and ServerChan3 requests use proxy only when both global proxy and notification-channel proxy are enabled.

### Telegram

```json
{
  "bot_token": "1234567890:AAEh...",
  "chat_id": "-1001234567890"
}
```

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

Webhook body example:

```json
{
  "event": "announcement",
  "subject": "[UpstreamOps] xxx",
  "body": "notification body",
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

### WeCom

```json
{
  "webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxx"
}
```

### DingTalk

```json
{
  "webhook_url": "https://oapi.dingtalk.com/robot/send?access_token=xxx",
  "secret": "SEC..."
}
```

### Feishu

```json
{
  "webhook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/xxxx",
  "secret": "..."
}
```

### ServerChan3

```json
{
  "uid": "your UID",
  "sendkey": "sctp_xxx"
}
```

Messages are sent through `https://{uid}.push.ft07.com/send/{sendkey}.send`.

## Subscription Rules

Notification channels can limit which upstreams, events, or rate groups they receive. Empty value, empty string, `null`, or `[]` means all upstreams and all events.

```json
[
  { "channel_ids": [1, 2], "mode": "all" },
  { "channel_ids": [3], "mode": "groups", "groups": ["default", "pro"], "events": ["rate_changed"] },
  { "channel_ids": [4], "mode": "all", "events": ["announcement", "monitor_failed"] }
]
```

- `channel_ids`: upstream channel ID list. Historical `channel_id` single-value rules are still accepted.
- `events`: event type list. Empty means all events for that upstream.
- `mode=all`: receive all rate groups.
- `mode=groups`: receive only selected groups for rate-related events.

## Notification Event Types

- `balance_low`: balance below threshold.
- `rate_changed`: rate changed.
- `rate_structure_changed`: group structure changed.
- `rate_added`: group added. Kept for historical compatibility.
- `rate_removed`: group removed. Kept for historical compatibility.
- `announcement`: new upstream announcement.
- `login_failed`: login failed.
- `captcha_failed`: captcha solving failed.
- `monitor_failed`: balance, spending, or rate collection failed.
- `subscription_daily_remaining_low`: daily subscription remaining quota below threshold.
- `subscription_weekly_remaining_low`: weekly subscription remaining quota below threshold.
- `subscription_monthly_remaining_low`: monthly subscription remaining quota below threshold.
- `subscription_expiring`: subscription is about to expire.

## Request Gateway Guide

The request gateway aggregates multiple upstreams (monitored NewAPI/Sub2API channels, or direct Providers maintained inside the gateway) into a unified OpenAI / Anthropic / Responses–compatible entry. Clients hold a single **gateway key**; real upstream secrets, billing multipliers, protocol conversion, and failover are handled server-side.

### Concepts

| Concept | Description |
|---------|-------------|
| **Group** | Configuration unit: route table, group-level model map, model list, retry / failover / cooldown / first-token timeout, group UA |
| **Key** | Client auth credential bound to a group; supports quota and IP allow/deny lists; plaintext is shown only on create/reveal |
| **Route** | A schedulable upstream target: monitor channel + source group, or a direct Provider; weight, ratio conversion, protocol, UA policy |
| **Provider** | Gateway-managed base URL + API key + default billing ratio; no need to create a monitor channel first |
| **Model map** | Client model name → upstream model name; group and route maps stack (route first, then group); `"*"` wildcard supported |
| **Model list** | Exposed via `/v1/models`; modes: `auto` / `manual` / `hybrid` |

### Recommended setup

1. **Prepare upstreams**
   - Option A: an existing monitored NewAPI/Sub2API channel and the source group to use.
   - Option B: create a **Provider** on the gateway page (base URL, key, protocol, default billing ratio).
2. **Create a gateway group**
   Sort direction (ratio asc/desc), reorder-after-scan, retry/failover, optional first-token timeout, group UA.
3. **Add routes**
   Choose monitor or provider; set weight, ratio conversion, `upstream_protocol`, UA mode; optionally **Ensure upstream keys** (monitor routes only).
4. **Models**
   Configure group/route maps; use Preview / Sync / Probe to maintain the model list.
5. **Create a gateway key**
   Give clients the plaintext `sk-...` (shown once; use **Reveal** later).
6. **Client**
   Base URL points at this service (e.g. `http://host:8080`), paths under `/v1/...`, auth as below.

### Client authentication

```http
Authorization: Bearer sk-...
```

or:

```http
x-api-key: sk-...
```

Rules:

- Key must be **active**; its group must be **active**.
- If an IP allowlist is set, the request IP must match; if a denylist is set, a match rejects the request.
- If `quota > 0`, requests are rejected when `quota_used >= quota`.
- Auth failures return an error body shaped for the client protocol (OpenAI / Anthropic).

### Public endpoints (no admin login)

These paths are registered by the gateway and use a **gateway key** (separate from admin `/api/*`):

| Category | Paths |
|----------|--------|
| Discovery | `GET /v1` (also `GET /` when SPA is off) returns endpoint list |
| Models | `GET /v1/models`; Codex `GET /backend-api/codex/models`; Gemini `GET /v1beta/models` |
| Chat | `POST /v1/chat/completions`, `POST /v1/completions`; alias `POST /chat/completions` |
| Responses | `POST /v1/responses`, `POST /v1/responses/*`; aliases `/responses`, `/backend-api/codex/responses` |
| Anthropic | `POST /v1/messages`, `POST /v1/messages/count_tokens`; Antigravity prefix same as Messages |
| Passthrough | embeddings / images / videos / alpha, etc.: rewrite model then forward upstream path (no chat↔messages conversion) |
| Usage | `GET /v1/usage` (gateway key) |

Streaming: body `stream: true` (or equivalent) uses SSE. Chat streams try to force `stream_options.include_usage` so the final frame can report usage.

### Request flow (summary)

```text
Client → auth (key / IP / quota) → read body → take model
       → order schedulable routes by group policy
       → for each route:
            model map → resolve upstream protocol (auto / fixed)
            → convert body / path if needed
            → HTTP to upstream (optional first-token timeout)
            → on success: write client response (stream may convert incrementally) + record usage
            → on fail + failover allowed: temp-pause route, try next
       → all failed → return last error
```

### Protocols and conversion

| Inbound (client) | Upstream (route) | Behavior |
|------------------|------------------|----------|
| Chat | Chat | Same shape; stream may inject include_usage |
| Chat | Anthropic | body + path convert; SSE incremental or buffered |
| Chat | Responses | convert to `/v1/responses` |
| Anthropic | Anthropic | Same shape |
| Anthropic | Chat / Responses | Cross-convert |
| Responses | Chat / Anthropic / Responses | Cross-convert or same shape |
| embeddings etc. | any | No chat/messages semantics; model rewrite only |

Route field `upstream_protocol`:

- `auto`: Claude-like model names → Anthropic; otherwise follow inbound (Chat inbound does **not** silently upgrade to Responses)
- `openai` / `openai_chat`: upstream Chat Completions
- `openai_responses`: upstream Responses
- `anthropic`: upstream Messages

### Scheduling and billing ratio

- Only **enabled** routes not inside a temporary pause window are scheduled.
- Order: effective ratio (group direction asc/desc) → weight → position.
- Effective ratio (aligned with monitor account sync logic):
  1. `custom` → use custom value
  2. if source group matches → live ratio with raw / ×100 / ÷100 conversion
  3. else stored “account billing ratio” on the route
  4. else conversion default
- When the group enables reorder-after-ratio-scan, ratio scan rewrites route order and billing-ratio snapshots for related groups.
- Cost: `base_cost` from model unit price × token buckets; `actual_cost = base_cost × account billing ratio` (multiplied once).

### Failover and first-token timeout

- Default failover: no response, 429, 5xx; with group “failover on 4xx”, all 4xx may failover too.
- Failed routes may get a temporary not-schedulable deadline (cooldown seconds from `gateway.tempPauseSeconds` / group config).
- Group: `retry_count`, `failover_max`, `cooldown_seconds`.
- **First-token timeout**: enabled only when another route can still be tried; the last candidate turns first-token cut-off off so a pointless timeout is avoided.
- Once valid SSE has been committed to the client, the gateway generally does not switch routes (avoids half-stream dual responses).

### Model list modes

- `auto`: mainly dedupe-merge of each route’s upstream `/models` sync.
- `manual`: hand / custom list wins.
- `hybrid`: merge sync results with custom entries.
Admin UI: Preview / Sync / probe-by-model. Public `GET /v1/models` uses a short TTL cache (`gateway.modelsCacheTTLSeconds`).

### User-Agent

Route `user_agent_mode`:

- `passthrough`: do not rewrite client UA on the forward path
- `group`: use group-level UA (empty → no rewrite)
- `custom`: use route custom UA

Admin model list / probe without a client UA falls back to `upstream.userAgent` or a built-in default when empty.

### Request ID and troubleshooting

- Each request gets a gateway-owned **X-Upstream-Ops-Request-Id** (24 hex chars) used to correlate usage rows. Client `X-Request-Id` is **not** used as the primary key (avoids replay pollution).
- Client request-id headers are forwarded upstream as-is; the gateway response adds its own header without overwriting upstream/client `X-Request-Id`.
- Usage page filters by request id, model, group, success/failure; failures store a truncated upstream error summary and redacted response headers.

### Route sources: monitor vs direct Provider

| | **Monitor route** | **Provider route** |
|--|-------------------|--------------------|
| Upstream | Monitored NewAPI/Sub2API channel + source group | Gateway Provider (base URL + key) |
| Keys | **Ensure upstream keys** creates/reuses a dedicated source key | Provider’s own key; no ensure |
| Billing ratio | Live source-group ratio + conversion | Provider default billing rate (or custom) |
| Best for | Same channels as balance/rate monitoring | Ad-hoc or unmonitored direct APIs |

A group may mix both route types; scheduling and failover rules are the same.

### Usage and billing records

Each forward attempt (success or failure) tries to write a usage row, field style aligned with sub2api for reconciliation:

- Correlation: gateway request id, group, key, route, endpoint, inbound/upstream protocol
- Tokens: prompt / completion / total, plus cache read/write buckets when the upstream reports them
- Cost: `base_cost` (unit price × tokens), `actual_cost` (× account billing ratio once)
- Latency: total latency, first-token latency (streaming)
- Outcome: success / failure; failures store truncated upstream error summary and redacted headers

Admin UI: list filters, stats aggregation, model filter options, cleanup API. Clients may call `GET /v1/usage` with a gateway key (see actual response shape).

### Compatibility paths

Besides standard `/v1/*`, aliases and multi-product paths help point existing clients at this service:

| Client-style path | Gateway behavior |
|-------------------|------------------|
| `/chat/completions`, `/embeddings` | Same as corresponding `/v1/...` Chat family |
| `/responses`, `/backend-api/codex/responses` | Responses |
| `/backend-api/codex/models` | Model list |
| `/v1beta/models` (Gemini-style) | Model list compatibility |
| `/antigravity/v1/messages`, `/antigravity/v1/models` | Anthropic Messages / models |

Canonical list: `GET /v1` discovery payload at runtime.

### Examples

**Chat client → Claude upstream (route protocol anthropic)**

```bash
curl -s http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-YOUR_GATEWAY_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4",
    "messages": [{"role":"user","content":"hi"}],
    "stream": false
  }'
```

**Anthropic client**

```bash
curl -s http://127.0.0.1:8080/v1/messages \
  -H "x-api-key: sk-YOUR_GATEWAY_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4",
    "max_tokens": 256,
    "messages": [{"role":"user","content":"hi"}]
  }'
```

**Responses client (streaming)**

```bash
curl -sN http://127.0.0.1:8080/v1/responses \
  -H "Authorization: Bearer sk-YOUR_GATEWAY_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "input": "hi",
    "stream": true
  }'
```

**Model map JSON (group or route)**

```json
{
  "gpt-4o": "gpt-4o-2024-11-20",
  "claude-sonnet-4": "claude-sonnet-4-20250514",
  "*": "gpt-4o-mini"
}
```

Exact match first, then `"*"`. When group and route maps both apply, **route map first**, then group map.

### Runtime config (`gateway` section)

Editable in `config.yaml` or system settings; **hot-reloads after Apply** (no process restart). Values ≤0 fall back to built-in defaults:

| Key | Default | Meaning |
|-----|---------|---------|
| `tempPauseSeconds` | 30 | Default route cooldown (temp not-schedulable) for new groups |
| `forwardTimeoutSeconds` | 600 | Per-upstream forward / stream drain timeout (seconds) |
| `modelsCacheTTLSeconds` | 60 | Public `GET /v1/models` cache TTL |
| `maxFailoverSwitches` | 8 | Default max failover switches for new groups |
| `routeBatchConcurrency` | 8 (cap 64) | Concurrency for batch probe / ensure / model sync |
| `usageErrorBodyBytes` | 32768 | Max error body bytes stored on usage rows |
| `usageErrorMsgRunes` | 500 | Max error summary runes |
| `usageErrorHeaderValueRunes` | 8192 | Per error response-header value truncation |
| `usageErrorHeadersJSONBytes` | 65536 | Max error headers JSON size |

Note: `upstream.timeoutSeconds` mainly affects **monitor-side** calls to upstream sites (login, sync). Gateway forward timeout is `gateway.forwardTimeoutSeconds` — do not confuse the two.

Group policy (`retry_count` / `failover_max` / `cooldown_seconds` / first-token timeout, etc.) is configured per group; empty fields fall back to gateway defaults where applicable.

### Management APIs (admin auth required)

Admin APIs live under `/api/gateway/*` with a different auth system than public `/v1/*` (admin HMAC token).

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

### Backend layout (developers)

Gateway code lives under `backend/gateway`:

- `Service`: composition root and public API delegates
- `AdminService`: groups / keys / routes / direct providers / model sync
- `Runtime`: auth, forward, streaming, usage recording
- `protocol`: Chat / Messages / Responses conversion (including SSE state machines)
- Runtime defaults: `GatewayConfig` / `gateway.*` in `backend/config`

### FAQ

- **401 / invalid api key**: use a gateway key, not an upstream key; key and group must both be enabled.
- **No route / 502**: group has enabled routes? all temp-paused? use **Clear pause**.
- **Model 404 / upstream 400**: check mapped upstream model name and whether route `upstream_protocol` matches the real upstream.
- **Cost is 0 or wrong**: built-in prices may not cover the model; override on the Prices page; check account billing ratio vs source group.
- **Stream cut off then retry**: once SSE is committed, routes are not switched; client disconnect vs upstream error are recorded separately in usage.
- **Ensure keys failed**: monitor routes need ChannelAPI and a valid channel login; direct Providers do not use ensure.

## APIs and Operations

Announcement list:

```text
GET /api/announcements?page=1&page_size=20
```

Notification logs:

```text
GET /api/notifications/logs?page=1&page_size=20
```

Notification log rows include the upstream channel ID when the event is tied to a specific upstream channel.

Rate change logs:

```text
GET /api/rate-changes?page=1&page_size=20
GET /api/rate-changes?channel_id=1&page=1&page_size=20
```

Channels:

```text
GET /api/channels?page=1&page_size=20
GET /api/channels?page=1&page_size=-1
GET /api/channels?page=1&page_size=20&q=vip&status=healthy&tag=backup&sort=balance&order=desc
POST /api/channels/:id/clear-login-info
```

Paged mode (with `page` / `page_size`) accepts these optional parameters; the unpaged full list ignores them:

- `q`: substring search over name, username, site URL, notes, and tags (ASCII case-insensitive), up to 200 characters.
- `status`: `healthy`, `low`, `failed`, `idle` (not yet synced), or `paused`.
- `tag`: exact match on a single tag (ASCII case-insensitive), no commas, up to 32 characters.
- `sort`: `default` (channel sort order), `name`, `username`, `health`, or `balance`; name and username put digits and symbols first, then Chinese by pinyin, then letters (case-insensitive), matching the browser's `zh-CN` order (a few polyphonic characters may differ); blank usernames and channels without a balance always come last.
- `order`: `asc` (default) or `desc`; ignored for `sort=default`.

Invalid or over-long values return 400; `total` / `pages` reflect the filtered result.

On MySQL, `q` / `tag` matching follows the database collation: the MySQL 8 default `utf8mb4_0900_ai_ci` also ignores accents, non-ASCII letter case, and full-width / half-width differences (for example, `tag=Ärger` also matches `ärger`, and `tag=vip` also matches `ＶＩＰ`).

Recharge:

```text
GET  /api/channels/:id/recharge-info
POST /api/channels/:id/recharge
```

Redeem:

```text
POST /api/channels/:id/redeem
```

Subscription:

```text
GET  /api/channels/:id/subscription-info
POST /api/channels/:id/subscription
GET  /api/channels/:id/subscription-usage
```

Captcha providers:

```text
GET    /api/captcha-configs
POST   /api/captcha-configs
PUT    /api/captcha-configs/:id
POST   /api/captcha-configs/:id/refresh-balance
DELETE /api/captcha-configs/:id
```

SSE progress endpoints:

```text
POST /api/channels/:id/test-login
POST /api/channels/:id/sync
POST /api/channels/sync-all
```

## Runtime Configuration Hot Reload

The system settings page supports runtime hot reload without restarting the service.

Hot-reloadable modules:

- `app`
- `auth`
- `scheduler`
- `notifications`
- `retention`
- `proxy`
- `upstream`
- `gateway`

Database connection, HTTP port, and log level still require restart.

## Scheduler and Retention

Default schedules:

- Balance sync: every 15 minutes.
- Rate sync: every 30 minutes.
- Subscription usage check: runs with balance sync.
- Captcha balance refresh: scheduled and manual refresh are supported.
- History cleanup: daily.

Default retention:

- Monitor logs: 30 days.
- Balance snapshots: 90 days.
- Notification logs: 90 days.
- Upstream announcements: controlled by announcement retention days. `0` disables cleanup.
- Rate change logs are not cleaned by default.

## Data Security

The following sensitive fields are encrypted with `APP_SECRET`:

- Upstream account passwords.
- NewAPI cookies.
- Sub2API access tokens.
- Sub2API target Admin API Keys.
- Login session cookies and tokens.
- Notification channel secrets.
- SMTP passwords.
- Captcha provider API keys.

Important:

- `APP_SECRET` must remain stable.
- Changing `APP_SECRET` makes existing encrypted data undecryptable.
- Back up `.env` or configuration files together with the database.

## FAQ

### The page opens, but API requests fail

Check whether the backend service is running and whether reverse proxy routes `/api/*` correctly.

Frontend development URL:

```text
http://127.0.0.1:3010
```

Backend URL:

```text
http://127.0.0.1:8418
```

### Upstream login fails

Check the site URL, username, password, Turnstile requirement, captcha provider configuration, and whether token or cookie credentials have expired.

### Announcements are not pushed

Check whether the first announcement baseline sync has completed, rate sync runs successfully, notification channels are enabled, subscription rules include the upstream, and failed notification logs exist.

### Rate changes are not pushed

Check the minimum change percentage, notification subscription groups, and rate change history.

### Added or removed groups are not pushed

Added and removed groups are merged into a `rate_structure_changed` notification for the same scan. If a notification channel uses `mode=groups`, the added/removed list is filtered by subscribed groups before generating the notification.

The first rate sync only creates a baseline and does not push all existing groups as newly added groups.

### Low-balance alerts repeat too rarely

Check the low-balance alert cooldown in system settings. Cooldown state is stored in the database and survives restarts.

## License

MIT
