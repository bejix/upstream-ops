# 分组内模型信息查看 — 可行性与设计方案

> 目标：在现有「网关组」展示中，增加「查看该分组内各模型相关信息」的能力。
> 范围：`backend/gateway`、`backend/storage`、`backend/api`、`frontend/components/gateway`。

## 0. 结论摘要

| 问题 | 结论 |
|---|---|
| 是否可行 | **可行**。所需数据 90% 已在库内（分组 `models_json`、路由、用量日志、价格表），缺的只是一个只读聚合接口和展示层。无需改表结构。 |
| 是否值得做 | **值得，但要做减法**。当前要判断「这个分组里某个模型能走哪些渠道、最近有没有人调用、现在通不通」需要人肉在「路由 / 模型 / 用量」三个 tab 之间对照，且模型测试结果刷新即丢。这是高频排障动作。 |
| 建议方案 | **方案 B：分组内「模型」tab 增强 + 侧栏轻量入口 + 详情抽屉**，分 P0/P1/P2 三期落地。 |
| 明确不做 | 参数规模、上下文窗口、模型能力目录等需要外部知识库的字段（无权威数据源，维护成本远高于收益，见 §2.3）。 |

## 原型

可交互高保真原型：**`docs/group-models-prototype.html`**（直接用浏览器打开，无需构建、无外部依赖）。

| 想看的状态 | 打开方式 |
|---|---|
| 默认（模型列表新增列 + 侧栏模型数） | 直接打开 |
| 行内展开摘要 | 末尾加 `#expand` |
| 详情抽屉 | 末尾加 `#drawer` |
| 空分组 / 空状态 | 末尾加 `#empty` |
| 高亮本次新增的元素（虚线框） | 末尾加 `#annotate` |
| 浅色主题 | 末尾加 `#light` |

原型里可直接点击：切换分组、点行展开/收起、点行尾 ⓘ 开抽屉、抽屉内切 4 个 tab、搜索框过滤、筛选 pill、右上角切主题与标注。

---

## 1. 现状盘点（基于当前代码）

### 1.1 数据源

| 数据 | 存储位置 | 关键字段 | 现有接口 | 可直接用于展示 |
|---|---|---|---|---|
| 分组 | `gateway_groups` | `models_json`、`models_mode`、`model_mapping`、`status` | `GET /gateway/groups` | ✅ |
| 组内模型清单 | `gateway_groups.models_json` → `[]ModelListItem` | `id`、`source(sync\|custom)`、`channel_ids`、`sources[{route_id, channel_id, channel_name, source_group_id, source_group_name}]` | 随分组返回，前端 `parseModelsJSON` 解析 | ✅ |
| 路由 | `gateway_routes` | `enabled`、`weight`、`model_mapping`、`upstream_protocol`、`concurrency`、`billing_rate_multiplier`、临时暂停 | `GET /gateway/groups/:id/routes` | ✅ |
| 探测结果 | **仅前端内存** `modelTestResults` | `ok`、`status_code`、`latency_ms`、`error`、`upstream_model` | `POST /gateway/groups/:id/models/test` | ⚠️ 刷新即丢，且会真实打上游 |
| 实时模型聚合 | 内存 `modelsCache`（按组） | 上游返回的模型 ID 列表 | `GET /gateway/groups/:id/models/preview` | ⚠️ 会真打上游，慢、无密钥必失败 |
| 用量日志 | `gateway_usage_logs` | `gateway_group_id`、`requested_model`、`upstream_model`、`total_tokens`、`total_cost`、`success`、`duration_ms`、`created_at` | `GET /gateway/usage`、`/usage/stats`、`/usage/models` | ✅（缺「按模型分组」聚合） |
| 价格 | `model_price_overrides` | `billing_mode`、`input/output_price_per_token`、图片/视频档位 | `GET /gateway/prices`；仓储有 `ListEnabledMap()` | ✅ 一次查询拿全量 map |

### 1.2 三条关键结论

1. **模型清单已经在分组对象里**，不需要新增存储，也不需要每次实时拉上游。
2. **缺的是「聚合视图」**：把清单、路由、用量、价格拼成一张可读的表，以及一个只读接口。
3. **现有的 `preview` / `sync` / `test` 都不能当列表数据源**：三者都会发起上游请求（`probeRouteModel` 单路由最长 30s，`collectGroupModels` 按 `RouteBatchConcurrency` 并发打上游）。若在切换分组时自动调用，等于每次点侧栏都在烧钱和制造超时。**新接口必须严格只读 DB。**

