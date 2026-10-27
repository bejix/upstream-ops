# 渠道 · 上游分组内的模型信息 —— 可行性评估与设计方案

> **范围**：渠道卡片里「N 个分组」中的**单个上游分组** → 该分组下的上游模型。
> 不含渠道整体（余额/消费/登录）、不含网关分组（GatewayGroup）、**不依赖 API Key**。
> **目标渠道**：WorldClawPro、xem、码少（截图红框标出的三个，均为 NewAPI）。
> **数据**：`部署环境数据/upstream-ops.db`（2026-10-27）+ 用渠道凭证实时调用上游接口，全部为实测。

---

## 0. 结论摘要

| 问题 | 结论 |
|---|---|
| 可行吗 | **完全可行，三个渠道都验证通过。** `GET /api/pricing` 的 `enable_groups` 就是权威的「分组 → 模型」映射。 |
| 值得做吗 | **值得**。同一分组内模型倍率差异可达 75 倍（xem 的 `claude` 组：0.5 ~ 37.5），光看分组倍率完全无法预估成本。 |
| 兼容性 | **零表结构改动**，新增 1 个只读接口 + 前端 chip 展开区。不碰 `rate_snapshots`、不碰 `gateway_*`。 |
| 鉴权 | 复用现有渠道凭证（token 模式的 `password_cipher` 里存 JSON，需带 `New-Api-User` 头），会话失效时匿名兜底。 |
| 推荐方案 | **方案 B：分组 chip 点击 → 内联展开 6 行 → 「查看全部」弹层**，见 §6。 |

### 三个渠道实测结果

| 渠道 | 站点 | 模型 | 已授权分组 | 被模型引用分组 | 本地快照 | 响应 |
|---|---|---|---|---|---|---|
| WorldClawPro | worldclawpro.ai | 7 | 3（GM/特价/default） | 2（GM/特价） | 2 | 3.5 KB |
| xem | ai.xem8k5.top | 78 | 13 | 14 | 13 | 24 KB |
| 码少 | cjms.cc | 14 | 8 | 12 | 8 | 7.6 KB |

---

## 1. 术语与常见误解

| 术语 | 代码位置 | 含义 | 边界 |
|---|---|---|---|
| **渠道** Channel | `storage.Channel` | 一个上游站点账号 | 卡片上的余额、今日消费、暂停监控属于这一层 |
| **上游分组** Upstream Group | `storage.RateSnapshot` | 上游站点的分组（`default` / `特价` / `码少专用`…） | 截图里 chip 那一行就是这一层 |
| **上游模型** Upstream Model | `/api/pricing` 的 `data[].model_name` | `claude-opus-4-6` 这类 | 本次要展示的对象 |

### 最容易踩的坑

**`rate_snapshots.model_name` 存的是「分组名」，不是模型名。** 六处代码互证：

| 位置 | 证据 |
|---|---|
| `connector.RateResult` 注释 | `ModelName 在两个上游分别是"分组名"` |
| `newapi.GetRates` | 请求 `/api/user/self/groups`，把返回 map 的 **key** 写入 `ModelName` |
| 单测 `TestGetRatesAppliesDisplayCurrencyScale` | 断言值为 `claude / gemini / 国模` |
| `channel/service.go:1299` | 注释 `snapshot.ModelName ∈ set.Names` |
| 前端 `channel-cards.tsx:275` | 直接渲染为分组名 |
| 实际数据 | 本地快照里 `GM(0.05)`、`特价(0.04)`、`码少专用` —— 全是分组名 |

字段名是历史包袱，不是 bug。理解这一点是整个需求的前提。

---

## 2. 数据源验证（实测，非推测）

### 2.1 接口能力

```
GET {site}/api/pricing
Headers: Authorization: Bearer <access_token>
         New-Api-User: <user_id>          # token 模式必须
```

三个渠道全部 `200 OK`。码少即使 `password` 模式（会话已过期）也能**匿名**读到，只是分组描述缺失。

### 2.2 响应结构

```
{
  data: [{
    model_name, enable_groups: [...],      ← 分组归属，核心
    model_ratio, completion_ratio,
    quota_type, model_price,
    vendor_id, tags, supported_endpoint_types,
    cache_ratio, create_cache_ratio,
    billing_mode, billing_expr             ← 码少独有
  }],
  usable_group: { 分组名: 描述 },           ← 仅当前账号已授权的分组
  group_ratio:  { 分组名: 倍率 },
  vendors: [{id, name}],
  auto_groups: [...],
  marketplace_order: [...]                 ← 码少独有
}
```

