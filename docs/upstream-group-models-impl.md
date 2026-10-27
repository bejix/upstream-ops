# 渠道 · 上游分组内的模型信息 —— 实现说明（v1）

> 已实现并验证。对应设计文档 `upstream-group-models-design.md`，原型 `upstream-group-newapi-prototype.html`。

## 一、已实现范围

### 后端

| 文件 | 内容 |
|---|---|
| `backend/connector/catalog.go`（新增） | 可选接口 `CatalogProvider` + `Catalog`/`CatalogGroup`/`CatalogModel` 类型。**不改主 `Connector` 接口**，用类型断言接入 |
| `backend/connector/newapi/catalog.go`（新增） | 拉 `/api/pricing`，解析 `enable_groups`（分组归属）、`usable_group`（授权列表）、`group_ratio`（分组倍率）、`vendors`、`billing_expr`；三档降级 |
| `backend/connector/sub2api/catalog.go`（新增） | 显式返回「不支持」，区分「没实现」与「上游没这个能力」 |
| `backend/channel/catalog.go`（新增） | `GetUpstreamCatalog(ctx, channelID, fresh)`，复用 `prepareConnectorCall` 鉴权，5 分钟内存缓存 + `InvalidateUpstreamCatalog` |
| `backend/channel/service.go` | 新增 `catalogCache` 字段并在 `NewService` 初始化 |
| `backend/api/channels.go` | 注册 `GET /api/channels/:id/upstream-catalog?fresh=0` |
| `backend/api/api.go` | `channelService` 接口加一行 |

**零表结构改动**，不落库，不碰 `rate_snapshots`。

### 前端

| 文件 | 内容 |
|---|---|
| `frontend/lib/api-types.ts` | `UpstreamCatalogGroup/Model/Catalog/Response` 类型 |
| `frontend/lib/queries.ts` | `useUpstreamCatalog(channelID, enabled)` 懒加载 hook |
| `frontend/components/monitor/channel-group-models.tsx`（新增） | `GroupModelsPanel`（内联 6 行）、`GroupModelsDialog`（全量弹层）、`mergeGroupViews`、倍率/计费格式化 |
| `frontend/components/monitor/channel-cards.tsx` | `InlineRates` 改造：chip 可点击、选中态、懒加载目录、插入展开区与弹层 |

### 核心行为

1. **点分组 chip 展开该分组的模型**（最多 6 行），再点收起，点另一个自动切换
2. **「查看全部 N 个」打开弹层**：搜索（模型名/厂商/标签）、排序（默认/倍率/名称）、分页 20/页
3. **分组倍率 / 模型倍率分开显示**，不互相相乘（实测相乘会差 20 倍）
4. **未授权分组**：chip 橙点 + 倍率显示 `?`，展开标「未在授权列表」
5. **无倍率分组**：显示 `—` 而不是 `0`（`0` 会被误读成免费）
6. **阶梯计费**（码少）：倍率列显示「表达式」，计费列给摘要（`入×4 · 出×20` / `$0.03/次` / `$0.02/秒`），原文放 tooltip
7. **跨组模型**：行尾 `+N 组` 角标，hover/tooltip 列出其他分组
8. **上游独有分组**：本地快照没有但上游有的分组（如码少的 `码少专用`），自动补进 chip 列表并标「N 个未同步」
9. **降级**：`auth_expired` / `fetch_failed` / `no_pricing_api` 三态，各自给出可操作提示 + 重试

## 二、使用方式

### 前端

1. 打开监控页（渠道卡片列表）
2. 找到 WorldClawPro / xem / 码少 等 NewAPI 渠道
3. 点「N 个分组」里的任意分组 → 下方展开该分组的模型
4. 点「查看全部 N 个」→ 全量弹层，可搜索排序翻页
5. 卡片级「展开」（右上角）仍然只控制 chip 区高度，与模型区互不影响

### 接口

```bash
# 需要管理端鉴权
GET /api/channels/14/upstream-catalog          # 走 5 分钟缓存
GET /api/channels/14/upstream-catalog?fresh=1  # 强制刷新
```

响应（`apiFetch` 会自动解包 `data`）：

```json
{
  "capability": "group_models",
  "degrade": "",
  "groups": [{"name":"claude","ratio":4.55,"has_ratio":true,"authorized":true,"model_count":14}],
  "models": [{"name":"claude-opus-4-6","groups":["claude","claude-max"],"model_ratio":2.5,
              "completion_ratio":5,"vendor":"Anthropic","endpoints":["anthropic"]}]
}
```

## 三、验证记录

| 项目 | 结果 |
|---|---|
| `go build ./...` | ✅ |
| `go test ./...`（全部包） | ✅ |
| `npx tsc --noEmit` | ✅ |
| `npx eslint`（改动文件） | ✅ |
| `npm run build` | ✅（既有 chunk 体积警告与本次无关） |
| 接口实测 WorldClawPro / xem / 码少 | ✅ 3 个渠道全部 200，数据与上游抓包一致 |
| 浏览器端到端 | ✅ 点 chip 展开、弹层搜索分页、未授权标记、阶梯计费、上游独有分组补全 |

### 测试中发现并修复的 3 个真实缺陷

1. **`trimZero("20")` 返回 `"2"`** —— 补全倍率 `出×20` 被截成 `出×2`。修正为只在含小数点时裁尾零。
2. **未授权分组计数恒为 0** —— 原逻辑「有倍率就不算未授权」是错的，部分上游 `group_ratio` 会带上全部分组。改为只以 `usable_group` 判定授权。
3. **`apiFetch` 自动解包 `{data}`** —— hook 里多取了一层 `.data` 导致 `catalog` 为 `null`，展开区空白。已修正并写明注释。

## 四、后续可迭代方向

| 优先级 | 内容 |
|---|---|
| P1 | **Sub2API 支持**：其 `/api/v1/channels/available` 有「平台 → {分组, 模型}」，虽无分组级映射，但可做「平台池模型」视图，需新增 `platform_pool` 的 UI 分支 |
| P1 | **弹层内列配置**：可显示/隐藏代理列（缓存倍率、按次价、协议），当前详情列是固定的 |
| P2 | **本地快照反哺**：用上游实时分组清理 `rate_snapshots` 里上游已删除的分组（实测本地 33 个 vs 上游 1 个这类严重不一致） |
| P2 | **分组维度筛选**：按 `capability` 批量展开某渠道所有分组的模型数概览 |
| P2 | **价格变化提示**：对比 `pricing_version`，版本变化时在 chip 上打角标，提醒分组价格已更新 |
| P3 | **导出**：分组内模型清单导出 CSV |

## 五、已知限制

1. **只支持 NewAPI**（`capability=group_models`）。Sub2API 会显示「该站点类型不提供分组级模型对应关系」。
2. **`usable_group` 是账号视角**：同一站点换账号登录，看到的授权分组会不同。
3. **缓存 5 分钟**：上游改价后最多 5 分钟才反映；`?fresh=1` 可立即刷新。
4. **不做参数规模/上下文窗口**：上游接口不返回，硬编码必然过期（详见设计文档 §3）。
5. **`billing_expr` 解析是尽力而为**：无法识别的表达式显示「自定义表达式」，原文仍可在 tooltip 查看。