### 1.3 前端现状

- `groups-sidebar.tsx`：每个分组 3 行（名称 + 状态 tag / 描述 / 拖拽·编辑·删除），已有拖拽排序手势。
- `models-panel.tsx`：4 列表格（模型 ID / 渠道-分组 tag / 状态 / 操作），tag 颜色由 `sourceTagTone()` 按探测结果着色（灰=未测、红=失败、绿 ≤5s、黄 ≤10s、橙 >10s）。
- 模型数量上百时全表直出，无搜索、无分页。

---

## 2. 字段取舍（需求点 1）

### 2.1 建议展示

| 字段 | 层级 | 数据来源 | 取舍理由 |
|---|---|---|---|
| 模型 ID | L0 摘要 / L1 列表 | `models_json.id` | 主键，无法省略 |
| 来源类型 `sync`/`custom` | L1 列表 | `models_json.source` | 决定 `models_mode=manual/hybrid` 时是否对外暴露，运维必须一眼看出 |
| 可用来源数（路由 / 渠道-分组） | L1 列表 | `models_json.sources` | 回答「这个模型走哪几条线」，是当前最缺的信息 |
| 来源明细（渠道名 + 源分组） | L2 详情 | `sources` + `routes` | 排障要看具体是哪条上游 |
| 启用路由数 / 暂停数 | L1 列表 | `routes.enabled` + 临时暂停 | 区分「配置了」和「能调度」，比单纯数量有用 |
| 探测可用性 + 延迟 | L1 列表 | 探测结果（触发式） | 高价值但**昂贵**：必须显式触发、带时间戳、默认「未测试」，绝不自动跑 |
| 近 24h 请求数 / 费用 | L1 列表 | `gateway_usage_logs` 聚合 | 判断「死模型」最直接的两个数，一列放得下 |
| 最近调用时间 | L1 列表 | `MAX(created_at)` | 极便宜、信息量大，配合请求数能区分「没人用」和「刚上线」 |
| 成功率（7d） | L2 详情 | `success` 聚合 | 需要额外聚合，列表里放不下，放详情 |
| 上游模型名 / 映射链 | L2 详情 | `routes.model_mapping` + 日志 `upstream_model` / `model_mapping_chain` | 排查 404 的关键；一个模型可能映射到多个不同上游名，必须按来源分组展示 |
| 计费模式（token/按次/图片/视频） | L1 小标签 | `model_price_overrides.billing_mode` | 影响调用方怎么算成本，一个 8px 标签成本极低 |
| 单价（输入/输出） | L2 详情 | `model_price_overrides` | **必须标注「价目表单价，不含路由倍率」**，否则会与实际账单对不上 |

### 2.2 弱推断字段（可做，但要标注）

- **模型类型**（chat / embedding / rerank / image / video）：库里没有显式字段。可从 `billing_mode`（image/video/per_request）+ 模型 ID 规则（`text-embedding-*`、`rerank-*`、`*-image-*`）推断。建议只在详情里显示，且文案写「推测」，不要把推断结果当事实参与筛选。
- **版本**：信息本来就在 ID 里（`gpt-4o-2024-08-06`）。不建议单独建字段或做结构化版本比较——一旦上游命名变化就全错。可在详情里正则提取「日期版本」作辅助显示。

### 2.3 建议明确不做

| 字段 | 不做的理由 |
|---|---|
| 参数规模（7B/70B/MoE） | 无权威数据源。硬编码一张模型目录表，上游每周新增模型，维护成本高且必然过期；**错标比不标更有害**（有人会据此做容量规划）。 |
| 上下文窗口 / 最大输出 | 同上。而且同一模型在不同渠道上的实际上限可能不同，库里没有按路由维度的数据。 |
| 厂商 / 许可协议 | 与运维决策无关。 |

替代方案：在详情里留一个**自由备注**（可选，P2），让用户自己填「这个是 70B」这类知识，成本近零且不会过期。

> 取舍原则：**只展示「系统自己观测到的事实」**（配置、调用、探测），不展示「需要外部知识库的事实」（规格、能力）。前者永不撒谎，后者必然滞后。

---

## 3. 展示位置与层级（需求点 2）

三层结构，职责互不重叠：

