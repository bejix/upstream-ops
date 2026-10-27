"use client"

/**
 * 渠道分组内的上游模型视图。
 *
 * 两个层次：
 *   GroupModelsPanel  —— 卡片内联展开区，最多 6 行，回答「这组有没有某个模型」
 *   GroupModelsDialog —— 全量弹层，搜索/排序/分页，看完整清单
 *
 * 关键约定（来自上游实测，见 docs/upstream-group-models-design.md）：
 *   - rate_snapshots.model_name 存的是「分组名」，本组件展示的才是真正的模型
 *   - model_ratio 已是展示口径，不要再乘分组倍率
 *   - usable_group 与「模型引用的分组」可能不一致：未授权分组没倍率也没描述，要显式标注
 *   - 部分上游（如码少）用 billing_expr 表达式计费，model_ratio 为 0，需展示摘要
 */

import { useMemo, useState } from "react"
import { AlertTriangle, ChevronLeft, ChevronRight, Loader2, RotateCw, Search, X } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { useUpstreamCatalog } from "@/lib/queries"
import type {
  Channel,
  RateSnapshot,
  UpstreamCatalog,
  UpstreamCatalogGroup,
  UpstreamCatalogModel,
} from "@/lib/api-types"

/** 内联区最多展示的模型行数；超过则引导去弹层 */
const INLINE_LIMIT = 6
/** 弹层每页行数 */
const DIALOG_PAGE_SIZE = 20

type SortMode = "group-order" | "name" | "ratio"

/** 一个「上游分组 + 它在本地的快照」的组合，供 chip 与内联区共用。 */
export interface GroupView {
  /** 上游分组名（= RateSnapshot.model_name） */
  name: string
  /** 本地快照；上游没有该分组时为 undefined */
  snapshot?: RateSnapshot
  /** 上游目录里的分组；拉取失败或尚未加载时为 undefined */
  catalog?: UpstreamCatalogGroup
}

/**
 * 合并本地快照与上游目录，得到要展示的分组列表。
 *
 * - 以本地快照为主：chip 本来就来自快照，保持卡片原有顺序与内容不变
 * - 上游多出来的分组（本地没同步到）追加在末尾，避免「明明有却看不到」
 * - 目录还没加载完时，仍然只返回本地快照，界面不跳动
 */
export function mergeGroupViews(
  rates: RateSnapshot[],
  catalog?: UpstreamCatalog | null,
): GroupView[] {
  const byName = new Map<string, GroupView>()
  for (const r of rates) {
    byName.set(r.model_name, { name: r.model_name, snapshot: r })
  }
  if (catalog && catalog.capability === "group_models") {
    for (const g of catalog.groups) {
      const existing = byName.get(g.name)
      if (existing) {
        existing.catalog = g
      } else {
        byName.set(g.name, { name: g.name, catalog: g })
      }
    }
  }
  // 本地快照的顺序由调用方决定（已按倍率排序），这里保持插入顺序即可。
  return [...byName.values()]
}

/** 取某分组下的模型；未绑定任何分组的模型统一归到「未分组」 */
export function modelsOfGroup(catalog: UpstreamCatalog | null | undefined, group: string): UpstreamCatalogModel[] {
  if (!catalog) return []
  return catalog.models.filter((m) => (m.groups ?? []).includes(group))
}

/** 倍率展示：整数不带小数，小数最多 4 位，避免 0.04150000000000001 这类噪声 */
export function formatRatioValue(v: number): string {
  if (!Number.isFinite(v)) return "—"
  if (Number.isInteger(v)) return String(v)
  return String(Number(v.toFixed(4)))
}

/** 补全倍率：倍率语义，前面加 ×；解析不了时返回 null 由调用方回退 */
export function formatCompletion(v: number | undefined | null): string | null {
  if (v == null || !Number.isFinite(v)) return null
  if (Math.abs(v - Math.round(v)) < 1e-9) return `×${Math.round(v)}`
  return `×${Number(v.toFixed(2))}`
}

/** 是否按次/图片等「非倍率」方式计费 */
export function isFixedPriceBilling(m: UpstreamCatalogModel): boolean {
  const tiered = Boolean(m.billing_expr) || m.billing_mode === "tiered_expr"
  if (tiered) return false
  return (m.quota_type ?? 0) !== 0 && (m.model_price ?? 0) > 0
}