### 2.3 三个渠道的实测数据

**WorldClawPro**（7 模型 / 2 分组有模型）

| 分组 | 倍率 | 模型数 |
|---|---|---|
| 特价 | 0.04 | 4 |
| GM | 0.05 | 3 |

**xem**（78 模型 / 14 分组）

| 分组 | 模型数 | 分组 | 模型数 |
|---|---|---|---|
| 国模 | 18 | plus | 6 |
| claude-max | 15 | gemini | 6 |
| claude | 14 | gpt-image | 4 |
| gpt-pro | 11 | grok | 3 |
| vip | 11 | gemini4 | 2 |
| default / gpt-fast / grok-video | 8 | grok-image | 2 |

**码少**（14 模型 / 12 分组）

| 分组 | 模型数 | 说明 |
|---|---|---|
| 码少专用 | 11 | ⚠️ **不在 `usable_group` 里**（未授权） |
| GPT混池 | 4 | 已授权 |
| 富贵 / Gemini | 3 | 富贵 ⚠️ 未授权 |
| default / 测试 / guomo / Muse | 2 | default ⚠️ 未授权 |
| GLM / 生图0.03/次 / kimi | 1 | GLM ⚠️ 未授权 |

### 2.4 四个必须处理的实测异常

| # | 现象 | 影响 | 处理 |
|---|---|---|---|
| 1 | **`usable_group` 里的分组 ≠ 模型引用的分组**（码少 8 vs 12，4 个未授权） | 未授权分组没有倍率、没有描述，可能账号根本不能调用 | chip 标橙点，倍率显示 `?`，展开时明确标注「未在授权列表」 |
| 2 | **`usable_group` 有但没模型**（WorldClawPro 的 `default`；码少的 `vip`） | 分组存在但无在售模型 | 允许展开，显示「该分组当前没有可售模型」 |
| 3 | **码少用 `tiered_expr` 表达式计费**（11/14 个模型） | `model_ratio` 为 0，倍率列全是 `—`，等于无信息 | 解析表达式给出摘要（如 `入×4 · 出×20`、`$0.03/次`、`$0.02/秒`），full expr 放 tooltip |
| 4 | **同一分组内倍率跨度极大**（xem `claude` 组 0.5 ~ 37.5） | 只看分组倍率 4.55 完全无法预估成本 | 按模型逐行展示倍率，这是本功能的核心价值 |

---

## 3. 展示哪些信息（需求点 1）

| 字段 | 层级 | 来源 | 取舍理由 |
|---|---|---|---|
| 模型名 | L1 | `model_name` | 主键 |
| 模型倍率 | L1 | `model_ratio` | 核心决策信息。**不要乘 `group_ratio`**：实测 `2.5 × 4.55 × 4.55 = 51.7` 明显错误，正确值就是 2.5；两者量级差 10 倍，是不同口径 |
| 补全倍率 | L1 | `completion_ratio` | 输出贵 5 倍还是 1 倍，差一个数量级 |
| 计费模式 | L1 标签 | `quota_type` / `model_price` / `billing_mode` | 按倍率 / 按次 / 阶梯表达式，决定成本怎么算 |
| 阶梯表达式摘要 | L1 小字 | `billing_expr` | 码少独有。无摘要则 11 个模型的倍率列全是 `—` |
| 厂商 | L1 小字 | `vendor_id` → `vendors[]` | 低成本归类；`null` 时静默隐藏 |
| 能力标签 | L1 前 2 个 | `tags` | 覆盖不全（多数 `null`），不宜占主视觉 |
| 跨组归属 | L1 角标 | `enable_groups.length` | xem 22/78 个模型跨多组，提示「这不是该组专属」 |
| 接口协议 | L2 | `supported_endpoint_types` | 判断能否走 Anthropic/OpenAI/Image 端点 |
| 缓存倍率 | L2 | `cache_ratio` / `create_cache_ratio` | 影响长上下文成本，列表放不下 |
| 分组描述、倍率 | L0 信息条 | `usable_group` / `group_ratio` | 分组自述 + 实际倍率 |
| 授权状态 | L0 标签 | `usable_group` 是否含该分组 | **必须显式展示**，否则用户会以为能调其实不能 |

### 明确不做

| 字段 | 理由 |
|---|---|
| **参数规模 / 上下文窗口** | 三个渠道的 `/api/pricing` 都不返回。硬编码目录必然过期，**错标比不标更有害** |
| **模型类型**（chat/embedding） | 无显式字段；`supported_endpoint_types` 是协议不是类型，强行映射会误导 |
| **模型状态** | 只返回在售模型，下架即消失；无法区分「下架」和「从未上线」，不设状态列 |
| **使用情况** | 上游不返回；本地 `gateway_usage_logs` 是网关侧记录，与「上游分组」不同维度，强行关联会造成误读 |
| **版本号提取** | 已在模型名里（`claude-opus-4-6`），结构化提取会大面积失败 |