```
L0 侧栏摘要        分组名 [启用] [模型 12]
                   一句话描述
                   ────────────────
L1 分组内「模型」tab   搜索 | 筛选 | 排序
                   ┌──────────────────────────────────────────────┐
                   │ 模型 ID        来源  可用来源  24h调用  最近  可用性 │
                   │ gpt-4o        sync  3 渠道   1,204  2分钟前  ● 绿 │
                   │ my-embedding  custom 1 渠道   0      —      ○ 未测 │
                   └──────────────────────────────────────────────┘
                          ↓ 点行展开 / 点「详情」
L2 详情抽屉        概览 · 来源明细 · 用量 · 映射与计费
```

### 3.1 L0：侧栏摘要（零新增请求）

- 位置：`groups-sidebar.tsx` 第 1 行，状态 tag 右侧，加一个中性 chip「模型 12」。
- 数据：分组对象里已有 `models_json`，前端 `parseModelsJSON` 即可算出数量，**不需要新接口**。
- 点击行为：选中该分组 + 切到「模型」tab（复用现有 `selectedGroup` + `Tabs` 状态）。
- 为什么不做异常角标（如「2 个失败」）：依赖探测结果，而探测是触发式的；把「异常」挂在侧栏会诱导用户频繁重测。P2 若持久化探测结果再考虑。

### 3.2 L1：分组内「模型」tab（主战场）

在现有 4 列表格上增列，不新建页面：

| 列 | 内容 | 备注 |
|---|---|---|
| 模型 ID | 原样 + `sync/custom` 徽标 | 现有 |
| 渠道-分组 | 来源 tag（最多 3 个 + `+N`），颜色沿用 `sourceTagTone` | 现有，改为折叠 |
| 可用来源 | `3 启用 / 1 暂停` | 新增，纯 DB |
| 24h 调用 | 请求数；0 时显示 `—` 而非 `0` | 新增，聚合 |
| 最近调用 | 相对时间 | 新增，聚合 |
| 可用性 | `● 通过 1.2s · 5分钟前` / `○ 未测试` | 新增，来自探测缓存 |
| 操作 | 测试 / 详情 / 删除 | 现有 + 新增「详情」 |

- 分组 `models_mode=auto` 时，在表头加一行浅色提示：「当前为实时聚合模式，本列表是 YYYY-MM-DD HH:mm 的快照，仅供管理参考」——现有代码里已有类似 hint 文案，直接复用。
- 空列表、全量无来源等状态见 §6。

### 3.3 L2：详情抽屉

右侧 Sheet（宽 480–560px），内部 4 个小节，用 tab 或锚点切换：

1. **概览**：模型 ID、来源类型、计费模式、最近一次探测（时间/状态码/延迟/错误摘要）、近 7d 成功率。
2. **来源明细**：表格 `路由# / 渠道 / 源分组 / 权重 / 计费倍率 / 启用状态 / 上游模型名`。一个模型对应多行，这是回答「到底走哪条线」的地方。
3. **用量**：24h / 7d / 30d 切换，展示请求数、成功/失败、token（入/出/缓存）、费用、平均耗时、P95（若成本可控）。
4. **映射与计费**：组级 `model_mapping` 与路由级映射的实际命中链、价目表单价（标注不含倍率）。

### 3.4 为什么不把模型列表塞进侧栏（重要）

侧栏宽度约 250px，且已经承载拖拽排序手势。若做成「分组内联展开模型树」：模型一多就横向溢出；嵌套的 `draggable` 会与外层拖拽排序打架；侧栏的定位是**导航**，塞数据表会让它变成第二个页面。详见 §7 方案 A 的缺点。

---

## 4. 交互设计（需求点 3）

| 场景 | 交互 |
|---|---|
| 进入 | 侧栏点「模型 N」chip → 选中分组 + 切 tab；或直接点分组后手动切「模型」tab |
| 展开行内摘要 | 单击行（`role="button"`、`Enter` 同效），accordion 单开，展开区显示来源 tag 全量 + 最近一次探测详情 |
| 打开详情 | 行尾「详情」按钮 → 右侧抽屉（宽屏下可与列表并存，窄屏全屏覆盖） |
| 收起 | 再点行 / 点遮罩 / `Esc` |
| 搜索 | 顶部搜索框，300ms 防抖，匹配模型 ID 与上游模型名 |
| 筛选 | 来源（全部/sync/custom）、可用性（全部/未测/通过/失败）、用量（全部/有调用/零调用） |
| 排序 | 模型 ID、24h 请求数、费用、最近调用；默认按「来源数 desc, 模型 ID asc」 |
| 信息过多 | ① 来源 tag 最多 3 个 + `+N`；② 抽屉内分 4 节而非长滚动；③ 用量表 10 条/页；④ 模型 >50 条前端分页（每页 50），不做无限滚动（便于定位） |
| 探测 | 「测试」按钮**二次确认**（明确写「会真实请求上游并可能产生费用」），单条结果写回前端缓存并打时间戳；批量测试沿用现有 `POST /models/test` |
| 键盘可达 | 行 `tabIndex=0`、Enter 展开；抽屉 focus trap；`Esc` 关闭 |