/**
 * 计费方式的展示文案。
 * 优先级：表达式 > 按次 > 按倍率；表达式优先是因为它的 model_ratio 通常是 0，
 * 若显示倍率会误导成「免费」。
 */
export function billingLabel(m: UpstreamCatalogModel): {
  kind: "expr" | "fixed" | "ratio"
  short: string
  detail?: string
} {
  if (m.billing_expr || m.billing_mode === "tiered_expr") {
    return {
      kind: "expr",
      short: "阶梯",
      detail: m.billing_brief || "自定义表达式",
    }
  }
  if (isFixedPriceBilling(m)) {
    return { kind: "fixed", short: `按次 $${formatRatioValue(m.model_price)}` }
  }
  return { kind: "ratio", short: "按倍率" }
}

/**
 * 分组倍率的展示文案。
 *
 * has_ratio=false 表示上游没给这个分组的倍率（常见于未授权分组，或该分组根本没配置倍率），
 * 此时不能显示 0 —— 0 会被读成「免费」，必须显示「—」并给出原因。
 */
export function formatGroupRatio(g: UpstreamCatalogGroup | undefined): {
  text: string
  unknown: boolean
} {
  if (!g) return { text: "—", unknown: true }
  if (!g.has_ratio) return { text: "—", unknown: true }
  return { text: formatRatioValue(g.ratio), unknown: false }
}

function degradedHint(degrade: string | undefined, message: string | undefined): string | null {
  switch (degrade) {
    case "auth_expired":
      return "该渠道登录已失效，请重新登录后再查看分组模型"
    case "no_pricing_api":
      return "该站点类型不提供「分组 → 模型」的对应关系，无法列出分组内模型"
    case "fetch_failed":
      return message?.trim() || "读取上游定价失败"
    case "":
    case undefined:
      return null
    default:
      return message?.trim() || null
  }
}

type InlineProps = {
  group: string
  /** 已在父组件拉到的目录，避免重复请求 */
  catalog: UpstreamCatalog | null
  loading: boolean
  error: string | null
  onOpenAll: () => void
  onRetry: () => void
}