> 原则：**只展示上游权威接口直接给出的事实**。

---

## 4. 展示位置与层级（需求点 2）

```
L0  分组 chip（形态不变，仅转可点击）
    ┌──────────────┬──────────┬────────┬────────┐
    │● 国模  0.0415│● claude 4.55│● pro 0.07│○ 码少专用 ?│
    └──────────────┴──────────┴────────┴────────┘
       ● 已授权    ○ 未在授权列表（标橙）
              ↓ 点击
L1  内联展开（卡片内，不遮挡，可横向对比其他分组）
    ┌ 信息条：claude · kiro企业 · 分组倍率 4.55 · 模型 14 · [已授权] ┐
    │ 模型                倍率   补全   计费 / 归属                  │
    │ claude-opus-4-6     2.5    ×5    按倍率  +1组                 │
    │ ...（最多 6 行）                                                │
    │ 显示前 6 / 14 个                    查看全部 14 个 →            │
    └─────────────────────────────────────────────────────────────────┘
              ↓
L2  弹层：搜索 / 排序 / 分页 / 协议与缓存列
```

要点：
- **L1 只放 6 行 3 列**。渠道卡片本来就窄，放多了会撑爆整张卡片。
- **未授权分组**用橙色圆点区分，倍率显示 `?` 而非 `—`（`—` 会被误读成「免费」或「无倍率」）。
- **阶梯计费的模型**，倍率列显示「表达式」，计费列给摘要，避免整列空洞。

---

## 5. 交互方式（需求点 3）

| 场景 | 交互 |
|---|---|
| 触发 | 点分组 chip（现 `cursor: default` 纯展示 → 改为可点击） |
| 展开 / 收起 | 再点收起；点另一 chip 自动切换（同时只开一个） |
| 与卡片级「展开」的关系 | 卡片级控制 chip 区高度（`max-h-12 → max-h-150`），chip 级控制模型区，两者独立 |
| 加载 | 懒加载，5 分钟内存缓存（按 `channel_id`）；切 chip 复用同一份 |
| 信息过多 | L1 最多 6 行 → 「查看全部」进 L2；L2 20 行/页 + 搜索 + 排序 |
| 键盘 | chip `tabIndex=0`，Enter/Space 展开，Esc 收起 |
| 提示冲突 | chip hover tooltip 延迟从 150ms 提到 400ms，避免「滑过弹提示 + 点击又展开」双重反馈 |
| 刷新 | `?fresh=1` 强制绕过缓存；UI 显示 `fetched_at` |

---

## 6. 方案对比（需求点 6）

### 方案 A：独立弹窗（复用 `ChannelGroupsDialog` 模式）

| 优点 | 缺点 |
|---|---|
| 改动最小，只加一个 Dialog | 每次看都要开关弹窗，切分组成本高 |
| 表格空间大，信息密度高 | 遮挡卡片，无法对照同卡片其他分组 |
| 与现有「分组」总表交互一致 | 丢失「扫一眼」场景，违背分组的浏览动线 |

### 方案 B：chip 内联展开（6 行）+「查看全部」弹层【推荐】

| 优点 | 缺点 |
|---|---|
| 零跳转回答「这组有什么」 | 卡片内空间有限，L1 只能 3 列 |
| 可横向对比同卡片不同分组（切 chip 即切分组） | 需同时维护 L1/L2 两份视图 |
| 懒加载 + 5min 缓存，切 chip 不重复请求 | 展开后卡片高度变化，需 `scrollIntoView` 处理 |
| 信息分层匹配「多数人只想确认一两个模型」 | |

### 方案 C：卡片内常驻模型列表

xem 13 分组 × 平均 6 模型 ≈ 78 行，卡片彻底失控；且首屏要拉全量。**否决。**

### 推荐：方案 B

1. **信息分层匹配真实提问**。多数场景只想确认「claude 组有没有 opus-4-6」。
2. **改动可控**。后端 1 个新接口（不改表、不改现有接口），前端 1 个新组件 + chip 转可点击。
3. **数据源已验证**。`enable_groups` 是权威映射，三个渠道实测一致。
4. **异常可表达**。授权隔离、阶梯计费这类真实差异，L1/L2 分层刚好能容纳。

---