**与现有交互的冲突检查**：现有「渠道-分组 tag 点击即测」的行为容易误触，建议改为「点击开详情，测试移到行操作列」，并在 tag 上加 `cursor: default`。这是一处必要的破坏性调整，需在 PR 说明里标注。

---

## 5. 接口与数据结构（需求点 4）

### 5.1 设计原则

1. **只读、不打上游**：与 `preview` / `sync` / `test` 彻底分离。
2. **不改表、不改 `models_json` 格式**：新接口是纯新增，老前端不受影响。
3. **一次请求拿列表，按需拿详情**：避免 N+1，也避免首屏返回 200 个模型的完整用量。

### 5.2 新增接口

#### (1) 组内模型概览

```
GET /api/gateway/groups/:id/models/overview?window=24h
```

```json
{
  "group_id": 3,
  "models_mode": "hybrid",
  "snapshot_at": "2026-10-27T13:40:00+08:00",
  "summary": {
    "model_count": 42,
    "sync_count": 38,
    "custom_count": 4,
    "route_count": 6,
    "enabled_route_count": 5,
    "models_without_source": 2,
    "models_zero_usage": 11
  },
  "items": [
    {
      "id": "gpt-4o",
      "source": "sync",
      "billing_mode": "token",
      "source_count": 3,
      "enabled_route_count": 2,
      "paused_route_count": 1,
      "usage": { "requests": 1204, "total_tokens": 812334, "total_cost": 3.42, "last_used_at": "2026-10-27T13:38:12+08:00" },
      "probe": { "ok": true, "status_code": 200, "latency_ms": 1180, "checked_at": "2026-10-27T13:35:00+08:00" }
    }
  ]
}
```

- `probe` 在 P0 由前端已有的 `modelTestResults` 合并渲染，后端可先返回 `null`；P1 改为后端内存缓存。
- `window` 只接受 `24h|7d|30d`，默认 `24h`，非法值回落默认（与现有 `queryInt` 风格一致）。

#### (2) 单模型详情

```
GET /api/gateway/groups/:id/models/detail?model=<urlencoded>&window=7d
```

> **必须用 query 参数而不是 path 参数**：模型名常含 `/`（如 `openai/gpt-4o`、`anthropic/claude-3-5-sonnet`），放进 `:model` 会破坏路由匹配。这是最容易踩的坑。

返回：`sources[]`（含路由/渠道/源分组/权重/倍率/上游模型名）、`usage`（分桶 token + 成功率 + 平均耗时）、`price`（价目表）、`mapping_chain[]`。

### 5.3 后端改动清单

| 文件 | 改动 | 规模 |
|---|---|---|
| `backend/gateway/admin_models_overview.go`（新增） | `GroupModelsOverview`、`GroupModelDetail` 两个只读方法 | ~220 行 |
| `backend/gateway/admin_types.go` | 新增 DTO：`ModelOverviewItem`、`ModelUsageRollup`、`ModelProbeSnapshot`、`ModelDetail` | ~60 行 |
| `backend/storage/gateway.go` | 新增 `ModelStats(q GatewayUsageQuery) (map[string]ModelUsageRollup, error)`：`GROUP BY requested_model` 聚合请求数/成功数/token/费用/最近时间 | ~50 行 |
| `backend/api/gateway_admin.go` | 注册两条 GET 路由（放在 `/models/preview` 之前或之后均可，无冲突） | ~10 行 |

复用点（不重复造）：
- `ParseModelsJSON()` 解析清单；
- `Routes.ListByGroupID()` 取路由；
- `Prices.ListEnabledMap()` **一次查询**拿全量价格 map；
- `applyFilters()` 复用现有用量筛选（注意：需像 `ListModels` 一样先清空 `q.Model`，否则会把自己过滤掉）。

### 5.4 用量聚合 SQL 形态

```sql
SELECT requested_model,
       COUNT(*)                AS requests,
       SUM(success)            AS success_count,
       SUM(total_tokens)       AS total_tokens,
       SUM(total_cost)         AS total_cost,
       MAX(created_at)         AS last_used_at
FROM gateway_usage_logs
WHERE gateway_group_id = ? AND created_at >= ?
GROUP BY requested_model;
```

