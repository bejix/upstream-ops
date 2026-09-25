"use client"

import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis, CartesianGrid } from "recharts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useIsMobile } from "@/hooks/use-mobile"
import { useBalanceTrend, useCostTrend, useDashboardSummary } from "@/lib/queries"
import { money } from "@/lib/format"
import { cn } from "@/lib/utils"

function formatY(n: number) {
  if (n === 0) return "$0"
  if (n >= 1000) return `$${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}K`
  if (n >= 100) return `$${n.toFixed(0)}`
  return `$${n.toFixed(n >= 10 ? 1 : 2)}`
}

/**
 * niceCeil 把最大值向上取整到一个"好看的"刻度，避免曲线贴顶。
 * 例如 47 → 50；478 → 500；12,300 → 15,000。
 */
function niceCeil(n: number): number {
  if (!Number.isFinite(n) || n <= 0) return 10
  const padded = n * 1.15
  const mag = Math.pow(10, Math.floor(Math.log10(padded)))
  const norm = padded / mag
  const step = norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 5 ? 5 : 10
  return step * mag
}

function formatDay(iso: string) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${d.getMonth() + 1}月${d.getDate()}日`
}

interface ChartPoint {
  day: string
  balance: number | null
  cost: number | null
}

interface TooltipPayloadItem {
  dataKey?: string
  value: number
}

function ChartTooltip({ active, payload, label }: { active?: boolean; payload?: TooltipPayloadItem[]; label?: string }) {
  if (!active || !payload?.length) return null
  const balance = payload.find((p) => p.dataKey === "balance")?.value
  const cost = payload.find((p) => p.dataKey === "cost")?.value
  return (
    <div className="rounded-lg border border-border bg-popover px-3 py-2 shadow-md">
      <p className="text-xs text-muted-foreground">{label}</p>
      {balance != null ? (
        <p className="text-sm font-semibold text-brand">
          {"余额："}{money(balance)}
        </p>
      ) : null}
      {cost != null ? (
        <p className="mt-1 text-sm font-semibold text-warning">
          {"消费："}{money(cost)}
        </p>
      ) : null}
    </div>
  )
}

export function BalanceOverview() {
  const isMobile = useIsMobile()
  const trend = useBalanceTrend(7)
  const costTrend = useCostTrend(7)
  const summary = useDashboardSummary()

  const channels = summary.data?.channels ?? []
  const trendMap = new Map<string, ChartPoint>()

  for (const point of trend.data ?? []) {
    const key = point.day
    const existing = trendMap.get(key)
    trendMap.set(key, {
      day: formatDay(point.day),
      balance: point.balance,
      cost: existing?.cost ?? null,
    })
  }
  for (const point of costTrend.data ?? []) {
    const key = point.day
    const existing = trendMap.get(key)
    trendMap.set(key, {
      day: existing?.day ?? formatDay(point.day),
      balance: existing?.balance ?? null,
      cost: point.cost,
    })
  }

  const data = Array.from(trendMap.entries())
    .sort(([a], [b]) => new Date(a).getTime() - new Date(b).getTime())
    .map(([, value]) => value)
  const balanceValues = data.map((d) => d.balance ?? 0)
  const costValues = data.map((d) => d.cost ?? 0)
  const yMax = data.length > 0 ? niceCeil(Math.max(...balanceValues)) : 10
  const costMax = data.length > 0 ? niceCeil(Math.max(...costValues)) : 10
  const isLoading = trend.loading || costTrend.loading
  const chartMargin = isMobile
    ? { top: 6, right: 4, left: -18, bottom: 0 }
    : { top: 8, right: 12, left: 0, bottom: 0 }
  const dot = isMobile ? false : { r: 4, fill: "var(--background)", strokeWidth: 2 }
  const activeDot = isMobile ? { r: 4, strokeWidth: 0 } : { r: 5, strokeWidth: 0 }

  return (
    <Card className="border border-border py-4 shadow-none lg:h-100 sm:py-6">
      <CardHeader className="flex shrink-0 flex-row items-center justify-between px-4 pb-2 sm:px-6">
        <CardTitle className="text-base font-semibold">{"余额概览"}</CardTitle>
        <span className="text-xs text-muted-foreground">{"最近 7 天"}</span>
      </CardHeader>
      <CardContent className="flex min-h-0 flex-1 flex-col px-4 sm:px-6">
        <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
          <span className="inline-flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-brand" />
            <span className="text-muted-foreground">{"余额"}</span>
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-warning" />
            <span className="text-muted-foreground">
              {"消费趋势"}
            </span>
          </span>
        </div>
        {/* 图表保底 min-h-40：lg 下卡片定高、图表 flex-1 分剩余空间，渠道再多也只压缩下方渠道列表，不会把图表挤成 0 高 */}
        <div className="h-64 min-h-40 w-full sm:h-72 lg:h-auto lg:flex-1">
          {isLoading ? (
            <div className="flex h-full items-center justify-center text-xs text-muted-foreground">{"加载中…"}</div>
          ) : data.length === 0 ? (
            <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
              {"暂无趋势采样，等待下次扫描或手动刷新"}
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={data} margin={chartMargin}>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" vertical={false} />
                <XAxis
                  dataKey="day"
                  tickLine={false}
                  axisLine={false}
                  interval={isMobile ? 1 : 0}
                  minTickGap={isMobile ? 8 : 5}
                  tick={{ fill: "var(--muted-foreground)", fontSize: isMobile ? 10 : 11 }}
                  dy={isMobile ? 6 : 8}
                />
                <YAxis
                  tickLine={false}
                  axisLine={false}
                  width={isMobile ? 40 : 48}
                  tick={{ fill: "var(--muted-foreground)", fontSize: isMobile ? 10 : 11 }}
                  tickFormatter={formatY}
                  domain={[0, yMax]}
                />
                <YAxis
                  yAxisId="cost"
                  orientation="right"
                  tickLine={false}
                  axisLine={false}
                  width={isMobile ? 0 : 52}
                  tick={isMobile ? false : { fill: "var(--muted-foreground)", fontSize: 11 }}
                  tickFormatter={formatY}
                  domain={[0, costMax]}
                />
                <Tooltip content={<ChartTooltip />} cursor={{ stroke: "var(--border)", strokeDasharray: "4 4" }} />
                <Line
                  type="monotone"
                  dataKey="balance"
                  stroke="var(--brand)"
                  strokeWidth={2}
                  dot={dot}
                  activeDot={{ ...activeDot, fill: "var(--brand)" }}
                />
                <Line
                  yAxisId="cost"
                  type="monotone"
                  dataKey="cost"
                  stroke="var(--warning)"
                  strokeWidth={2}
                  connectNulls={false}
                  dot={dot}
                  activeDot={{ ...activeDot, fill: "var(--warning)" }}
                />
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>

        {/* per-channel chips：最多约 3 行半，超出在列表内滚动；min-h-0 允许空间不足时让位给图表，不会溢出卡片 */}
        {channels.length > 0 ? (
          <div className="mt-3 flex max-h-24 min-h-0 flex-wrap items-center gap-x-5 gap-y-2 overflow-y-auto overscroll-contain border-t border-border pt-3">
            {channels.map((c) => {
              const isFailed = !!c.last_error
              const isUnknown = c.last_balance == null
              return (
                <span key={c.id} className="inline-flex max-w-full items-center gap-1.5 text-xs">
                  <span
                    className={cn(
                      "size-2 rounded-full",
                      isFailed ? "bg-danger" : isUnknown ? "bg-muted-foreground/40" : "bg-success",
                    )}
                  />
                  <span className="max-w-32 truncate font-medium text-foreground sm:max-w-none">{c.name}</span>
                  <span className="min-w-0 tabular-nums text-muted-foreground">
                    {money(c.last_balance)}
                    {" · 今日 "}
                    {money(c.today_cost)}
                  </span>
                </span>
              )
            })}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