## 7. 数据结构与接口（需求点 4）

### 7.1 原则

1. **零表结构改动**，不新增表、不给 `rate_snapshots` 加列。
2. **不落库**，上游实时数据放内存缓存（仿现有 `channelGroupsCache`，5 min TTL）。
3. **一次请求拿全量**，前端按分组过滤，不做 N 次请求。
4. **复用现有鉴权**，不新增登录流程。

### 7.2 新增接口

```
GET /api/channels/:id/upstream-catalog?fresh=0
```

```json
{
  "channel_id": 15,
  "channel_name": "码少",
  "channel_type": "newapi",
  "capability": "group_models",
  "degrade": null,
  "fetched_at": "2026-10-27T16:05:00+08:00",
  "pricing_version": "5a90f2b8...",
  "groups": [
    {
      "name": "码少专用",
      "ratio": null,
      "description": null,
      "authorized": false,
      "model_count": 11,
      "in_local_snapshot": false
    },
    {
      "name": "GPT混池",
      "ratio": 0.06,
      "description": "",
      "authorized": true,
      "model_count": 4,
      "in_local_snapshot": true
    }
  ],
  "models": [
    {
      "name": "claude-opus-5.5",
      "groups": ["码少专用", "富贵"],
      "vendor": "Anthropic",
      "tags": null,
      "billing_mode": "tiered_expr",
      "billing_expr": "tier(\"base\", p * 4 + c * 20 + cr * 0.2 + cc * 5)",
      "billing_brief": "入×4 · 出×20",
      "model_ratio": 0,
      "completion_ratio": 1,
      "model_price": 0,
      "quota_type": 0,
      "cache_ratio": null,
      "endpoints": ["anthropic", "openai"]
    }
  ],
  "notes": ["4 个分组被模型引用但不在当前账号的授权列表中"]
}
```

关键设计点：

| 字段 | 为什么这样设计 |
|---|---|
| `groups[].authorized` | **服务端算好**，别让前端去比对 `usable_group`，容易漏 |
| `models[].groups` | 数组形式保留多组归属，前端按组过滤；不按组切分返回，否则丢失「跨组」信息 |
| `billing_brief` | 服务端解析 `billing_expr` 给摘要，前端不重复实现解析 |
| `degrade` | `null / auth_expired / fetch_failed / no_pricing_api`，驱动前端降级 UI |
| `capability` | 预留扩展位（若将来接 Sub2API，值为 `platform_pool`） |

### 7.3 后端改动清单

| 文件 | 改动 | 规模 |
|---|---|---|
| `backend/connector/connector.go` | 新增可选接口 `CatalogProvider`，类型断言调用，**不改主 `Connector` 接口** | ~20 行 |
| `backend/connector/newapi/catalog.go`（新增） | 拉 `/api/pricing`，解析 `enable_groups` + `usable_group` + `group_ratio` + `billing_expr` | ~150 行 |
| `backend/channel/service.go` | `GetUpstreamCatalog(ctx, channelID, fresh)`，复用 `prepareConnectorCall` + 内存缓存 | ~70 行 |
| `backend/api/channels.go` | 注册 `GET /:id/upstream-catalog` | ~10 行 |
| `backend/api/api.go` | `channelService` 接口加 1 行 | 1 行 |

**约 250 行，全部新增，零修改现有逻辑。**

### 7.4 鉴权细节（实测确认）

| 凭证模式 | 存储位置 | 请求方式 |
|---|---|---|
| `token` | `channels.password_cipher`，明文是 `{"access_token":"...","user_id":"9829"}` | `Authorization: Bearer <token>` + **`New-Api-User: <user_id>`** |
| `password` | 同上，明文是密码 | 走 `prepareConnectorCall` 登录拿 session |

**坑一**：token 模式不带 `New-Api-User` 会 401。
**坑二**：漏 `User-Agent` 头会被 WAF 拦成 **403**（不是 401），容易误判为权限问题。
**坑三**：码少的 `/api/user/self/groups` 返回 401 但 `/api/pricing` 匿名可读 → 分组描述会缺失，此时应降级而非报错。

### 7.5 与现有逻辑的兼容

| 现有 | 影响 |
|---|---|
| `rate_snapshots` / `channelRates` 接口 | 不碰，返回结构不变 |
| `ChannelGroupsDialog`（全渠道分组总表） | 不碰，后续可选增强 |
| `only_created_key_groups_enabled` 开关 | 不碰，该开关只影响 chip 是否显示，与本功能独立 |
| 分组 chip 的 Tooltip | 延迟微调，结构不变 |

---