索引现状：`gateway_group_id`、`created_at`、`requested_model` 各自都有单列索引，200 个模型、百万级日志下可接受（实测目标 < 80ms）。若后续变慢，补复合索引 `(gateway_group_id, requested_model, created_at)`，GORM `AutoMigrate` 加 tag 即可，不影响现有查询。

### 5.5 前端改动清单

| 文件 | 改动 |
|---|---|
| `frontend/lib/api-types.ts` | 新增 overview/detail 类型（snake_case，与现有风格一致） |
| `frontend/lib/queries.ts` | 新增 `useGroupModelsOverview(groupId, window)`（若项目用统一 query 层） |
| `frontend/components/gateway/models-panel.tsx` | 表格增列、搜索/筛选/分页、行展开 |
| `frontend/components/gateway/model-detail-sheet.tsx`（新增） | 详情抽屉 |
| `frontend/components/gateway/groups-sidebar.tsx` | 模型数 chip + 点击回调 |
| `frontend/components/gateway/gateway-utils.ts` | 新增 `usageTone()`（0 调用/正常/异常）、复用 `sourceTagTone` |

### 5.6 兼容性要点

- 不改 `GatewayGroup` / `gateway_routes` / `models_json` 任何字段；
- 新接口对老前端无影响（无人调用）；
- 探测结果持久化若要做（P2），**新建表** `gateway_model_probes(group_id, model, route_id, ok, status_code, latency_ms, error, checked_at)`，不给现有表加列；
- 空 `window`、超长 `model`（>256 字符）一律 400，参数化查询防注入。

---

## 6. 边界情况（需求点 5）

| 场景 | 表现 | 处理 |
|---|---|---|
| 分组无任何模型 | 列表空 | 空状态文案分两种：① 无路由 →「该分组还没有路由，先去『路由』添加来源」+ 跳转按钮；② 有路由但未同步 →「点『从渠道同步』拉取上游模型」 |
| 分组无路由但有 `models_json` | 有模型无来源 | 每个模型显示「未绑定来源」灰标，不报错；`summary.models_without_source` 计数提示 |
| `models_mode=auto` | 列表是旧快照 | 表头提示快照时间 + 「实时聚合模式下列表仅供参考」，不隐藏列表 |
| `custom` 模型无 `sources` | `source_count=0` | 显示「未绑定来源」，详情页给出「在路由页配置映射」的引导 |
| 来源渠道被删 | `channel_name` 为空 | 回退 `#<channel_id>` 占位（复用 `formatChannelGroupLabel` 逻辑），标「来源已失效」 |
| 路由被删但 `sources` 残留 | `route_id` 指向不存在 | 详情页按 `route_id` 反查失败时显示「来源已移除」，**不静默丢弃**（快照是历史事实） |
| 用量表为空 / 已被 cleanup | 全部 0 | 显示 `—` 而非 `0`，并在表头提示「无用量数据（可能已清理或从未调用）」 |
| 概览接口 500 | 列表不可用 | 表格内错误条 + 「重试」，不阻塞分组切换；侧栏 chip 仍可用（本地解析） |
| 用量聚合失败但清单成功 | 部分降级 | 保留清单列，用量列显示 `—` 并在表头标注「用量统计暂不可用」。后端建议把用量查询做成可失败子步骤（返回 `usage_error` 字段），而不是整体 500 |
| 详情抽屉单条失败 | 抽屉内错误 | 抽屉内错误态 + 重试按钮，不影响列表 |
| 模型名超长 / 含特殊字符 | 布局或 URL 问题 | 列表 `truncate` + `title`；URL 必须 `encodeURIComponent`；React 默认转义，禁用 `dangerouslySetInnerHTML` |
| 分组在查看期间被删除 | 404 | 捕获后提示「分组已删除」并回到分组列表 |
| 跨零点 / 时区 | 「24h」口径不一致 | 统一用后端时间窗（`now-24h`），响应里回带 `window_from`/`window_to`，前端只展示不计算 |
| 大分组（500+ 模型） | 首屏卡顿 | 分页 50 + 搜索；overview 接口支持 `limit`/`offset`（默认 200 上限），超出提示「请使用搜索」 |

---

## 7. 方案对比（需求点 6）

### 方案 A：侧栏内联展开模型树

在 `GroupsSidebar` 每个分组下加一个「▸ 模型 12」，展开后显示模型名 + 状态点。