/** 卡片内联展开区：信息条 + 最多 6 行模型 + 「查看全部」。 */
export function GroupModelsPanel({
  group,
  catalog,
  loading,
  error,
  onOpenAll,
  onRetry,
}: InlineProps) {
  const groupInfo = catalog?.groups.find((g) => g.name === group)
  const models = useMemo(() => modelsOfGroup(catalog, group), [catalog, group])
  const hint = error ? error : degradedHint(catalog?.degrade, catalog?.degrade_message)
  const groupRatio = formatGroupRatio(groupInfo)

  const header = (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 border-b border-border bg-muted/25 px-2.5 py-2">
      <span className="text-xs font-medium">{group}</span>
      {groupInfo?.description ? (
        <span className="max-w-[18rem] truncate text-[11px] text-muted-foreground" title={groupInfo.description}>
          {groupInfo.description}
        </span>
      ) : null}
      <span className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-1 text-[10px] text-muted-foreground">
        <span>
          分组倍率{" "}
          <span
            className={cn("font-medium", groupRatio.unknown ? "text-muted-foreground" : "text-foreground")}
            title={groupRatio.unknown ? "上游未提供该分组的倍率" : undefined}
          >
            {groupRatio.text}
          </span>
        </span>
        <span>
          模型 <span className="font-medium text-foreground">{models.length}</span>
        </span>
      </span>
    </div>
  )

  const flags = (
    <div className="flex flex-wrap gap-1 px-2.5 pb-1.5 pt-1.5">
      {groupInfo && !groupInfo.authorized ? (
        <Badge variant="outline" className="h-4 gap-1 px-1 text-[9px] font-normal text-warning ring-warning/40">
          <AlertTriangle className="size-2.5" /> 未在授权列表
        </Badge>
      ) : null}
      {groupInfo && groupInfo.authorized ? (
        <Badge variant="outline" className="h-4 px-1 text-[9px] font-normal">
          已授权
        </Badge>
      ) : null}
      {!groupInfo && !loading ? (
        <Badge variant="outline" className="h-4 px-1 text-[9px] font-normal text-warning ring-warning/40">
          上游无此分组
        </Badge>
      ) : null}
    </div>
  )

  let body: React.ReactNode
  if (loading) {
    body = (
      <div className="space-y-1.5 px-2.5 py-2.5">
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-3 animate-pulse rounded bg-muted" style={{ width: `${88 - i * 14}%` }} />
        ))}
      </div>
    )
  } else if (hint) {
    body = (
      <div className="space-y-2 px-2.5 py-2.5">
        <p className="text-[11px] leading-4 text-muted-foreground">{hint}</p>
        <Button size="sm" variant="outline" className="h-6 px-2 text-[11px]" onClick={onRetry}>
          <RotateCw className="size-3" /> 重试
        </Button>
      </div>
    )
  } else if (models.length === 0) {
    body = (
      <p className="px-2.5 py-3 text-[11px] leading-4 text-muted-foreground">
        {groupInfo
          ? "该分组当前没有可售模型。上游定价接口只返回在售模型，已下架的不会出现在这里。"
          : "该分组未出现在上游定价列表中，可能已停用。"}
      </p>
    )
  } else {
    const shown = models.slice(0, INLINE_LIMIT)
    body = (
      <>
        <table className="w-full table-fixed border-collapse text-[11px]">
          <colgroup>
            <col style={{ width: "46%" }} />
            <col style={{ width: "17%" }} />
            <col style={{ width: "14%" }} />
            <col style={{ width: "23%" }} />
          </colgroup>
          <tbody>
            {shown.map((m) => {
              const bill = billingLabel(m)
              const comp = formatCompletion(m.completion_ratio)
              return (
                <tr key={m.name} className="border-b border-border/50 last:border-0">
                  <td className="py-1 pl-2.5 pr-1">
                    <div className="flex min-w-0 items-center gap-1">
                      <span className="truncate font-mono" title={m.name}>
                        {m.name}
                      </span>
                      {(m.groups?.length ?? 0) > 1 ? (
                        <Tooltip delayDuration={200}>
                          <TooltipTrigger asChild>
                            <span className="shrink-0 cursor-default rounded border border-brand/40 px-1 text-[9px] leading-3 text-brand">
                              +{(m.groups?.length ?? 1) - 1} 组
                            </span>
                          </TooltipTrigger>
                          <TooltipContent side="top" className="max-w-xs text-xs">
                            也属于：{(m.groups ?? []).filter((g) => g !== group).join(" / ")}
                          </TooltipContent>
                        </Tooltip>
                      ) : null}
                    </div>
                    {m.vendor ? (
                      <div className="truncate text-[10px] leading-3 text-muted-foreground">{m.vendor}</div>
                    ) : null}
                  </td>
                  <td className="py-1 pr-1 text-right tabular-nums">
                    {bill.kind === "ratio" ? formatRatioValue(m.model_ratio) : <span className="text-muted-foreground">—</span>}
                  </td>
                  <td className="py-1 pr-1 text-right tabular-nums text-muted-foreground">{comp ?? "—"}</td>
                  <td className="py-1 pr-2.5">
                    {bill.kind === "expr" ? (
                      <Tooltip delayDuration={200}>
                        <TooltipTrigger asChild>
                          <div className="cursor-default">
                            <span className="rounded bg-warning/10 px-1 text-[9px] leading-4 text-warning ring-1 ring-inset ring-warning/25">
                              {bill.short}
                            </span>
                            <div className="truncate text-[10px] leading-3 text-muted-foreground">{bill.detail}</div>
                          </div>
                        </TooltipTrigger>
                        <TooltipContent side="top" className="max-w-sm break-all font-mono text-[11px]">
                          {m.billing_expr}
                        </TooltipContent>
                      </Tooltip>
                    ) : (
                      <span className="text-muted-foreground">{bill.short}</span>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
        <div className="flex items-center gap-2 border-t border-border px-2.5 py-1.5 text-[10px] text-muted-foreground">
          <span>
            显示 {shown.length} / {models.length} 个
          </span>
          {models.length > INLINE_LIMIT ? (
            <button
              type="button"
              onClick={onOpenAll}
              className="ml-auto inline-flex items-center gap-0.5 text-brand hover:underline"
            >
              查看全部 {models.length} 个
              <ChevronRight className="size-3" />
            </button>
          ) : null}
        </div>
      </>
    )
  }

  return (
    // 浮层样式：必须是不透明的 card 背景 + 阴影，否则会透出下方的按钮组和相邻卡片。
    // max-h + overflow-auto 兜住极端情况（某分组几十个模型时内联区也不至于冲出屏幕）。
    <div className="max-h-[min(60vh,26rem)] overflow-auto rounded-md border border-brand/40 bg-card shadow-lg ring-1 ring-black/5 dark:ring-white/10">
      {header}
      {flags}
      {body}
    </div>
  )
}

type DialogProps = {
  channel: Channel
  group: string | null
  catalog: UpstreamCatalog | null
  loading: boolean
  error: string | null
  open: boolean
  onOpenChange: (v: boolean) => void
  onRetry: () => void
}

/** 全量弹层：搜索、排序、分页。 */
export function GroupModelsDialog({
  channel,
  group,
  catalog,
  loading,
  error,
  open,
  onOpenChange,
  onRetry,
}: DialogProps) {
  const [query, setQuery] = useState("")
  const [sort, setSort] = useState<SortMode>("group-order")
  const [page, setPage] = useState(1)

  const all = useMemo(() => {
    const list = modelsOfGroup(catalog, group ?? "")
    const q = query.trim().toLowerCase()
    const filtered = q
      ? list.filter(
          (m) =>
            m.name.toLowerCase().includes(q) ||
            (m.vendor ?? "").toLowerCase().includes(q) ||
            (m.tags ?? []).some((t) => t.toLowerCase().includes(q)),
        )
      : list
    const out = [...filtered]
    if (sort === "name") out.sort((a, b) => a.name.localeCompare(b.name))
    else if (sort === "ratio") out.sort((a, b) => b.model_ratio - a.model_ratio)
    return out
  }, [catalog, group, query, sort])

  const pages = Math.max(1, Math.ceil(all.length / DIALOG_PAGE_SIZE))
  const current = Math.min(page, pages)
  const rows = all.slice((current - 1) * DIALOG_PAGE_SIZE, current * DIALOG_PAGE_SIZE)
  const groupInfo = catalog?.groups.find((g) => g.name === group)
  const hint = error ? error : degradedHint(catalog?.degrade, catalog?.degrade_message)

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) {
          setQuery("")
          setPage(1)
          setSort("group-order")
        }
      }}
    >
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="text-base font-medium">{group ?? "分组模型"}</DialogTitle>
          <DialogDescription className="text-xs">
            {channel.name}
            {groupInfo?.description ? ` · ${groupInfo.description}` : ""}
            {groupInfo ? ` · 分组倍率 ${formatGroupRatio(groupInfo).text}` : ""}
            {all.length ? ` · ${all.length} 个模型` : ""}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <div className="relative min-w-0 flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(e) => {
                setQuery(e.target.value)
                setPage(1)
              }}
              placeholder="搜索模型名 / 厂商 / 标签"
              className="h-9 pl-8 text-xs"
            />
            {query ? (
              <button
                type="button"
                aria-label="清空搜索"
                onClick={() => {
                  setQuery("")
                  setPage(1)
                }}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="size-3.5" />
              </button>
            ) : null}
          </div>
          <div className="flex gap-1">
            {(
              [
                ["group-order", "默认"],
                ["ratio", "按倍率"],
                ["name", "按名称"],
              ] as const
            ).map(([key, label]) => (
              <Button
                key={key}
                size="sm"
                variant={sort === key ? "default" : "outline"}
                className="h-9 px-2.5 text-xs"
                onClick={() => {
                  setSort(key)
                  setPage(1)
                }}
              >
                {label}
              </Button>
            ))}
          </div>
        </div>

        {groupInfo && !groupInfo.authorized ? (
          <p className="flex items-start gap-1.5 rounded-md border border-warning/40 bg-warning/[0.06] px-2.5 py-1.5 text-[11px] leading-4 text-warning">
            <AlertTriangle className="mt-0.5 size-3 shrink-0" />
            该分组被上游模型引用，但不在当前账号的可用分组列表里，大概率无法调用。
          </p>
        ) : null}
        {catalog?.notes?.length ? (
          <p className="text-[11px] leading-4 text-muted-foreground">{catalog.notes.join("；")}</p>
        ) : null}

        <ScrollArea className="h-[52vh] rounded-md border">
          <Table className="text-xs">
            <TableHeader>
              <TableRow>
                <TableHead className="h-9 font-medium text-muted-foreground">模型</TableHead>
                <TableHead className="hidden h-9 font-medium text-muted-foreground sm:table-cell">厂商</TableHead>
                <TableHead className="hidden h-9 font-medium text-muted-foreground md:table-cell">标签</TableHead>
                <TableHead className="h-9 text-right font-medium text-muted-foreground">倍率</TableHead>
                <TableHead className="h-9 text-right font-medium text-muted-foreground">补全</TableHead>
                <TableHead className="h-9 font-medium text-muted-foreground">计费</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                    <Loader2 className="mx-auto mb-1 size-4 animate-spin" />
                    加载中…
                  </TableCell>
                </TableRow>
              ) : hint ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                    <p className="mb-2">{hint}</p>
                    <Button size="sm" variant="outline" className="h-7 text-xs" onClick={onRetry}>
                      <RotateCw className="size-3" /> 重试
                    </Button>
                  </TableCell>
                </TableRow>
              ) : rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                    {query ? "没有匹配的模型" : "该分组当前没有可售模型"}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((m) => {
                  const bill = billingLabel(m)
                  const comp = formatCompletion(m.completion_ratio)
                  return (
                    <TableRow key={m.name}>
                      <TableCell className="min-w-40 whitespace-normal">
                        <div className="break-all font-mono">{m.name}</div>
                        {(m.groups?.length ?? 0) > 1 ? (
                          <div className="mt-0.5 text-[10px] text-brand">
                            也属于 {(m.groups ?? []).filter((g) => g !== group).join(" / ")}
                          </div>
                        ) : null}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground sm:table-cell">
                        {m.vendor || "—"}
                      </TableCell>
                      <TableCell className="hidden max-w-40 whitespace-normal text-[10px] text-muted-foreground md:table-cell">
                        {(m.tags ?? []).join(" · ") || "—"}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {bill.kind === "ratio" ? formatRatioValue(m.model_ratio) : "—"}
                      </TableCell>
                      <TableCell className="text-right tabular-nums text-muted-foreground">
                        {comp ?? "—"}
                      </TableCell>
                      <TableCell>
                        {bill.kind === "expr" ? (
                          <Tooltip delayDuration={200}>
                            <TooltipTrigger asChild>
                              <span className="cursor-default">
                                <span className="rounded bg-warning/10 px-1 text-[9px] text-warning ring-1 ring-inset ring-warning/25">
                                  阶梯
                                </span>
                                <span className="ml-1 text-[10px] text-muted-foreground">{bill.detail}</span>
                              </span>
                            </TooltipTrigger>
                            <TooltipContent side="top" className="max-w-sm break-all font-mono text-[11px]">
                              {m.billing_expr}
                            </TooltipContent>
                          </Tooltip>
                        ) : (
                          <span className="text-muted-foreground">{bill.short}</span>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          </Table>
        </ScrollArea>

        <div className="flex items-center gap-2">
          <span className="text-[11px] text-muted-foreground">
            共 {all.length} 个{query ? "（已搜索）" : ""}
          </span>
          <div className="ml-auto flex items-center gap-1">
            <Button
              size="icon"
              variant="outline"
              className="size-7"
              disabled={current <= 1}
              onClick={() => setPage(current - 1)}
              aria-label="上一页"
            >
              <ChevronLeft className="size-3.5" />
            </Button>
            <span className="min-w-14 text-center text-[11px] tabular-nums text-muted-foreground">
              {current} / {pages}
            </span>
            <Button
              size="icon"
              variant="outline"
              className="size-7"
              disabled={current >= pages}
              onClick={() => setPage(current + 1)}
              aria-label="下一页"
            >
              <ChevronRight className="size-3.5" />
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

/**
 * 拉取目录的便捷 hook 包装：只在需要时（enabled）请求，并返回稳定的空值。
 * 单独抽出来是为了让 InlineRates 与详情弹层共享同一份数据。
 */
export function useGroupCatalog(channelID: number, enabled: boolean) {
  const query = useUpstreamCatalog(channelID, enabled)
  // apiFetch 已解包 { data }，query.data 直接就是 UpstreamCatalog。
  const catalog = query.data ?? null
  // 拉取失败时后端把错误放进 data.degrade_message，同时附带外层 error 字段；
  // 解包后外层丢失，因此优先用 degrade_message，再退回请求级 error。
  const error = catalog?.degrade_message || query.error || null
  return {
    catalog,
    loading: query.loading,
    error,
    refetch: query.refetch,
  }
}

export { INLINE_LIMIT, DIALOG_PAGE_SIZE }
export type { InlineProps, DialogProps }