## 8. 边界情况（需求点 5）

| 场景 | 实测案例 | 处理 |
|---|---|---|
| **分组无模型** | WorldClawPro 的 `default`、码少的 `vip` | 「该分组当前没有可售模型」，不报错 |
| **分组未授权** | 码少的 `码少专用`/`富贵`/`default`/`GLM` | chip 橙点 + 倍率显 `?`；展开标「未在授权列表」；不阻止查看 |
| **分组在模型里但 `usable_group` 完全没有** | 同上 4 个 | 归类为「未授权」，`ratio: null`，`description: null` |
| **模型无 `vendor_id`** | xem 的 `vol-haonuhai` | 静默隐藏厂商标签，不留空位 |
| **模型无 `tags`** | 三个渠道多数模型都是 `null` | 隐藏标签区 |
| **`tiered_expr` 计费** | 码少 11/14 个模型 | 倍率列显「表达式」，计费列给摘要，完整 expr 放 tooltip |
| **`model_price > 0` 且 `quota_type != 0`** | 生图类 | 计费显「按次 $x」，倍率列 `—` |
| **同模型跨多分组** | xem 22/78 个 | 行尾 `+N 组` 角标，hover 列全部；倍率仍按当前分组展示 |
| **模型名含 `/`** | xem 的 `google/gemma-4-31b-it` | 接口用 query 传参，**禁止 path 参数** |
| **会话过期** | 码少 `/api/user/self/groups` 401 | `degrade: "auth_expired"`，匿名读 `/api/pricing` 兜底，提示重新登录 |
| **接口 404 / 改版** | — | `degrade: "fetch_failed"` + 截断 200 字的错误摘要 + 重试 |
| **本地快照分组上游已无** | MagicAI 等多出分组 | chip 仍显示，展开标「上游已无此分组，可能已停用」 |
| **大分组（>100 模型）** | xem `国模` 18 个（尚可） | L1 只 6 行；L2 20/页 + 搜索 |
| **并发点击/连点** | — | 同一 `channel_id` 同时只发一个请求，后续复用 Promise |
| **响应过大** | xem 24 KB | 单次读上限 2 MB（仿 `fetchUpstreamModels` 的 `io.LimitReader`），超限降级 |

---

## 9. 分期落地

| 期 | 内容 | 规模 |
|---|---|---|
| **P0** | `GetUpstreamCatalog` + NewAPI provider + chip 可点击 + L1 内联（6 行，含授权标记） | 后端 ~250 行，前端 ~200 行 |
| **P1** | L2 弹层（搜索/排序/分页）+ `billing_expr` 解析 + 详情列 | 前端 ~250 行 |
| **P2** | 用上游实时数据反哺本地快照（清理上游已删除的分组）；Sub2API 支持（若其开放对应接口） | ~150 行 |

## 10. 风险

| 风险 | 对策 |
|---|---|
| 上游接口改版 | `capability` + `degrade` 双字段驱动 UI，改版时降级不崩 |
| 会话频繁过期 | 复用 `prepareConnectorCall` 自动续期；`/api/pricing` 支持匿名兜底 |
| `billing_expr` 语法变化 | 解析失败时回退显示「自定义表达式」+ 原文 tooltip，不抛错 |
| 用户误读未授权分组 | 橙色圆点 + `?` 倍率 + 展开区显式标注，三重提示 |
| 缓存导致数据陈旧 | 显示 `fetched_at` + `?fresh=1` 手动刷新 |

---

## 原型

`docs/upstream-group-newapi-prototype.html` —— 单文件，注入 **三个渠道的真实 `/api/pricing` 响应**，可直接双击打开。

| 场景 | URL hash |
|---|---|
| WorldClawPro（2 分组 / 7 模型） | `#9` |
| xem（13 分组 / 78 模型） | `#14` |
| xem 的 claude 分组展开 | `#14/claude` |
| 码少（含 4 个未授权分组 + 阶梯计费） | `#15` |
| 码少专用（未授权分组展开） | `#15/码少专用` |
| 会话过期降级 | 侧栏「降级演示 → 会话过期」 |

---

## 附录：复现

```bash
# 1. 解密渠道凭证：AES-GCM，key = SHA256(config.yaml 的 security.appSecret)
#    密文格式 = base64(nonce[12] || ciphertext)
# 2. token 模式取 JSON 里的 access_token / user_id
# 3. 请求：
curl -H "Authorization: Bearer <access_token>" \
     -H "New-Api-User: <user_id>" \
     -H "User-Agent: upstream-ops/0.1" \
     https://ai.xem8k5.top/api/pricing
```