| 优点 | 缺点 |
|---|---|
| 切换分组零跳转，感知最快 | 侧栏 ~250px，模型名一长就截断，多来源信息完全放不下 |
| 实现看起来最"轻" | 与外层分组拖拽排序手势冲突（嵌套 draggable 是经典坑） |
| | 模型多时侧栏变成滚动地狱，且每次展开都要拉数据 |
| | 侧栏语义从"导航"变成"数据视图"，与现有信息密度不匹配 |

**结论：不推荐。** 只保留一个「模型 N」数字 chip 作为入口。

### 方案 B：分组内「模型」tab 增强 + 详情抽屉（推荐）

| 优点 | 缺点 |
|---|---|
| 宽度充足，表格能容纳 6–7 列，信息密度最高 | 需要先选分组再切 tab（多一次点击，用侧栏 chip 缓解） |
| 复用现有 `ModelsPanel` / `sourceTagTone` / `findSourceTest`，改动集中在一个组件 | 现有"点 tag 即测"的交互需要调整，属破坏性改动 |
| 与现有 tab 体系（路由/密钥/模型/用量）一致，用户无学习成本 | 列表与详情需维护两份状态（可接受） |
| 后续可平滑升级为抽屉内嵌图表、导出 | |
| 改动面可控：后端 1 个新文件 + 1 个聚合方法，前端 1 个新组件 | |

### 方案 C：独立「模型总览」页（跨分组）

新增顶级 tab，表格列出所有分组的模型，可按分组筛选。

| 优点 | 缺点 |
|---|---|
| 一次看全站模型，适合"某个模型在哪些组里"这类横向问题 | 偏离本次需求（"分组内查看"），是另一个产品决策 |
| 天然支持全局搜索 | 数据量 = 分组数 × 模型数，聚合成本高，需要更强的分页与缓存 |
| | 与现有「用量」tab 的筛选能力重叠，容易做成两套 |

**结论：不作为本期方案**，但接口设计（`overview` 支持 `group_id` 可选）可为其留口子。

### 推荐：方案 B，理由

1. **信息密度与交互成本的最优解**：主战场必须在宽容器里，侧栏只做入口。
2. **改动最小**：不动表结构、不动 `models_json`、不动现有接口；后端两个只读方法，前端一个抽屉组件。
3. **复用最大化**：颜色规则、来源解析、测试流程全部沿用，用户认知一致。
4. **可分期**：P0 甚至可以不碰探测逻辑（全部显示「未测试」），先把「来源 + 用量」这两个纯 DB 事实做出来，价值已经成立。

---

## 8. 分期落地建议

| 期 | 内容 | 预估 | 价值 |
|---|---|---|---|
| **P0** | `overview` 接口 + 表格新增「可用来源 / 24h 调用 / 最近调用」三列 + 侧栏模型数 chip + 搜索与空态 | 后端 0.5–1d，前端 1d | 解决 80% 的「这模型有人用吗、走哪条线」 |
| **P1** | 详情抽屉（来源明细 / 用量 / 映射与计费）+ 探测结果后端内存缓存（30min TTL，仿 `modelsCache`）+ tag 点击改开详情 | 1.5–2d | 排障闭环，去掉"刷新即丢" |
| **P2** | 探测结果持久化（新表）+ 侧栏异常角标 + 用量导出 CSV + 自定义备注字段 | 2–3d | 长期可观测性 |

## 9. 风险与对策

| 风险 | 对策 |
|---|---|
| 用量聚合在大表上变慢 | 先只查 24h；必要时补复合索引；接口加 30s 缓存 |
| 用户误以为「可用性」= 模型一定好用 | 文案严格区分「已启用（配置事实）」与「探测通过（某时刻事实）」，并显示探测时间 |
| 误触测试产生真实费用 | 二次确认 + 文案明示；把测试从 tag 点击移到独立按钮 |
| `auto` 模式下列表过期误导 | 显示快照时间 + 明确提示列表仅供参考 |
| 模型名含 `/` 导致路由 404 | 详情接口用 query 参数，禁用 path 参数 |
| 一次返回数据过大 | overview 默认 200 条上限 + 分页；详情按需拉 |

---

## 附录：一句话版本

> 数据都在库里，缺的是一张把「清单 + 路由 + 用量 + 价格」拼起来的只读表和一层抽屉；
> 不要做参数规模这类需要外部知识库的字段，不要用 `preview`/`test` 当列表数据源；
> 入口放侧栏（一个数字 chip），主战场放「模型」tab，详情放右侧抽屉。
