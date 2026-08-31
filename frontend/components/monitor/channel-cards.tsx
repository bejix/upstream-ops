"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { toast } from "sonner"
import {
  ArrowUpDown,
  ArrowRightLeft,
  CheckCircle2,
  ChevronDown,
  CreditCard,
  ExternalLink,
  KeyRound,
  Loader2,
  LogIn,
  MoreHorizontal,
  Pause,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Search,
  Tags,
  Trash2,
  Gift,
  ChevronsLeft,
  ChevronsRight,
  XCircle,
} from "lucide-react"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { useChannels, useChannelsPage, useChannelRates } from "@/lib/queries"
import { apiFetch } from "@/lib/api"
import { useTriggerRefresh } from "@/lib/refresh-context"
import { channelTypeLabel, decimal, formatRatio, money, relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { syncAllChannelsStream, syncChannelStream, testLoginStream, type ProgressEvent } from "@/lib/sync-stream"
import type { Channel, ChannelRedeemResult, RateSnapshot } from "@/lib/api-types"
import { ChannelFormDialog } from "@/components/monitor/channel-form-dialog"
import { ChannelRedeemDialog } from "@/components/monitor/channel-redeem-dialog"
import { ChannelRechargeDialog } from "@/components/monitor/channel-recharge-dialog"
import {
  ChannelAPIKeysDialog,
  type ChannelAPIKeyInitialAction,
} from "@/components/monitor/channel-api-keys-dialog"
import {
  ChannelSubscriptionUsageMetricTiles,
} from "@/components/monitor/channel-subscription-usage-dialog"

type Status = "healthy" | "low" | "failed" | "idle"
type ChannelPageSize = 9 | 18 | 36 | 72 | 81 | "all"
type GroupSortMode = "channel-asc" | "channel-desc" | "ratio-asc" | "ratio-desc"

const channelPageSizeOptions: ChannelPageSize[] = [9, 18, 36, 72, 81, "all"]
const CHANNEL_PAGE_SIZE_STORAGE_KEY = "upstream-ops:channel-page-size"

function storedChannelPageSize(): ChannelPageSize {
  if (typeof window === "undefined") return 9
  const value = window.localStorage.getItem(CHANNEL_PAGE_SIZE_STORAGE_KEY)
  if (value === "all") return value
  const parsed = Number(value)
  return channelPageSizeOptions.includes(parsed as ChannelPageSize) ? parsed as ChannelPageSize : 9
}

function pageNumbers(currentPage: number, totalPages: number) {
  const first = Math.max(1, currentPage - 3)
  const last = Math.min(totalPages, currentPage + 3)
  return Array.from({ length: last - first + 1 }, (_, i) => first + i)
}

function statusOf(c: Channel): Status {
  if (c.last_error) return "failed"
  if (c.last_balance == null) return "idle"
  if (c.balance_threshold > 0 && c.last_balance < c.balance_threshold) return "low"
  return "healthy"
}

const statusMap: Record<Status, { label: string; cls: string }> = {
  healthy: { label: "健康", cls: "text-success bg-success/10" },
  low: { label: "低余额", cls: "text-warning bg-warning/10" },
  failed: { label: "登录失败", cls: "text-danger bg-danger/10" },
  idle: { label: "尚未采集", cls: "text-muted-foreground bg-muted/40" },
}

function StatTile({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex h-16 min-w-0 flex-col justify-between rounded-md border border-border bg-muted/20 px-2.5 py-2">
      <span className="text-[10px] leading-none text-muted-foreground">{label}</span>
      <div className="min-w-0 overflow-hidden text-[13px] font-semibold leading-tight text-foreground">
        {typeof children === "string" ? <span className="block truncate">{children}</span> : children}
      </div>
    </div>
  )
}

function rechargeMultiplierTip(c: Channel) {
  const mode = c.recharge_multiplier_mode === "multiply" ? "余额 × 倍率" : "余额 / 倍率"
  if (c.recharge_multiplier != null && c.recharge_multiplier > 0) {
    return `充值倍率：${decimal(c.recharge_multiplier, 4)}（${mode}）`
  }
  return `充值倍率：跟随上游（${mode}）`
}

/** ratioTone 按倍率给 chip 上色，与 ChannelRatesPanel 共用同一套规则。 */
function ratioTone(r: number): string {
  if (r <= 0.8) return "bg-success/10 text-success ring-success/20"
  if (r > 2) return "bg-danger/10 text-danger ring-danger/20"
  if (r > 1.2) return "bg-warning/10 text-warning ring-warning/20"
  return "bg-muted text-foreground ring-border"
}

/** InlineRates 在渠道卡片内部展示当前所有分组倍率，默认 2 行折叠 + 展开按钮。 */
function InlineRates({ channel }: { channel: Channel }) {
  const { data, loading } = useChannelRates(channel.id, channel.only_created_key_groups_enabled)
  const rates = [...(data ?? [])].sort((a, b) => a.ratio - b.ratio)
  const [expanded, setExpanded] = useState(false)
  const [hasOverflow, setHasOverflow] = useState(false)
  const chipBoxRef = useRef<HTMLDivElement>(null)

  // 监听 chip 容器尺寸变化，决定是否要显示"展开"按钮。
  // 收起状态下 scrollHeight > clientHeight 表示有内容被裁剪。
  useEffect(() => {
    const el = chipBoxRef.current
    if (!el) return
    const check = () => {
      if (expanded) return
      setHasOverflow(el.scrollHeight > el.clientHeight + 1)
    }
    check()
    const ro = new ResizeObserver(check)
    ro.observe(el)
    return () => ro.disconnect()
  }, [rates.length, expanded])

  if (loading) return null
  if (rates.length === 0) return null

  const showToggle = hasOverflow || expanded

  return (
    <div className="mt-3 border-t border-border pt-2.5">
      <div className="mb-1.5 flex items-center justify-between">
        <p className="text-[11px] text-muted-foreground">
          {rates.length} 个分组
        </p>
        {showToggle ? (
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="inline-flex items-center gap-0.5 text-[11px] text-muted-foreground hover:text-foreground"
          >
            {expanded ? "收起" : "展开"}
            <ChevronDown
              className={cn(
                "size-3 transition-transform duration-200",
                expanded && "rotate-180",
              )}
            />
          </button>
        ) : null}
      </div>

      <div className="relative min-h-16">
        <div
          ref={chipBoxRef}
          className={cn(
            "flex flex-wrap gap-1 overflow-hidden transition-[max-height] duration-300 ease-out",
            // 收起：max-h-12 (~48px) 约 2 行；展开：足够大的上限，留点缓冲让 transition 不立即消失。
            expanded ? "max-h-150" : "max-h-12",
          )}
        >
          {rates.map((r) => (
            <Tooltip key={r.id} delayDuration={150}>
              <TooltipTrigger asChild>
                <span
                  className={cn(
                    "inline-flex cursor-default items-center gap-1 rounded px-1.5 py-0.5 text-[11px] ring-1 ring-inset transition-colors hover:bg-muted/60",
                    ratioTone(r.ratio),
                  )}
                >
                  <span className="font-medium">{r.model_name}</span>
                  <span className="rounded bg-primary/10 px-1 font-semibold tabular-nums text-primary ring-1 ring-inset ring-primary/15">
                    {formatRatio(r.ratio)}
                  </span>
                </span>
              </TooltipTrigger>
              <TooltipContent side="top" className="max-w-xs text-xs">
                <p className="font-medium">{r.model_name}</p>
                {r.description ? (
                  <p className="mt-0.5 text-muted-foreground">{r.description}</p>
                ) : (
                  <p className="mt-0.5 italic text-muted-foreground">{"(无描述)"}</p>
                )}
                <p className="mt-0.5 text-muted-foreground">
                  {"最近更新："}
                  {relativeTime(r.last_seen_at)}
                </p>
              </TooltipContent>
            </Tooltip>
          ))}
        </div>
        {/* 折叠时底部淡出，提示还有更多内容 */}
        {!expanded && hasOverflow ? (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 h-4 bg-linear-to-t from-background to-transparent" />
        ) : null}
      </div>
    </div>
  )
}

interface ChannelGroupRow {
  key: string
  channel: Channel
  rate: RateSnapshot
}

interface ChannelAPIKeyDialogState {
  channel: Channel
  initialAction?: ChannelAPIKeyInitialAction
}

function ChannelGroupsDialog({
  open,
  onOpenChange,
  channels,
  onAPIKeyAction,
  nestedDialogOpen,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  channels: Channel[]
  onAPIKeyAction: (channel: Channel, rate: RateSnapshot, type: "create" | "migrate") => void
  nestedDialogOpen: boolean
}) {
  const [query, setQuery] = useState("")
  const [sortMode, setSortMode] = useState<GroupSortMode>("channel-asc")
  const [rows, setRows] = useState<ChannelGroupRow[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dialogContentElement, setDialogContentElement] = useState<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true)
    setError(null)

    Promise.all(
      channels.map(async (channel) => {
        const rates = await apiFetch<RateSnapshot[]>(
          `/channels/${channel.id}/rates${channel.only_created_key_groups_enabled ? "?only_with_keys=1" : ""}`,
        )
        return { channel, rates }
      }),
    )
      .then((result) => {
        if (cancelled) return
        setRows(
          result.flatMap(({ channel, rates }) =>
            rates.map((rate) => ({
              key: `${channel.id}-${rate.id}`,
              channel,
              rate,
            })),
          ),
        )
      })
      .catch((e: Error) => {
        if (!cancelled) setError(e.message || "加载分组失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [open, channels])

  const filteredRows = useMemo(() => {
    const q = query.trim().toLowerCase()
    return rows
      .filter(({ channel, rate }) => {
        if (!q) return true
        return [
          channel.name,
          channelTypeLabel(channel.type),
          rate.model_name,
          rate.description ?? "",
          formatRatio(rate.ratio),
        ].some((value) => value.toLowerCase().includes(q))
      })
      .sort((a, b) => {
        if (sortMode === "ratio-asc" || sortMode === "ratio-desc") {
          const diff = a.rate.ratio - b.rate.ratio
          return sortMode === "ratio-asc" ? diff : -diff
        }
        const diff = a.channel.name.localeCompare(b.channel.name, "zh-CN")
          || a.rate.model_name.localeCompare(b.rate.model_name, "zh-CN")
        return sortMode === "channel-asc" ? diff : -diff
      })
  }, [query, rows, sortMode])

  const channelCount = new Set(rows.map((row) => row.channel.id)).size

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        ref={setDialogContentElement}
        className="sm:max-w-4xl"
        onInteractOutside={(event) => {
          if (nestedDialogOpen) event.preventDefault()
        }}
        onEscapeKeyDown={(event) => {
          if (nestedDialogOpen) event.preventDefault()
        }}
      >
        <DialogHeader>
          <DialogTitle className="text-base font-medium">{"分组"}</DialogTitle>
          <DialogDescription className="text-xs">
            {loading ? "正在加载全部渠道分组" : `${rows.length} 个分组 · ${channelCount} 个渠道`}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <div className="relative min-w-0 flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索渠道、分组或倍率"
              className="h-9 pl-8 text-xs"
            />
          </div>
          <Select value={sortMode} onValueChange={(value) => setSortMode(value as GroupSortMode)}>
            <SelectTrigger className="h-9 w-full gap-2 text-xs sm:w-40">
              <ArrowUpDown className="size-4 text-muted-foreground" />
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="end">
              <SelectItem value="channel-asc">{"渠道 A-Z"}</SelectItem>
              <SelectItem value="channel-desc">{"渠道 Z-A"}</SelectItem>
              <SelectItem value="ratio-asc">{"倍率从低到高"}</SelectItem>
              <SelectItem value="ratio-desc">{"倍率从高到低"}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <ScrollArea className="h-[60vh] rounded-md border">
          <Table className="text-xs">
            <TableHeader>
              <TableRow>
                <TableHead className="h-9 font-medium text-muted-foreground">{"渠道"}</TableHead>
                <TableHead className="hidden h-9 font-medium text-muted-foreground sm:table-cell">{"类型"}</TableHead>
                <TableHead className="h-9 font-medium text-muted-foreground">{"分组"}</TableHead>
                <TableHead className="h-9 text-right font-medium text-muted-foreground">{"倍率"}</TableHead>
                <TableHead className="hidden h-9 text-right font-medium text-muted-foreground sm:table-cell">{"更新"}</TableHead>
                <TableHead className="h-9 text-right font-medium text-muted-foreground">{"操作"}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-24 text-center text-xs text-muted-foreground">
                    {"加载中…"}
                  </TableCell>
                </TableRow>
              ) : error ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-24 text-center text-xs text-danger">
                    {error}
                  </TableCell>
                </TableRow>
              ) : filteredRows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-24 text-center text-xs text-muted-foreground">
                    {rows.length === 0 ? "暂无分组数据" : "没有匹配的分组"}
                  </TableCell>
                </TableRow>
              ) : (
                filteredRows.map(({ key, channel, rate }) => (
                  <TableRow key={key}>
                    <TableCell>{channel.name}</TableCell>
                    <TableCell className="hidden sm:table-cell">
                      <span
                        className={cn(
                          "inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-normal ring-1 ring-inset",
                          channel.type === "newapi"
                            ? "bg-brand/10 text-brand ring-brand/20"
                            : "bg-sky-500/10 text-sky-700 ring-sky-500/25 dark:text-sky-300",
                        )}
                      >
                        {channelTypeLabel(channel.type)}
                      </span>
                    </TableCell>
                    <TableCell className="min-w-40 whitespace-normal sm:min-w-60">
                      <div>{rate.model_name}</div>
                      {rate.description ? (
                        <div className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">
                          {rate.description}
                        </div>
                      ) : null}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      <span className={cn("rounded px-1.5 py-0.5 ring-1 ring-inset", ratioTone(rate.ratio))}>
                        {formatRatio(rate.ratio)}
                      </span>
                    </TableCell>
                    <TableCell className="hidden text-right text-muted-foreground sm:table-cell">
                      {relativeTime(rate.last_seen_at)}
                    </TableCell>
                    <TableCell className="text-right">
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="touch-manipulation"
                            aria-label="API Key 操作"
                            title="API Key 操作"
                          >
                            <KeyRound className="size-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent
                          portalContainer={dialogContentElement}
                          align="end"
                          sideOffset={6}
                          className="z-[70]"
                          onCloseAutoFocus={(event) => event.preventDefault()}
                        >
                          <DropdownMenuItem className="cursor-pointer py-2" onSelect={() => onAPIKeyAction(channel, rate, "create")}>
                            <Plus className="size-4" />
                            新建到此分组
                          </DropdownMenuItem>
                          <DropdownMenuItem className="cursor-pointer py-2" onSelect={() => onAPIKeyAction(channel, rate, "migrate")}>
                            <ArrowRightLeft className="size-4" />
                            将现有 API Key 移到此分组
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}

interface ChannelSyncState {
  running: boolean
  events: ProgressEvent[]
  latest: ProgressEvent | null
  finalOk: boolean | null
  fading: boolean
}

function emptySyncState(): ChannelSyncState {
  return { running: false, events: [], latest: null, finalOk: null, fading: false }
}

interface BulkSyncState {
  running: boolean
  completed: number
  total: number
}

const stageLabel: Record<ProgressEvent["stage"], string> = {
  captcha: "打码",
  session: "会话",
  login: "登录",
  balance: "余额",
  cost: "消费",
  subscription: "订阅",
  rates: "倍率",
  done: "完成",
  error: "失败",
}

const stageOrder: Record<ProgressEvent["stage"], number> = {
  captcha: 1,
  session: 2,
  login: 3,
  balance: 4,
  cost: 5,
  subscription: 6,
  rates: 7,
  done: 9,
  error: 9,
}

/** 按 stage 去重，每个 stage 只留最后一条事件（"在做中→完成"会被覆盖成完成态）。 */
function deriveSteps(events: ProgressEvent[]): ProgressEvent[] {
  const byStage = new Map<ProgressEvent["stage"], ProgressEvent>()
  for (const ev of events) byStage.set(ev.stage, ev)
  return [...byStage.values()].sort((a, b) => stageOrder[a.stage] - stageOrder[b.stage])
}

function SyncProgressStrip({ state }: { state: ChannelSyncState }) {
  if (!state.running && state.latest == null) return null
  const steps = deriveSteps(state.events)

  return (
    <div
      className={cn(
        "mt-3 rounded-lg border border-border bg-muted/30 px-3 py-2.5",
        // 入场：上方滑入 + 淡入
        "animate-in fade-in slide-in-from-top-1 duration-300",
        // 出场：和 scheduleHide 里的 500ms 对齐
        "transition-all duration-500 ease-out",
        state.fading ? "-translate-y-0.5 opacity-0" : "opacity-100",
      )}
    >
      {steps.length === 0 ? (
        <div className="flex items-center gap-2 text-xs">
          <Loader2 className="size-3.5 shrink-0 animate-spin text-muted-foreground" />
          <span className="text-foreground/80">{"准备中…"}</span>
        </div>
      ) : (
        <ul className="space-y-1.5">
          {steps.map((ev) => {
            // 终止态：stage=done 或 error；显式 ok=true / false 也算
            const failed = ev.stage === "error" || ev.ok === false
            const succeeded = ev.stage === "done" || ev.ok === true
            const running = !failed && !succeeded
            const Icon = running ? Loader2 : failed ? XCircle : CheckCircle2
            const tone = running ? "text-muted-foreground" : failed ? "text-danger" : "text-success"
            return (
              <li
                key={ev.stage}
                className="flex items-start gap-2 text-xs animate-in fade-in duration-200"
              >
                <Icon
                  className={cn("size-3.5 shrink-0", tone, running && "animate-spin")}
                />
                <span className="w-9 shrink-0 text-[11px] text-muted-foreground">
                  {stageLabel[ev.stage]}
                </span>
                <div className="min-w-0 flex-1 overflow-x-auto">
                  <span
                    className={cn(
                      "block whitespace-pre-wrap",
                      failed ? "text-danger" : running ? "text-foreground/80" : "text-foreground",
                    )}
                  >
                    {ev.message}
                  </span>
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

export function ChannelCards() {
  const { data: channels, loading: channelsLoading } = useChannels()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<ChannelPageSize>(storedChannelPageSize)
  const pageQuery = useChannelsPage(page, pageSize === "all" ? -1 : pageSize)
  const refresh = useTriggerRefresh()
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [editing, setEditing] = useState<Channel | null>(null)
  const [creating, setCreating] = useState(false)
  const [groupsOpen, setGroupsOpen] = useState(false)
  const [redeeming, setRedeeming] = useState<Channel | null>(null)
  const [recharging, setRecharging] = useState<Channel | null>(null)
  const [apiKeyDialog, setAPIKeyDialog] = useState<ChannelAPIKeyDialogState | null>(null)
  const [busyAction, setBusyAction] = useState<string | null>(null)
  // 每个渠道当前 sync 进度（最新一条事件） + 历史事件
  const [syncState, setSyncState] = useState<Record<number, ChannelSyncState>>({})
  const [bulkSync, setBulkSync] = useState<BulkSyncState>({ running: false, completed: 0, total: 0 })
  const anySyncRunning = bulkSync.running || Object.values(syncState).some((s) => s.running)
  const channelPage = pageQuery.data
  const visibleChannels = channelPage?.items ?? []
  const totalChannels = channelPage?.total ?? 0
  const pageSizeAll = pageSize === "all"
  const totalPages = pageSizeAll ? 1 : (channelPage?.pages ?? 1)
  const currentPage = pageSizeAll ? 1 : Math.min(page, totalPages)
  const effectivePageSize = pageSizeAll ? Math.max(totalChannels, 1) : pageSize
  const rangeStart = totalChannels === 0 ? 0 : (currentPage - 1) * effectivePageSize + 1
  const rangeEnd = Math.min((currentPage - 1) * effectivePageSize + visibleChannels.length, totalChannels)
  const pagerNumbers = pageNumbers(currentPage, totalPages)

  // 成功后自动消失需要的两段定时器：先 5s 显示，再 500ms 过渡（与 strip 的 transition-opacity duration-500 对齐）。
  const hideTimers = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map())

  useEffect(() => {
    const timers = hideTimers.current
    return () => {
      timers.forEach((t) => clearTimeout(t))
      timers.clear()
    }
  }, [])

  useEffect(() => {
    setPage((prev) => Math.min(prev, totalPages))
  }, [totalPages])

  function clearHideTimer(id: number) {
    const t = hideTimers.current.get(id)
    if (t != null) {
      clearTimeout(t)
      hideTimers.current.delete(id)
    }
  }

  function scheduleHide(id: number) {
    clearHideTimer(id)
    const t1 = setTimeout(() => {
      patchSync(id, (prev) => ({ ...prev, fading: true }))
      const t2 = setTimeout(() => {
        setSyncState((s) => {
          const { [id]: _gone, ...rest } = s
          void _gone
          return rest
        })
        hideTimers.current.delete(id)
      }, 500)
      hideTimers.current.set(id, t2)
    }, 5000)
    hideTimers.current.set(id, t1)
  }

  function patchSync(id: number, fn: (prev: ChannelSyncState) => ChannelSyncState) {
    setSyncState((s) => ({ ...s, [id]: fn(s[id] ?? emptySyncState()) }))
  }

  async function startStream(channel: Channel, action: "sync" | "test-login") {
    clearHideTimer(channel.id)
    patchSync(channel.id, () => ({
      running: true,
      events: [],
      latest: null,
      finalOk: null,
      fading: false,
    }))
    let sawError = false
    const stream = action === "sync" ? syncChannelStream : testLoginStream
    try {
      await stream(channel.id, {
        onEvent: (ev) => {
          if (ev.stage === "error" || ev.ok === false) sawError = true
          patchSync(channel.id, (prev) => ({
            ...prev,
            events: [...prev.events, ev],
            latest: ev,
          }))
        },
      })
      const ok = !sawError
      patchSync(channel.id, (prev) => ({
        ...prev,
        running: false,
        finalOk: ok,
      }))
      if (ok) scheduleHide(channel.id)
    } catch (e) {
      const err = e as Error
      const failureLabel = action === "sync" ? "同步失败" : "测试登录失败"
      patchSync(channel.id, (prev) => ({
        ...prev,
        running: false,
        finalOk: false,
        latest: {
          stage: "error",
          message: err.message || failureLabel,
          time: new Date().toISOString(),
        },
      }))
      // 失败保留，不调度自动隐藏
    } finally {
      refresh()
    }
  }

  async function startBulkSync() {
    const list = channels ?? []
    if (list.length === 0) return

    for (const channel of list) {
      clearHideTimer(channel.id)
      patchSync(channel.id, () => ({
        running: true,
        events: [],
        latest: null,
        finalOk: null,
        fading: false,
      }))
    }

    setBulkSync({ running: true, completed: 0, total: list.length })
    try {
      await syncAllChannelsStream({
        onEvent: (ev) => {
          if (ev.channel_id != null) {
            patchSync(ev.channel_id, (prev) => ({
              ...prev,
              events: [...prev.events, ev],
              latest: ev,
              running: ev.stage !== "done" && ev.stage !== "error",
              finalOk: ev.stage === "done" ? true : ev.stage === "error" ? false : prev.finalOk,
              fading: false,
            }))
            if (ev.stage === "done") {
              scheduleHide(ev.channel_id)
            }
          }

          if (ev.index != null && ev.total != null) {
            setBulkSync((prev) => ({
              ...prev,
              completed: Math.max(prev.completed, ev.index ?? prev.completed),
              total: ev.total ?? prev.total,
            }))
          }

          if (ev.channel_id == null && (ev.stage === "done" || ev.stage === "error")) {
            if (ev.stage === "done") {
              toast.success(ev.message)
            } else {
              toast.error(ev.message)
            }
          }
        },
      })
    } catch (e) {
      const err = e as Error
      toast.error(err.message || "批量同步失败")
    } finally {
      setSyncState((s) => {
        const next: Record<number, ChannelSyncState> = {}
        for (const [id, state] of Object.entries(s)) {
          next[Number(id)] = { ...state, running: false }
        }
        return next
      })
      setBulkSync((prev) => ({ ...prev, running: false }))
      refresh()
    }
  }

  async function withBusy(key: string, fn: () => Promise<unknown>) {
    setBusyAction(key)
    try {
      await fn()
      refresh()
    } catch (e) {
      const err = e as Error
      toast.error(err.message || "操作失败")
    } finally {
      setBusyAction(null)
    }
  }

  function renderRedeemSummary(result: ChannelRedeemResult) {
    if (result.type === "subscription") {
      const group = result.group_name ? ` · ${result.group_name}` : ""
      const days = result.validity_days ? ` · ${result.validity_days} 天` : ""
      return `${result.message || "兑换成功"}${group}${days}`
    }
    if (result.type === "concurrency") {
      const extra = result.new_concurrency != null ? ` · 当前并发 ${result.new_concurrency}` : ""
      return `${result.message || "兑换成功"}${extra}`
    }
    const extra = result.new_balance != null ? ` · 当前余额 ${money(result.new_balance)}` : ""
    return `${result.message || "兑换成功"}${extra}`
  }

  return (
    <section>
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-baseline gap-3">
          <h2 className="text-base font-semibold text-foreground">{"渠道"}</h2>
          <p className="text-xs text-muted-foreground">{"实时健康、余额与同步状态"}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2 sm:gap-3">
          <span className="text-xs text-muted-foreground">
            {totalChannels}{" 个渠道"}
          </span>
          <Button
            variant="outline"
            size="sm"
            className="gap-1.5 text-xs"
            disabled={anySyncRunning}
            onClick={() => void startBulkSync()}
          >
            <RefreshCw className={cn("size-3.5", bulkSync.running && "animate-spin")} />
            {bulkSync.running ? `同步中 ${bulkSync.completed}/${bulkSync.total}` : "同步"}
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="gap-1.5 text-xs"
            disabled={channelsLoading || totalChannels === 0}
            onClick={() => setGroupsOpen(true)}
          >
            <Tags className="size-3.5" />
            {"分组"}
          </Button>
          <Button
            size="sm"
            className="gap-1.5 text-xs"
            onClick={() => {
              setEditing(null)
              setCreating(true)
            }}
          >
            <Plus className="size-3.5" />
            {"新增"}
          </Button>
        </div>
      </div>

      {pageQuery.loading && !channelPage ? (
        <p className="rounded-lg border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">
          {"加载中…"}
        </p>
      ) : totalChannels === 0 ? (
        <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">{"还没有任何渠道。"}</p>
          <Button
            size="sm"
            className="mt-3 gap-1.5"
            onClick={() => {
              setEditing(null)
              setCreating(true)
            }}
          >
            <Plus className="size-3.5" />
            {"添加第一个渠道"}
          </Button>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-1 items-start gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-3">
            {visibleChannels.map((c) => {
              const status = statusOf(c)
              const meta = statusMap[status]
              return (
                <Card key={c.id} className="flex flex-col gap-0 border border-border p-3 shadow-none sm:p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                      <span className="truncate text-sm font-semibold text-foreground">{c.name}</span>
                      <span
                        className={cn(
                          "inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium ring-1 ring-inset",
                          c.type === "newapi"
                            ? "bg-brand/10 text-brand ring-brand/20"
                            : "bg-sky-500/10 text-sky-700 ring-sky-500/25 dark:text-sky-300",
                        )}
                      >
                        {channelTypeLabel(c.type)}
                      </span>
                      {!c.monitor_enabled ? (
                        <span className="inline-flex items-center rounded bg-warning/10 px-1.5 py-0.5 text-[10px] font-medium text-warning ring-1 ring-inset ring-warning/20">
                          {"已暂停"}
                        </span>
                      ) : null}
                    </div>
                    <div className="flex shrink-0 items-center gap-1.5">
                      <div className="text-right text-[10px] leading-4 text-muted-foreground">
                        <p>{relativeTime(c.last_balance_at ?? c.updated_at)}</p>
                      </div>
                      <Tooltip delayDuration={150}>
                        <TooltipTrigger asChild>
                          <Button
                            asChild
                            variant="ghost"
                            size="icon-sm"
                            className="size-7 text-muted-foreground hover:text-foreground"
                          >
                            <a
                              href={c.site_url}
                              target="_blank"
                              rel="noopener noreferrer"
                              aria-label={`新窗口打开 ${c.name} 站点地址`}
                            >
                              <ExternalLink className="size-3.5" />
                            </a>
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent side="top" className="text-xs">
                          {"新窗口打开站点地址"}
                        </TooltipContent>
                      </Tooltip>
                    </div>
                  </div>

                  <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3">
                    <StatTile label="余额">
                      <Tooltip delayDuration={150}>
                        <TooltipTrigger asChild>
                          <span className="block truncate">{money(c.last_balance)}</span>
                        </TooltipTrigger>
                        <TooltipContent side="top" className="text-xs">
                          {rechargeMultiplierTip(c)}
                        </TooltipContent>
                      </Tooltip>
                    </StatTile>
                    <StatTile label="今日消费">{money(c.today_cost)}</StatTile>
                    <StatTile label="累计消费">{money(c.total_cost)}</StatTile>
                    {c.balance_threshold > 0 ? (
                      <StatTile label="阈值 / 状态">
                        <div className="flex min-w-0 items-center gap-1.5">
                          <Tooltip delayDuration={150}>
                            <TooltipTrigger asChild>
                              <span className="truncate text-[11px] font-medium text-foreground">
                                {money(c.balance_threshold)}
                              </span>
                            </TooltipTrigger>
                            <TooltipContent side="top" className="text-xs">
                              {`余额低于 ${money(c.balance_threshold)} 时通知`}
                            </TooltipContent>
                          </Tooltip>
                          <span className="text-[10px] text-muted-foreground">/</span>
                          <span className={cn("inline-flex shrink-0 items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium ring-1 ring-inset", meta.cls)}>
                            {meta.label}
                          </span>
                        </div>
                      </StatTile>
                    ) : null}
                    <ChannelSubscriptionUsageMetricTiles channel={c} />
                    {c.last_error ? (
                      <div className="col-span-3 rounded-md border border-border bg-muted/20 px-2.5 py-2">
                        <p className="max-h-16 overflow-y-auto whitespace-pre-wrap break-words pr-1 text-[11px] leading-4 text-danger" title={c.last_error}>
                          {c.last_error}
                        </p>
                      </div>
                    ) : null}
                  </div>

                  <InlineRates channel={c} />

                  <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3">
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1 text-xs"
                      disabled={!!syncState[c.id]?.running || anySyncRunning}
                      onClick={() => startStream(c, "sync")}
                    >
                      <RefreshCw
                        className={cn("size-3", syncState[c.id]?.running && "animate-spin")}
                      />
                      {"同步"}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1 text-xs"
                      disabled={!!syncState[c.id]?.running || anySyncRunning}
                      onClick={() => startStream(c, "test-login")}
                      >
                        <LogIn className="size-3" />
                        {"测试登录"}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1 text-xs"
                      onClick={() => setRecharging(c)}
                    >
                      <CreditCard className="size-3" />
                      {"充值"}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1 text-xs"
                      onClick={() => setRedeeming(c)}
                    >
                      <Gift className="size-3" />
                      {"兑换"}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1 text-xs"
                      onClick={() => setAPIKeyDialog({ channel: c })}
                    >
                      <KeyRound className="size-3" />
                      {"密钥"}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1 text-xs"
                      onClick={() => {
                        setEditing(c)
                        setCreating(true)
                      }}
                    >
                      <Pencil className="size-3" />
                      {"编辑"}
                    </Button>
                  </div>

                  <SyncProgressStrip state={syncState[c.id] ?? emptySyncState()} />

                  <div className="mt-3 flex items-center justify-between gap-2 border-t border-border pt-2.5">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="gap-1 text-xs text-muted-foreground"
                      disabled={busyAction === `toggle-${c.id}`}
                      onClick={() =>
                        withBusy(`toggle-${c.id}`, () =>
                          apiFetch(`/channels/${c.id}/${c.monitor_enabled ? "disable" : "enable"}`, {
                            method: "POST",
                          }),
                        )
                      }
                    >
                      {c.monitor_enabled ? (
                        <Pause className="size-3" />
                      ) : (
                        <Play className="size-3" />
                      )}
                      {c.monitor_enabled ? "暂停监控" : "恢复监控"}
                    </Button>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="gap-1 text-xs text-muted-foreground"
                          disabled={busyAction === `clear-login-${c.id}` || busyAction === `delete-${c.id}`}
                        >
                          <MoreHorizontal className="size-3" />
                          {"更多"}
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="w-44">
                        <DropdownMenuItem
                          disabled={busyAction === `clear-login-${c.id}`}
                          onSelect={async (e) => {
                            e.preventDefault()
                            const ok = await confirm({
                              title: `清空 ${c.name} 的登录信息？`,
                              description: "将清空缓存会话；Token 模式还会清空已保存的 Access Token、Refresh Token 和 NewAPI Cookie。账号密码本身不会删除。",
                              confirmLabel: "清空",
                              destructive: true,
                            })
                            if (!ok) return
                            void withBusy(`clear-login-${c.id}`, async () => {
                              await apiFetch(`/channels/${c.id}/clear-login-info`, { method: "POST" })
                              toast.success("已清空登录信息")
                            })
                          }}
                        >
                          <XCircle className="size-3.5" />
                          {"清空登录信息"}
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          variant="destructive"
                          disabled={busyAction === `delete-${c.id}`}
                          onSelect={async (e) => {
                            e.preventDefault()
                            const ok = await confirm({
                              title: `删除渠道 ${c.name}？`,
                              description: "删除后该渠道的余额历史、倍率快照与登录凭据都将一并清除，且无法恢复。",
                              confirmLabel: "删除",
                              destructive: true,
                            })
                            if (!ok) return
                            void withBusy(`delete-${c.id}`, () =>
                              apiFetch(`/channels/${c.id}`, { method: "DELETE" }),
                            )
                          }}
                        >
                          <Trash2 className="size-3.5" />
                          {"删除"}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </Card>
              )
            })}
          </div>

          <div className="mt-3 flex flex-col gap-2 rounded-lg border border-border bg-muted/10 px-3 py-2 sm:flex-row sm:items-center sm:justify-between">
            <div className="text-xs text-muted-foreground">
              {pageSizeAll
                ? `显示全部 ${totalChannels} 个渠道`
                : `显示 ${rangeStart}-${rangeEnd} / ${totalChannels} 个渠道`}
            </div>
            <div className="flex flex-wrap items-center gap-2 sm:justify-end">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <span>{"每页"}</span>
                <Select
                  value={String(pageSize)}
                  onValueChange={(value) => {
                    const nextPageSize = value === "all" ? "all" : Number(value) as ChannelPageSize
                    setPageSize(nextPageSize)
                    window.localStorage.setItem(CHANNEL_PAGE_SIZE_STORAGE_KEY, String(nextPageSize))
                    setPage(1)
                  }}
                >
                  <SelectTrigger size="sm" className="h-8 w-20 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align="end">
                    {channelPageSizeOptions.map((value) => (
                      <SelectItem key={value} value={String(value)}>
                        {value === "all" ? "全部" : value}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-wrap items-center gap-1.5">
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 px-2 text-xs"
                  disabled={pageSizeAll || currentPage <= 1}
                  onClick={() => setPage(1)}
                >
                  <ChevronsLeft className="size-3.5" />
                  <span className="hidden sm:inline">{"首页"}</span>
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 px-2 text-xs"
                  disabled={pageSizeAll || currentPage <= 1}
                  onClick={() => setPage((prev) => Math.max(1, prev - 1))}
                >
                  {"上一页"}
                </Button>
                {pageSizeAll ? (
                  <span className="min-w-12 text-center text-xs text-muted-foreground">
                    {"全部"}
                  </span>
                ) : (
                  pagerNumbers.map((pageNumber) => (
                    <Button
                      key={pageNumber}
                      variant={pageNumber === currentPage ? "default" : "outline"}
                      size="sm"
                      className="h-8 min-w-8 px-2 text-xs"
                      onClick={() => setPage(pageNumber)}
                    >
                      {pageNumber}
                    </Button>
                  ))
                )}
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 px-2 text-xs"
                  disabled={pageSizeAll || currentPage >= totalPages}
                  onClick={() => setPage((prev) => Math.min(totalPages, prev + 1))}
                >
                  {"下一页"}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 px-2 text-xs"
                  disabled={pageSizeAll || currentPage >= totalPages}
                  onClick={() => setPage(totalPages)}
                >
                  <span className="hidden sm:inline">{"末页"}</span>
                  <ChevronsRight className="size-3.5" />
                </Button>
              </div>
            </div>
          </div>
        </>
      )}

      <ChannelFormDialog
        open={creating}
        onOpenChange={(v) => {
          setCreating(v)
          if (!v) setEditing(null)
        }}
        channel={editing}
      />

      <ChannelGroupsDialog
        open={groupsOpen}
        onOpenChange={setGroupsOpen}
        channels={channels ?? []}
        nestedDialogOpen={apiKeyDialog != null}
        onAPIKeyAction={(channel, rate, type) => {
          setAPIKeyDialog({
            channel,
            initialAction: {
              type,
              targetGroup: {
                id: rate.remote_group_id ?? null,
                name: rate.model_name,
              },
            },
          })
        }}
      />

      <ChannelRedeemDialog
        open={redeeming != null}
        onOpenChange={(v) => {
          if (!v) setRedeeming(null)
        }}
        channel={redeeming}
        onSuccess={(result) => {
          toast.success(renderRedeemSummary(result))
        }}
      />

      <ChannelRechargeDialog
        open={recharging != null}
        onOpenChange={(v) => {
          if (!v) setRecharging(null)
        }}
        channel={recharging}
      />

      <ChannelAPIKeysDialog
        open={apiKeyDialog != null}
        onOpenChange={(v) => {
          if (!v) setAPIKeyDialog(null)
        }}
        channel={apiKeyDialog?.channel ?? null}
        initialAction={apiKeyDialog?.initialAction}
      />

      {confirmDialog}
    </section>
  )
}
