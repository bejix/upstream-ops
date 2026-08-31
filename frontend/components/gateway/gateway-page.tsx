import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { toast } from "sonner"
import {
  DollarSign,
  Layers,
  Loader2,
  Pencil,
  RefreshCw,
  Route,
  ScrollText,
  KeyRound,
  MessageSquareText,
  SlidersHorizontal,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { apiFetch } from "@/lib/api"
import { useChannels } from "@/lib/queries"
import { cn } from "@/lib/utils"
import type {
  GatewayEnsureKeysResult,
  GatewayGroup,
  GatewayKey,
  GatewayKeyCreateResult,
  GatewayModelListItem,
  GatewayModelSyncResult,
  GatewayModelTestResponse,
  GatewayModelTestResult,
  GatewayProviderOption,
  GatewayRoute,
  GatewayServiceTierRule,
  GatewaySystemPromptRule,
  GatewayUsageModelOption,
  GatewayUsagePage,
  GatewayUsageStats,
  ModelDefaultPrice,
  ModelPriceOverride,
  RateSnapshot,
} from "@/lib/api-types"
import {
  parseMappingJSON,
  serializeMappingJSON,
  type MappingRow,
} from "./model-mapping-editor"
import { GatewayProvidersPanel } from "./gateway-providers-panel"
import { GroupsSidebar } from "./groups-sidebar"
import { KeysPanel } from "./keys-panel"
import { RoutesPanel } from "./routes-panel"
import { ModelsPanel } from "./models-panel"
import { UsagePanel } from "./usage-panel"
import { PricesPanel } from "./prices-panel"
import { PromptInjectionPanel } from "./prompt-injection-panel"
import { ServiceTierPolicyPanel } from "./service-tier-policy-panel"
import {
  CreatedSecretDialog,
  DefaultsPriceDialog,
  EnsureKeysResultDialog,
  GroupFormDialog,
  KeyFormDialog,
  ModelSyncResultDialog,
  ModelTestDialog,
  RoutePauseErrorDialog,
} from "./gateway-dialogs"
import {
  emptyGroupForm,
  emptyKeyForm,
  emptyPriceForm,
  ipListToText,
  mTokToPerToken,
  parseModelsJSON,
  parsePriceTiers,
  parseServiceTierRules,
  parseSystemPromptRules,
  perTokenToMTok,
  routeAccountRate,
  routeSourceKind,
  serializePriceTiers,
  sortGatewayRouteRows,
  testBusyKey,
  textToIPListJSON,
  type ConfigTab,
  type GroupFormState,
  type KeyFormState,
  type MainTab,
  type PriceFormState,
} from "./gateway-utils"

export function GatewayPage() {
  const { confirm, dialog: confirmDialog } = useConfirm()
  const channels = useChannels()
  const channelList = channels.data ?? []

  const [groups, setGroups] = useState<GatewayGroup[]>([])
  const [selectedGroupID, setSelectedGroupID] = useState<number | null>(null)
  const [mainTab, setMainTab] = useState<MainTab>("gateway")
  const [configTab, setConfigTab] = useState<ConfigTab>("keys")
  const [loading, setLoading] = useState(false)
  const [autoRefreshInterval, setAutoRefreshInterval] = useState("manual")
  const refreshInFlightRef = useRef(false)
  const [busy, setBusy] = useState(false)
  /** 切换网关组时右侧配置区 loading */
  const [groupLoading, setGroupLoading] = useState(false)
  /** 切换组时递增，丢弃过期请求结果，避免旧组数据覆盖新组 */
  const loadSeqRef = useRef(0)

  const [groupDialogOpen, setGroupDialogOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<GatewayGroup | null>(null)
  const [groupForm, setGroupForm] = useState<GroupFormState>(emptyGroupForm)

  const [keys, setKeys] = useState<GatewayKey[]>([])
  /** key id → 明文密钥（列表内联展示/复制） */
  const [keySecrets, setKeySecrets] = useState<Record<number, string>>({})
  const [keysRefreshing, setKeysRefreshing] = useState(false)
  const [keyDialogOpen, setKeyDialogOpen] = useState(false)
  const [editingKey, setEditingKey] = useState<GatewayKey | null>(null)
  const [keyForm, setKeyForm] = useState<KeyFormState>(emptyKeyForm)
  const [createdSecret, setCreatedSecret] = useState<string | null>(null)

  const [routeDrafts, setRouteDrafts] = useState<Partial<GatewayRoute>[]>([])
  const [providerOptions, setProviderOptions] = useState<GatewayProviderOption[]>([])
  const [sourceGroupsByChannel, setSourceGroupsByChannel] = useState<
    Record<number, RateSnapshot[]>
  >({})

  const [mappingRows, setMappingRows] = useState<MappingRow[]>([])
  const [modelsMode, setModelsMode] = useState<string>("auto")
  const [modelItems, setModelItems] = useState<GatewayModelListItem[]>([])
  const [customModel, setCustomModel] = useState("")
  const [rateSort, setRateSort] = useState<string>("asc")
  const [serviceTierRules, setServiceTierRules] = useState<GatewayServiceTierRule[]>(
    () => parseServiceTierRules(),
  )
  const [systemPromptRules, setSystemPromptRules] = useState<GatewaySystemPromptRule[]>([])

  const [usage, setUsage] = useState<GatewayUsagePage | null>(null)
  const [usageStats, setUsageStats] = useState<GatewayUsageStats | null>(null)
  /** 使用记录独立筛选，不跟左侧网关组；默认全部 */
  const [usageGroupFilter, setUsageGroupFilter] = useState<string>("all")
  const [usageKeys, setUsageKeys] = useState<GatewayKey[]>([])
  const [usageKeyFilter, setUsageKeyFilter] = useState("all")
  /** 模型下拉：all = 全部；其它为后端聚合的 requested_model */
  const [usageModelFilter, setUsageModelFilter] = useState("all")
  const [usageModelOptions, setUsageModelOptions] = useState<
    GatewayUsageModelOption[]
  >([])
  const [usageRequestIDFilter, setUsageRequestIDFilter] = useState("")
  /** all | success | client | fail | multi | multi_success | multi_fail */
  const [usageSuccessFilter, setUsageSuccessFilter] = useState<string>("all")
  const [usageFrom, setUsageFrom] = useState("")
  const [usageTo, setUsageTo] = useState("")
  const [usagePage, setUsagePage] = useState(1)
  const [usagePageSize, setUsagePageSize] = useState(50)
  const [usageLoading, setUsageLoading] = useState(false)
  /** 使用记录请求序号，与网关组 loadSeq 解耦 */
  const usageSeqRef = useRef(0)

  const [prices, setPrices] = useState<ModelPriceOverride[]>([])
  const [priceDialogOpen, setPriceDialogOpen] = useState(false)
  const [editingPrice, setEditingPrice] = useState<ModelPriceOverride | null>(null)
  const [priceForm, setPriceForm] = useState<PriceFormState>(emptyPriceForm)
  const [defaultsOpen, setDefaultsOpen] = useState(false)
  const [defaultsQuery, setDefaultsQuery] = useState("")
  const [defaultPrices, setDefaultPrices] = useState<ModelDefaultPrice[]>([])
  const [defaultsLoading, setDefaultsLoading] = useState(false)

  /** model id → 最近一次探测结果（列表摘要用） */
  const [modelTestResults, setModelTestResults] = useState<
    Record<string, GatewayModelTestResult[]>
  >({})
  const [modelTestOpen, setModelTestOpen] = useState(false)
  const [modelSyncOpen, setModelSyncOpen] = useState(false)
  const [modelSyncResult, setModelSyncResult] = useState<GatewayModelSyncResult | null>(
    null,
  )
  const [ensureKeysOpen, setEnsureKeysOpen] = useState(false)
  const [ensureKeysResult, setEnsureKeysResult] =
    useState<GatewayEnsureKeysResult | null>(null)
  const [modelTestTarget, setModelTestTarget] = useState<GatewayModelListItem | null>(null)
  /** null 空闲；`modelId` 批量；`modelId#routeId` 单条（列表 tag / 弹窗共用） */
  const [modelTesting, setModelTesting] = useState<string | null>(null)
  const [modelTestDialogResults, setModelTestDialogResults] = useState<
    GatewayModelTestResult[]
  >([])
  /** 路由临时暂停错误详情弹窗 */
  const [pauseErrorRoute, setPauseErrorRoute] = useState<Partial<GatewayRoute> | null>(null)
  const modelTestTargetRef = useRef<GatewayModelListItem | null>(null)
  modelTestTargetRef.current = modelTestTarget
  const modelTestOpenRef = useRef(false)
  modelTestOpenRef.current = modelTestOpen

  const selectedGroup = useMemo(
    () => groups.find((g) => g.id === selectedGroupID) ?? null,
    [groups, selectedGroupID],
  )

  const channelNameByID = useMemo(() => {
    const m = new Map<number, string>()
    for (const c of channelList) m.set(c.id, c.name)
    return m
  }, [channelList])

  const providerNameByID = useMemo(() => {
    const m = new Map<number, string>()
    for (const p of providerOptions) m.set(p.id, p.name)
    return m
  }, [providerOptions])

  /** 拉取网关组列表；返回 items，便于刷新时继续拉密钥/路由 */
  const fetchGroups = useCallback(async (): Promise<GatewayGroup[]> => {
    const res = await apiFetch<{ items: GatewayGroup[] }>("/gateway/groups")
    return res.items ?? []
  }, [])

  const loadGroups = useCallback(async () => {
    setLoading(true)
    try {
      const items = await fetchGroups()
      setGroups(items)
      setSelectedGroupID((prev) => {
        if (prev && items.some((g) => g.id === prev)) return prev
        return items[0]?.id ?? null
      })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "加载网关组失败")
    } finally {
      setLoading(false)
    }
  }, [fetchGroups])

  const loadKeys = useCallback(async (groupID: number, seq?: number): Promise<boolean> => {
    try {
      const res = await apiFetch<{ items: GatewayKey[] }>(
        `/gateway/groups/${groupID}/keys`,
      )
      if (seq != null && seq !== loadSeqRef.current) return false
      const items = res.items ?? []
      setKeys(items)
      // 先清空旧明文，避免短暂显示上一组密钥；reveal 后台并行，不阻塞切换
      setKeySecrets({})
      void Promise.all(
        items.map(async (k) => {
          try {
            const r = await apiFetch<{ secret: string }>(`/gateway/keys/${k.id}/reveal`, {
              method: "POST",
            })
            if (seq != null && seq !== loadSeqRef.current) return
            if (r.secret) {
              setKeySecrets((prev) => ({ ...prev, [k.id]: r.secret }))
            }
          } catch {
            /* 单条 reveal 失败不阻塞列表 */
          }
        }),
      )
      return true
    } catch (e) {
      if (seq != null && seq !== loadSeqRef.current) return false
      toast.error(e instanceof Error ? e.message : "加载密钥失败")
      return false
    }
  }, [])

  async function refreshKeys() {
    if (!selectedGroup) return
    setKeysRefreshing(true)
    try {
      const ok = await loadKeys(selectedGroup.id)
      if (ok) toast.success("密钥列表已刷新")
    } finally {
      setKeysRefreshing(false)
    }
  }

  const loadSourceGroups = useCallback(async (channelID: number) => {
    if (!channelID) return
    try {
      const list = await apiFetch<RateSnapshot[]>(`/channels/${channelID}/rates`)
      setSourceGroupsByChannel((prev) => ({ ...prev, [channelID]: list }))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "加载源分组失败")
    }
  }, [])

  const loadRoutes = useCallback(
    async (groupID: number, seq?: number) => {
      try {
        const res = await apiFetch<{ items: GatewayRoute[] }>(
          `/gateway/groups/${groupID}/routes`,
        )
        if (seq != null && seq !== loadSeqRef.current) return
        const items = res.items ?? []
        setRouteDrafts(items.map((r) => ({ ...r })))
        for (const r of items) {
          if (r.source_channel_id) void loadSourceGroups(r.source_channel_id)
        }
      } catch (e) {
        if (seq != null && seq !== loadSeqRef.current) return
        toast.error(e instanceof Error ? e.message : "加载路由失败")
      }
    },
    [loadSourceGroups],
  )

  /** 将组配置同步到右侧本地草稿（映射 / 模型 / 倍率方向） */
  const applyGroupConfigLocal = useCallback((g: GatewayGroup) => {
    setRateSort(g.rate_sort_direction || "asc")
    setModelsMode(g.models_mode || "auto")
    setMappingRows(parseMappingJSON(g.model_mapping))
    setModelItems(parseModelsJSON(g.models_json))
    setServiceTierRules(parseServiceTierRules(g.service_tier_rules_json))
    setSystemPromptRules(parseSystemPromptRules(g.system_prompt_rules_json))
  }, [])

  /**
   * 重载当前组的密钥 + 路由 + 组配置草稿。
   * 顶部「刷新」与切换组共用；seq 用于丢弃过期响应。
   */
  const reloadGroupDetail = useCallback(
    async (group: GatewayGroup, opts?: { showLoading?: boolean }) => {
      const seq = ++loadSeqRef.current
      if (opts?.showLoading !== false) setGroupLoading(true)
      applyGroupConfigLocal(group)
      setKeys([])
      setKeySecrets({})
      setRouteDrafts([])
      setModelTestResults({})
      setModelTesting(null)
      setModelTestOpen(false)
      setModelTestTarget(null)
      setModelTestDialogResults([])
      await Promise.all([loadKeys(group.id, seq), loadRoutes(group.id, seq)])
      if (seq === loadSeqRef.current) {
        setGroupLoading(false)
      }
    },
    [applyGroupConfigLocal, loadKeys, loadRoutes],
  )

  // 时间：兼容 datetime-local（无时区）与 ISO；统一成 RFC3339（含毫秒）
  const usageTimeToRFC3339 = useCallback((raw: string) => {
    const s = raw.trim()
    if (!s) return null
    const localMatch = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(
      s,
    )
    let d: Date
    if (localMatch) {
      const [, y, mo, day, h, mi, sec] = localMatch
      d = new Date(
        Number(y),
        Number(mo) - 1,
        Number(day),
        Number(h),
        Number(mi),
        Number(sec ?? 0),
        0,
      )
    } else {
      d = new Date(s)
    }
    if (Number.isNaN(d.getTime())) return null
    return d.toISOString()
  }, [])

  const loadUsageModels = useCallback(
    async (opts?: {
      groupID?: string | number | null
      keyID?: string
      from?: string
      to?: string
    }) => {
      try {
        const qs = new URLSearchParams()
        const gid = opts?.groupID
        if (gid != null && gid !== "" && gid !== "all" && Number(gid) > 0) {
          qs.set("group_id", String(gid))
        }
        if (opts?.keyID && opts.keyID !== "all") {
          qs.set("gateway_key_id", opts.keyID)
        }
        if (opts?.from?.trim()) {
          const iso = usageTimeToRFC3339(opts.from)
          if (iso) qs.set("from", iso)
        }
        if (opts?.to?.trim()) {
          const iso = usageTimeToRFC3339(opts.to)
          if (iso) qs.set("to", iso)
        }
        const res = await apiFetch<{ items: GatewayUsageModelOption[] }>(
          `/gateway/usage/models?${qs}`,
        )
        const items = res.items ?? []
        setUsageModelOptions(items)
        // 当前选中模型不在列表中时回退到全部
        setUsageModelFilter((cur) => {
          if (!cur || cur === "all") return "all"
          return items.some((m) => m.model === cur) ? cur : "all"
        })
      } catch {
        /* 下拉失败不阻断列表 */
      }
    },
    [usageTimeToRFC3339],
  )

  const loadUsage = useCallback(
    async (opts?: {
      /** 省略或 "all" 表示全部网关组 */
      groupID?: string | number | null
      keyID?: string
      model?: string
      requestID?: string
      /** all | success | client | fail | multi | multi_success | multi_fail；兼容 true/false */
      success?: string
      from?: string
      to?: string
      page?: number
      pageSize?: number
    }) => {
      const seq = ++usageSeqRef.current
      setUsageLoading(true)
      try {
        // 分页参数必须由调用方传入当前筛选态，避免闭包依赖 page 导致 effect 循环
        const pageNum = opts?.page ?? 1
        let size = opts?.pageSize ?? 50
        if (!Number.isFinite(size) || size <= 0) size = 50
        if (size > 200) size = 200
        const qs = new URLSearchParams({
          page: String(pageNum),
          page_size: String(size),
        })
        const gid = opts?.groupID
        if (gid != null && gid !== "" && gid !== "all" && Number(gid) > 0) {
          qs.set("group_id", String(gid))
        }
        if (opts?.keyID && opts.keyID !== "all") qs.set("gateway_key_id", opts.keyID)
        const model = (opts?.model ?? "").trim()
        if (model && model !== "all") qs.set("model", model)
        if (opts?.requestID?.trim()) qs.set("request_id", opts.requestID.trim())
        const result = (opts?.success ?? "all").trim()
        if (result && result !== "all") {
          // 统一走 result；兼容旧 true/false
          if (result === "true") qs.set("result", "success")
          else if (result === "false") qs.set("result", "fail")
          else qs.set("result", result)
        }
        if (opts?.from?.trim()) {
          const iso = usageTimeToRFC3339(opts.from)
          if (iso) qs.set("from", iso)
        }
        if (opts?.to?.trim()) {
          const iso = usageTimeToRFC3339(opts.to)
          if (iso) qs.set("to", iso)
        }
        const [page, stats] = await Promise.all([
          apiFetch<GatewayUsagePage>(`/gateway/usage?${qs}`),
          apiFetch<GatewayUsageStats>(`/gateway/usage/stats?${qs}`),
        ])
        if (seq !== usageSeqRef.current) return
        setUsage(page)
        setUsageStats(stats)
        setUsagePage(page.page || pageNum)
        if (page.page_size > 0) setUsagePageSize(page.page_size)
      } catch (e) {
        if (seq !== usageSeqRef.current) return
        toast.error(e instanceof Error ? e.message : "加载使用记录失败")
      } finally {
        if (seq === usageSeqRef.current) setUsageLoading(false)
      }
    },
    [usageTimeToRFC3339],
  )

  const loadPrices = useCallback(async () => {
    try {
      const res = await apiFetch<{ items: ModelPriceOverride[] }>("/gateway/prices")
      setPrices(res.items ?? [])
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "加载价格失败")
    }
  }, [])

  const loadProviderOptions = useCallback(async () => {
    try {
      const res = await apiFetch<{ items: GatewayProviderOption[] }>(
        "/gateway/providers/options",
      )
      setProviderOptions(res.items ?? [])
    } catch {
      /* 下拉失败不阻断主流程 */
    }
  }, [])

  useEffect(() => {
    void loadGroups()
    void loadPrices()
    void loadProviderOptions()
    // 使用记录默认加载全部网关组，与左侧选中组无关
    void loadUsage({ groupID: "all", page: 1, pageSize: 50 })
  }, [loadGroups, loadPrices, loadProviderOptions, loadUsage])

  // 模型下拉随组 / 密钥 / 时间范围变化重新聚合（不含模型自身筛选）
  useEffect(() => {
    void loadUsageModels({
      groupID: usageGroupFilter,
      keyID: usageKeyFilter,
      from: usageFrom,
      to: usageTo,
    })
  }, [
    usageGroupFilter,
    usageKeyFilter,
    usageFrom,
    usageTo,
    loadUsageModels,
  ])

  const usageQueryOpts = useCallback(
    (page = usagePage, pageSize = usagePageSize) => ({
      groupID: usageGroupFilter,
      keyID: usageKeyFilter,
      model: usageModelFilter,
      requestID: usageRequestIDFilter,
      success: usageSuccessFilter,
      from: usageFrom,
      to: usageTo,
      page,
      pageSize,
    }),
    [
      usageGroupFilter,
      usageKeyFilter,
      usageModelFilter,
      usageRequestIDFilter,
      usageSuccessFilter,
      usageFrom,
      usageTo,
      usagePage,
      usagePageSize,
    ],
  )

  // 密钥列表：全部组时聚合各组密钥；选中组时只保留该组（排除其它组）
  const refreshUsageKeys = useCallback(
    async (groupID: string, groupList: GatewayGroup[] = groups) => {
      try {
        if (groupID === "all" || !groupID) {
          if (groupList.length === 0) {
            setUsageKeys([])
            setUsageKeyFilter("all")
            return
          }
          const pages = await Promise.all(
            groupList.map((g) =>
              apiFetch<{ items: GatewayKey[] }>(
                `/gateway/groups/${g.id}/keys`,
              ).catch(() => ({ items: [] as GatewayKey[] })),
            ),
          )
          const byID = new Map<number, GatewayKey>()
          for (const p of pages) {
            for (const k of p.items ?? []) byID.set(k.id, k)
          }
          const items = Array.from(byID.values()).sort((a, b) => b.id - a.id)
          setUsageKeys(items)
          setUsageKeyFilter((cur) => {
            if (!cur || cur === "all") return "all"
            return items.some((k) => String(k.id) === cur) ? cur : "all"
          })
          return
        }
        const gid = Number(groupID)
        if (!gid) {
          setUsageKeys([])
          setUsageKeyFilter("all")
          return
        }
        const res = await apiFetch<{ items: GatewayKey[] }>(
          `/gateway/groups/${gid}/keys`,
        )
        const items = res.items ?? []
        setUsageKeys(items)
        // 当前选中密钥不属于该组时回退「全部密钥」
        setUsageKeyFilter((cur) => {
          if (!cur || cur === "all") return "all"
          return items.some((k) => String(k.id) === cur) ? cur : "all"
        })
      } catch {
        setUsageKeys([])
        setUsageKeyFilter("all")
      }
    },
    [groups],
  )

  const refreshUsage = useCallback(
    (page = usagePage) => {
      const opts = usageQueryOpts(page)
      void loadUsage(opts)
      // 刷新时同步重拉模型聚合下拉（随当前组/密钥/时间）
      void loadUsageModels({
        groupID: opts.groupID,
        keyID: opts.keyID,
        from: opts.from,
        to: opts.to,
      })
      // 刷新时重拉密钥列表（按当前组过滤）
      void refreshUsageKeys(String(opts.groupID ?? "all"))
    },
    [loadUsage, loadUsageModels, refreshUsageKeys, usagePage, usageQueryOpts],
  )

  /** 顶部刷新：组列表 + 当前组密钥/路由/配置 + 直连选项 + 价格 + 使用记录 */
  const refreshAll = useCallback(async () => {
    if (refreshInFlightRef.current) return
    refreshInFlightRef.current = true
    setLoading(true)
    try {
      const [items] = await Promise.all([
        fetchGroups(),
        loadProviderOptions(),
        loadPrices(),
      ])
      setGroups(items)

      const prevID = selectedGroupID
      const nextID =
        prevID && items.some((g) => g.id === prevID)
          ? prevID
          : (items[0]?.id ?? null)
      setSelectedGroupID(nextID)

      if (nextID) {
        const g = items.find((x) => x.id === nextID)
        if (g) {
          // 同组 id 时切换组 useEffect 不会触发，必须主动重拉密钥/路由
          await reloadGroupDetail(g)
        }
      } else {
        loadSeqRef.current += 1
        setGroupLoading(false)
        setKeys([])
        setKeySecrets({})
        setRouteDrafts([])
        setMappingRows([])
        setModelItems([])
        setSystemPromptRules([])
      }

      const opts = usageQueryOpts(usagePage, usagePageSize)
      void loadUsage(opts)
      void loadUsageModels({
        groupID: opts.groupID,
        keyID: opts.keyID,
        from: opts.from,
        to: opts.to,
      })
      void refreshUsageKeys(String(opts.groupID ?? "all"), items)

      toast.success("已刷新")
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "刷新失败")
    } finally {
      setLoading(false)
      refreshInFlightRef.current = false
    }
  }, [
    fetchGroups,
    loadPrices,
    loadProviderOptions,
    loadUsage,
    loadUsageModels,
    refreshUsageKeys,
    reloadGroupDetail,
    selectedGroupID,
    usagePage,
    usagePageSize,
    usageQueryOpts,
  ])

  useEffect(() => {
    const delay = Number(autoRefreshInterval)
    if (!Number.isFinite(delay) || delay <= 0) return
    const intervalID = window.setInterval(() => {
      void refreshAll()
    }, delay)
    return () => window.clearInterval(intervalID)
  }, [autoRefreshInterval, refreshAll])

  function goUsagePage(p: number) {
    const pages = Math.max(1, usage?.pages ?? 1)
    const next = Math.max(1, Math.min(pages, p))
    setUsagePage(next)
    void loadUsage(usageQueryOpts(next, usagePageSize))
  }

  // 使用记录：网关组变更时刷新密钥选项（选组=排除非本组；全部=全部密钥可选）
  useEffect(() => {
    void refreshUsageKeys(usageGroupFilter, groups)
  }, [usageGroupFilter, groups, refreshUsageKeys])

  useEffect(() => {
    if (!selectedGroup) {
      loadSeqRef.current += 1
      setGroupLoading(false)
      setKeys([])
      setKeySecrets({})
      setRouteDrafts([])
      setMappingRows([])
      setModelItems([])
      setServiceTierRules(parseServiceTierRules())
      setSystemPromptRules([])
      return
    }
    void reloadGroupDetail(selectedGroup)
    // 仅在切换组时重载密钥/路由；使用记录独立，不受网关组影响
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedGroup?.id, reloadGroupDetail])

  function openCreateGroup() {
    setEditingGroup(null)
    setGroupForm(emptyGroupForm())
    setGroupDialogOpen(true)
  }

  function openEditGroup(g: GatewayGroup) {
    setEditingGroup(g)
    setGroupForm({
      name: g.name,
      description: g.description ?? "",
      status: g.status === "disabled" ? "disabled" : "active",
      rate_resort_enabled: !!g.rate_resort_enabled,
      retry_enabled: g.retry_enabled !== false,
      retry_count: String(g.retry_count ?? 0),
      failover_enabled: g.failover_enabled !== false,
      failover_max: String(g.failover_max ?? 8),
      failover_on_4xx: !!g.failover_on_4xx,
      cooldown_seconds: String(g.cooldown_seconds ?? 30),
      first_token_timeout_sec: String(g.first_token_timeout_sec ?? 0),
      user_agent: g.user_agent ?? "",
    })
    setGroupDialogOpen(true)
  }

  async function submitGroupForm() {
    const name = groupForm.name.trim()
    if (!name) {
      toast.error("请填写组名称")
      return
    }
    const retryCount = Math.max(0, Math.min(10, Number(groupForm.retry_count) || 0))
    const failoverMax = Math.max(0, Math.min(32, Number(groupForm.failover_max) || 0))
    const cooldownSeconds = Math.max(
      0,
      Math.min(86400, Number(groupForm.cooldown_seconds) || 0),
    )
    let firstTokenTimeout = Math.floor(Number(groupForm.first_token_timeout_sec) || 0)
    if (firstTokenTimeout < 0) firstTokenTimeout = 0
    if (firstTokenTimeout > 0 && firstTokenTimeout < 1) firstTokenTimeout = 1
    if (firstTokenTimeout > 300) firstTokenTimeout = 300
    const policy = {
      rate_resort_enabled: groupForm.rate_resort_enabled,
      retry_enabled: groupForm.retry_enabled,
      retry_count: retryCount,
      failover_enabled: groupForm.failover_enabled,
      failover_max: failoverMax,
      failover_on_4xx: groupForm.failover_on_4xx,
      cooldown_seconds: cooldownSeconds,
      first_token_timeout_sec: firstTokenTimeout,
      user_agent: groupForm.user_agent.trim(),
    }
    setBusy(true)
    try {
      if (editingGroup) {
        await apiFetch(`/gateway/groups/${editingGroup.id}`, {
          method: "PUT",
          body: JSON.stringify({
            name,
            description: groupForm.description.trim(),
            status: groupForm.status,
            ...policy,
          }),
        })
        toast.success("组已更新")
      } else {
        const g = await apiFetch<GatewayGroup>("/gateway/groups", {
          method: "POST",
          body: JSON.stringify({
            name,
            description: groupForm.description.trim(),
            ...policy,
          }),
        })
        setSelectedGroupID(g.id)
        toast.success("已创建网关组")
      }
      setGroupDialogOpen(false)
      await loadGroups()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败")
    } finally {
      setBusy(false)
    }
  }

  async function deleteGroup(id: number) {
    const ok = await confirm({
      title: "删除网关组",
      description: "将删除组内所有密钥与路由，且不可恢复。",
    })
    if (!ok) return
    try {
      await apiFetch(`/gateway/groups/${id}`, { method: "DELETE" })
      toast.success("已删除")
      await loadGroups()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "删除失败")
    }
  }

  async function reorderGroups(orderedIDs: number[]) {
    if (orderedIDs.length === 0) return
    const prev = groups
    // 乐观更新，避免拖拽后闪回
    const byID = new Map(groups.map((g) => [g.id, g]))
    const optimistic: GatewayGroup[] = []
    orderedIDs.forEach((id, i) => {
      const g = byID.get(id)
      if (g) optimistic.push({ ...g, position: i })
    })
    setGroups(optimistic)
    try {
      const res = await apiFetch<{ items: GatewayGroup[] }>("/gateway/groups/reorder", {
        method: "PUT",
        body: JSON.stringify({ ids: orderedIDs }),
      })
      setGroups(res.items ?? optimistic)
    } catch (e) {
      setGroups(prev)
      toast.error(e instanceof Error ? e.message : "排序失败")
    }
  }

  async function saveGroupConfig() {
    if (!selectedGroup) return
    setBusy(true)
    try {
      await apiFetch(`/gateway/groups/${selectedGroup.id}`, {
        method: "PUT",
        body: JSON.stringify({
          rate_sort_direction: rateSort,
          model_mapping: serializeMappingJSON(mappingRows),
          models_mode: modelsMode,
          models_json: JSON.stringify(modelItems),
          service_tier_rules_json: JSON.stringify(serviceTierRules),
        }),
      })
      // 后端在排序方向变更时会重排路由 position；前端同步草稿
      const res = await apiFetch<{ items: GatewayRoute[] }>(
        `/gateway/groups/${selectedGroup.id}/routes`,
      )
      setRouteDrafts((res.items ?? []).map((r) => ({ ...r })))
      toast.success("网关组配置已保存")
      await loadGroups()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败")
    } finally {
      setBusy(false)
    }
  }

  async function savePromptInjection() {
    if (!selectedGroup) return
    if (systemPromptRules.some(
      (rule) => rule.key_scope === "selected" &&
        rule.key_ids.some((keyID) => !keys.some((key) => key.id === keyID)),
    )) {
      toast.error("所选 Key 必须属于当前组")
      return
    }
    setBusy(true)
    try {
      const updated = await apiFetch<GatewayGroup>(
        `/gateway/groups/${selectedGroup.id}`,
        {
          method: "PUT",
          body: JSON.stringify({
            system_prompt_rules_json: JSON.stringify(systemPromptRules),
          }),
        },
      )
      setGroups((items) =>
        items.map((group) => (group.id === updated.id ? updated : group)),
      )
      setSystemPromptRules(parseSystemPromptRules(updated.system_prompt_rules_json))
      toast.success("提示词注入配置已保存")
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败")
    } finally {
      setBusy(false)
    }
  }

  function openCreateKey() {
    setEditingKey(null)
    setKeyForm(emptyKeyForm())
    setKeyDialogOpen(true)
  }

  function openEditKey(k: GatewayKey) {
    setEditingKey(k)
    setKeyForm({
      name: k.name,
      status: k.status === "disabled" ? "disabled" : "active",
      quota: String(k.quota ?? 0),
      ip_whitelist: ipListToText(k.ip_whitelist),
      ip_blacklist: ipListToText(k.ip_blacklist),
      use_custom_key: false,
      custom_key: "",
      key_len: 48,
      reset_quota_used: false,
    })
    setKeyDialogOpen(true)
  }

  async function submitKeyForm() {
    if (!selectedGroup) return
    const name = keyForm.name.trim()
    if (!name) {
      toast.error("请填写密钥名称")
      return
    }
    const customKey = keyForm.use_custom_key ? keyForm.custom_key.trim() : ""
    if (!editingKey && keyForm.use_custom_key && !customKey) {
      toast.error("请填写自定义密钥")
      return
    }
    setBusy(true)
    try {
      if (editingKey) {
        await apiFetch(`/gateway/keys/${editingKey.id}`, {
          method: "PUT",
          body: JSON.stringify({
            name,
            status: keyForm.status,
            quota: Number(keyForm.quota) || 0,
            ip_whitelist: textToIPListJSON(keyForm.ip_whitelist),
            ip_blacklist: textToIPListJSON(keyForm.ip_blacklist),
            reset_quota_used: keyForm.reset_quota_used || undefined,
          }),
        })
        toast.success("密钥已更新")
      } else {
        const body: Record<string, unknown> = {
          name,
          quota: Number(keyForm.quota) || 0,
          ip_whitelist: textToIPListJSON(keyForm.ip_whitelist),
          ip_blacklist: textToIPListJSON(keyForm.ip_blacklist),
        }
        if (keyForm.use_custom_key) {
          body.custom_key = customKey
        } else {
          body.key_len = keyForm.key_len
        }
        const res = await apiFetch<GatewayKeyCreateResult>(
          `/gateway/groups/${selectedGroup.id}/keys`,
          { method: "POST", body: JSON.stringify(body) },
        )
        if (res.secret) {
          setKeySecrets((prev) => ({ ...prev, [res.key.id]: res.secret }))
          setCreatedSecret(res.secret)
        }
        toast.success("密钥已创建")
      }
      setKeyDialogOpen(false)
      await loadKeys(selectedGroup.id)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败")
    } finally {
      setBusy(false)
    }
  }

  async function toggleKey(k: GatewayKey) {
    const status = k.status === "active" ? "disabled" : "active"
    try {
      await apiFetch(`/gateway/keys/${k.id}`, {
        method: "PUT",
        body: JSON.stringify({ status }),
      })
      if (selectedGroup) await loadKeys(selectedGroup.id)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "更新失败")
    }
  }

  async function deleteKey(id: number) {
    const ok = await confirm({ title: "删除密钥", description: "删除后客户端将无法使用该密钥。" })
    if (!ok) return
    try {
      await apiFetch(`/gateway/keys/${id}`, { method: "DELETE" })
      if (selectedGroup) await loadKeys(selectedGroup.id)
      toast.success("已删除")
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "删除失败")
    }
  }

  async function saveRoutes() {
    if (!selectedGroup) return
    setBusy(true)
    try {
      // 按当前倍率排序后提交，与同步账号 / 运行时尝试顺序一致
      const ordered = sortGatewayRouteRows(
        routeDrafts,
        sourceGroupsByChannel,
        providerOptions,
        rateSort,
      ).map(({ route }) => route)
      const routes = ordered.map((r) => {
        const kind = routeSourceKind(r)
        if (kind === "provider") {
          const pid = Number(r.gateway_provider_id) || 0
          const p = providerOptions.find((x) => x.id === pid)
          const mode = (r.rate_convert_mode as string) || "raw"
          const providerDefault =
            p?.default_billing_rate && p.default_billing_rate > 0
              ? p.default_billing_rate
              : 1
          // custom：用自定义值；raw 等非 custom：账号计费倍率用 provider 默认，
          // 勿写死 1（否则升序调度会把直连当成最贵）。
          const isCustom = mode === "custom"
          const value = isCustom
            ? Number(r.rate_convert_value) || providerDefault
            : providerDefault
          const uaMode =
            r.user_agent_mode === "group" || r.user_agent_mode === "custom"
              ? r.user_agent_mode
              : "passthrough"
          return {
            id: r.id,
            source_kind: "provider",
            source_channel_id: 0,
            gateway_provider_id: pid,
            source_group_id: null,
            source_group_name: "",
            weight: r.weight ?? 1,
            rate_convert_mode: isCustom ? "custom" : mode,
            // raw 等与监控一致：convert_value 占位 1；计费倍率单独落 provider 默认
            rate_convert_value: isCustom ? value : 1,
            billing_rate_multiplier: value > 0 ? value : 1,
            enabled: r.enabled !== false,
            model_mapping: r.model_mapping ?? "",
            upstream_protocol: r.upstream_protocol ?? "auto",
            concurrency: r.concurrency ?? 10,
            user_agent_mode: uaMode,
            user_agent_custom:
              uaMode === "custom" ? (r.user_agent_custom ?? "").trim() : "",
          }
        }
        const chId = Number(r.source_channel_id) || 0
        const groups = sourceGroupsByChannel[chId] ?? []
        const accountRate = routeAccountRate(r, groups)
        const uaMode =
          r.user_agent_mode === "group" || r.user_agent_mode === "custom"
            ? r.user_agent_mode
            : "passthrough"
        return {
          id: r.id,
          source_kind: "monitor",
          source_channel_id: r.source_channel_id,
          gateway_provider_id: 0,
          source_group_id: r.source_group_id ?? null,
          source_group_name: r.source_group_name ?? "",
          weight: r.weight ?? 1,
          rate_convert_mode: r.rate_convert_mode ?? "raw",
          rate_convert_value:
            (r.rate_convert_mode as string) === "custom"
              ? (r.rate_convert_value ?? 0)
              : 1,
          // 与网关路由计费一致：持久化换算后的账号计费倍率（原值=源 ratio）
          billing_rate_multiplier: accountRate > 0 ? accountRate : 1,
          enabled: r.enabled !== false,
          model_mapping: r.model_mapping ?? "",
          upstream_protocol: r.upstream_protocol ?? "auto",
          concurrency: r.concurrency ?? 10,
          user_agent_mode: uaMode,
          user_agent_custom:
            uaMode === "custom" ? (r.user_agent_custom ?? "").trim() : "",
        }
      })
      const res = await apiFetch<{ items: GatewayRoute[] }>(
        `/gateway/groups/${selectedGroup.id}/routes`,
        { method: "PUT", body: JSON.stringify({ routes }) },
      )
      setRouteDrafts((res.items ?? []).map((r) => ({ ...r })))
      toast.success("路由已保存")
      const missing = (res.items ?? []).some(
        (r) => routeSourceKind(r) === "monitor" && !r.source_api_key_name,
      )
      if (missing) {
        toast.message("部分路由尚未确保上游密钥，可点击「确保上游密钥」")
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败")
    } finally {
      setBusy(false)
    }
  }

  async function ensureKeys() {
    if (!selectedGroup) return
    setBusy(true)
    try {
      const res = await apiFetch<GatewayEnsureKeysResult>(
        `/gateway/groups/${selectedGroup.id}/routes/ensure-keys`,
        { method: "POST" },
      )
      setRouteDrafts((res.items ?? []).map((r) => ({ ...r })))
      setEnsureKeysResult(res)
      setEnsureKeysOpen(true)
      const parts = [`成功 ${res.ok_count ?? 0}`]
      if ((res.fail_count ?? 0) > 0) parts.push(`失败 ${res.fail_count}`)
      if ((res.skip_count ?? 0) > 0) parts.push(`跳过 ${res.skip_count}`)
      if ((res.fail_count ?? 0) > 0) {
        toast.message("确保完成（部分失败已跳过）", {
          description: parts.join(" · "),
        })
      } else {
        toast.success(parts.join(" · "))
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "确保密钥失败")
    } finally {
      setBusy(false)
    }
  }

  async function clearRoutePause(routeID?: number) {
    if (!routeID || !selectedGroup) return
    try {
      await apiFetch(`/gateway/routes/${routeID}/clear-pause`, { method: "POST" })
      toast.success("已清除暂停")
      await loadRoutes(selectedGroup.id)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "清除暂停失败")
    }
  }

  async function loadDefaultPrices(query = defaultsQuery) {
    setDefaultsLoading(true)
    try {
      const qs = query.trim() ? `?q=${encodeURIComponent(query.trim())}` : ""
      const res = await apiFetch<{ items: ModelDefaultPrice[] }>(
        `/gateway/prices/defaults${qs}`,
      )
      const items = Array.isArray(res?.items) ? res.items : Array.isArray(res) ? res : []
      setDefaultPrices(items)
    } catch (e) {
      setDefaultPrices([])
      toast.error(e instanceof Error ? e.message : "加载默认价失败")
    } finally {
      setDefaultsLoading(false)
    }
  }

  async function openDefaultsModal() {
    setDefaultsOpen(true)
    // 打开时拉全量；搜索框保留用户输入，由「搜索」再过滤
    await loadDefaultPrices(defaultsQuery)
  }

  async function searchDefaults() {
    await loadDefaultPrices(defaultsQuery)
  }

  function applyDefaultAsOverride(p: ModelDefaultPrice) {
    setEditingPrice(null)
    setPriceForm({
      ...emptyPriceForm(),
      model_name: p.model_name,
      billing_mode: "token",
      input_mtok: perTokenToMTok(p.input_price_per_token),
      output_mtok: perTokenToMTok(p.output_price_per_token),
      cache_create_mtok: perTokenToMTok(p.cache_creation_price_per_token),
      cache_read_mtok: perTokenToMTok(p.cache_read_price_per_token),
      enabled: true,
    })
    setDefaultsOpen(false)
    setPriceDialogOpen(true)
  }

  function openCreatePrice() {
    setEditingPrice(null)
    setPriceForm(emptyPriceForm())
    setPriceDialogOpen(true)
  }

  function openEditPrice(p: ModelPriceOverride) {
    setEditingPrice(p)
    setPriceForm({
      model_name: p.model_name,
      billing_mode: p.billing_mode || "token",
      input_mtok: perTokenToMTok(p.input_price_per_token),
      output_mtok: perTokenToMTok(p.output_price_per_token),
      cache_create_mtok: perTokenToMTok(p.cache_creation_price_per_token),
      cache_read_mtok: perTokenToMTok(p.cache_read_price_per_token),
      per_request_price: String(p.per_request_price ?? 0),
      pricing_tiers: parsePriceTiers(p.pricing_tiers_json),
      image_price_1k: String(p.image_price_1k ?? 0),
      image_price_2k: String(p.image_price_2k ?? 0),
      image_price_4k: String(p.image_price_4k ?? 0),
      video_price_480p: String(p.video_price_480p ?? 0),
      video_price_720p: String(p.video_price_720p ?? 0),
      video_price_1080p: String(p.video_price_1080p ?? 0),
      enabled: p.enabled !== false,
    })
    setPriceDialogOpen(true)
  }

  async function syncModels() {
    if (!selectedGroup) return
    setBusy(true)
    // 未点「保存」的自定义模型也要带上，避免同步结果覆盖后丢失
    const localCustom = modelItems
      .filter((m) => m.source === "custom" && m.id.trim())
      .map((m) => ({ id: m.id.trim(), source: "custom" as const }))
    // 输入框里尚未点「添加自定义」的 ID，一并保留
    const pendingID = customModel.trim()
    if (
      pendingID &&
      !localCustom.some((m) => m.id === pendingID) &&
      !modelItems.some((m) => m.id === pendingID)
    ) {
      localCustom.push({ id: pendingID, source: "custom" })
    }
    try {
      const res = await apiFetch<GatewayModelSyncResult>(
        `/gateway/groups/${selectedGroup.id}/models/sync`,
        {
          method: "POST",
          body: JSON.stringify({ custom_models: localCustom }),
        },
      )
      const synced = parseModelsJSON(res.group?.models_json)
      // 兼容旧后端未回写 custom 时，本地再并一次
      const seen = new Set(synced.map((m) => m.id))
      const merged = [...synced]
      for (const c of localCustom) {
        if (!seen.has(c.id)) {
          merged.push(c)
          seen.add(c.id)
        }
      }
      setModelItems(merged)
      if (pendingID) setCustomModel("")
      setModelSyncResult(res)
      setModelSyncOpen(true)
      const parts = [
        `合并 ${res.model_count ?? 0} 个模型`,
        `成功 ${res.ok_count ?? 0}`,
      ]
      if ((res.fail_count ?? 0) > 0) parts.push(`失败 ${res.fail_count}`)
      if ((res.skip_count ?? 0) > 0) parts.push(`跳过 ${res.skip_count}`)
      if (localCustom.length > 0) {
        parts.push(`保留自定义 ${localCustom.length}`)
      }
      if ((res.fail_count ?? 0) > 0) {
        toast.message("同步完成（部分渠道失败已跳过）", {
          description: parts.join(" · "),
        })
      } else {
        toast.success(parts.join(" · "))
      }
      await loadGroups()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "同步失败")
    } finally {
      setBusy(false)
    }
  }

  function addCustomModel() {
    const id = customModel.trim()
    if (!id) return
    if (modelItems.some((m) => m.id === id)) {
      toast.error("模型已存在")
      return
    }
    setModelItems([...modelItems, { id, source: "custom" }])
    setCustomModel("")
  }

  function openModelTest(m: GatewayModelListItem) {
    setModelTestTarget(m)
    setModelTestDialogResults(modelTestResults[m.id] ?? [])
    setModelTestOpen(true)
  }

  async function runModelTestFor(modelID: string, routeID?: number) {
    if (!selectedGroup) return
    if (modelTesting) return
    const busyKey = testBusyKey(modelID, routeID)
    setModelTesting(busyKey)
    try {
      const body: { model: string; route_id?: number } = { model: modelID }
      if (routeID != null) body.route_id = routeID
      const res = await apiFetch<GatewayModelTestResponse>(
        `/gateway/groups/${selectedGroup.id}/models/test`,
        { method: "POST", body: JSON.stringify(body) },
      )
      const items = res.items ?? []
      setModelTestResults((prev) => {
        const next = { ...prev }
        if (routeID == null) {
          next[modelID] = items
        } else {
          const existing = [...(prev[modelID] ?? [])]
          for (const r of items) {
            const idx = existing.findIndex((x) => x.route_id === r.route_id)
            if (idx >= 0) existing[idx] = r
            else existing.push(r)
          }
          next[modelID] = existing
        }
        return next
      })
      if (modelTestOpenRef.current && modelTestTargetRef.current?.id === modelID) {
        if (routeID == null) {
          setModelTestDialogResults(items)
        } else {
          setModelTestDialogResults((prev) => {
            const merged = [...prev]
            for (const r of items) {
              const idx = merged.findIndex((x) => x.route_id === r.route_id)
              if (idx >= 0) merged[idx] = r
              else merged.push(r)
            }
            return merged
          })
        }
      }
      if (items.length === 0) {
        toast.message("无可测试路由（请先确保上游密钥并启用路由）")
      } else if (res.all_ok) {
        toast.success(
          routeID != null
            ? `可用 · ${items[0]?.label ?? ""} · ${items[0]?.latency_ms ?? 0}ms`
            : `全部可用 ${res.ok_count}/${res.total}`,
        )
      } else {
        toast.error(
          routeID != null
            ? `不可用 · ${items[0]?.error || items[0]?.status_code || "失败"}`
            : `部分失败 ${res.ok_count}/${res.total}`,
        )
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "测试失败")
    } finally {
      setModelTesting(null)
    }
  }

  async function runModelTest(routeID?: number) {
    if (!modelTestTarget) return
    await runModelTestFor(modelTestTarget.id, routeID)
  }

  async function savePrice() {
    const model = priceForm.model_name.trim()
    if (!model) {
      toast.error("请填写模型名")
      return
    }
    setBusy(true)
    try {
      await apiFetch("/gateway/prices", {
        method: "PUT",
        body: JSON.stringify({
          model_name: model,
          billing_mode: priceForm.billing_mode,
          input_price_per_token: mTokToPerToken(priceForm.input_mtok),
          output_price_per_token: mTokToPerToken(priceForm.output_mtok),
          cache_creation_price_per_token: mTokToPerToken(priceForm.cache_create_mtok),
          cache_read_price_per_token: mTokToPerToken(priceForm.cache_read_mtok),
          per_request_price: Number(priceForm.per_request_price) || 0,
          pricing_tiers_json: serializePriceTiers(priceForm.pricing_tiers),
          image_price_1k: Number(priceForm.image_price_1k) || 0,
          image_price_2k: Number(priceForm.image_price_2k) || 0,
          image_price_4k: Number(priceForm.image_price_4k) || 0,
          video_price_480p: Number(priceForm.video_price_480p) || 0,
          video_price_720p: Number(priceForm.video_price_720p) || 0,
          video_price_1080p: Number(priceForm.video_price_1080p) || 0,
          enabled: priceForm.enabled,
        }),
      })
      toast.success(editingPrice ? "价格覆盖已更新" : "价格覆盖已创建")
      setPriceForm(emptyPriceForm())
      setEditingPrice(null)
      setPriceDialogOpen(false)
      await loadPrices()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败")
    } finally {
      setBusy(false)
    }
  }

  async function deletePrice(p: ModelPriceOverride) {
    const ok = await confirm({
      title: "删除价格覆盖",
      description: `确定删除「${p.model_name}」的价格覆盖？删除后将使用系统默认价。`,
      confirmLabel: "删除",
      destructive: true,
    })
    if (!ok) return
    setBusy(true)
    try {
      await apiFetch(`/gateway/prices/${p.id}`, { method: "DELETE" })
      toast.success("已删除价格覆盖")
      await loadPrices()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "删除失败")
    } finally {
      setBusy(false)
    }
  }

  const modelSuggestions = modelItems.map((m) => m.id)

  /** 路由列表按倍率排序展示（与同步账号、运行时尝试顺序一致） */
  const sortedRouteRows = useMemo(
    () =>
      sortGatewayRouteRows(
        routeDrafts,
        sourceGroupsByChannel,
        providerOptions,
        rateSort,
      ),
    [routeDrafts, sourceGroupsByChannel, providerOptions, rateSort],
  )

  return (
    <section className="space-y-4">
      {confirmDialog}

      <header className="space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-lg font-semibold text-foreground">请求网关</h1>
            <Badge
              variant="outline"
              className="border-border bg-muted/40 text-muted-foreground"
            >
              组 · 密钥 · 路由
            </Badge>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => void refreshAll()} disabled={loading}>
              <RefreshCw className={cn("size-3.5", loading && "animate-spin")} />
              刷新
            </Button>
            <Select value={autoRefreshInterval} onValueChange={setAutoRefreshInterval}>
              <SelectTrigger size="sm" className="h-8 w-28 text-xs" aria-label="自动刷新间隔">
                <SelectValue />
              </SelectTrigger>
              <SelectContent align="end">
                <SelectItem value="manual">手动</SelectItem>
                <SelectItem value="5000">5 秒</SelectItem>
                <SelectItem value="10000">10 秒</SelectItem>
                <SelectItem value="30000">30 秒</SelectItem>
                <SelectItem value="60000">60 秒</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <div className="max-w-3xl space-y-1.5 text-sm leading-6 text-muted-foreground">
          <p>
            按网关组配置密钥、路由与模型映射，并查看用量。
          </p>
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <span className="shrink-0">兼容接口</span>
            {[
              "/v1/chat/completions",
              "/v1/responses",
              "/v1/messages",
              "/v1/models",
            ].map((path) => (
              <code
                key={path}
                className="max-w-full break-all rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] leading-relaxed text-foreground/80"
              >
                {path}
              </code>
            ))}
          </div>
        </div>
      </header>

      <Tabs
        value={mainTab}
        onValueChange={(v) => setMainTab(v as MainTab)}
        className="space-y-4"
      >
        <TabsList className="h-auto w-full justify-start rounded-2xl border border-border bg-muted/40 p-1">
          <TabsTrigger value="gateway" className="gap-1.5 px-4 py-2">
            <Layers className="size-3.5" /> 网关
          </TabsTrigger>
          <TabsTrigger
            value="providers"
            className="gap-1.5 px-4 py-2"
            onClick={() => void loadProviderOptions()}
          >
            <Route className="size-3.5" /> 直连渠道
          </TabsTrigger>
          <TabsTrigger value="usage" className="gap-1.5 px-4 py-2">
            <ScrollText className="size-3.5" /> 使用记录
          </TabsTrigger>
          <TabsTrigger value="prices" className="gap-1.5 px-4 py-2">
            <DollarSign className="size-3.5" /> 价格覆盖
          </TabsTrigger>
        </TabsList>

        {/* ── 网关：左组列表 + 右配置 ── */}
        <TabsContent value="gateway" className="mt-0">
          <div className="grid gap-4 xl:grid-cols-[280px_minmax(0,1fr)]">
            <GroupsSidebar
              groups={groups}
              selectedGroupID={selectedGroupID}
              groupLoading={groupLoading}
              busy={busy}
              onSelect={setSelectedGroupID}
              onCreate={openCreateGroup}
              onEdit={openEditGroup}
              onDelete={(id) => void deleteGroup(id)}
              onReorder={(ids) => void reorderGroups(ids)}
            />

            <div className="min-w-0">
              {!selectedGroup ? (
                <Card className="overflow-hidden border-border shadow-none">
                  <CardContent className="py-16 text-center text-sm text-muted-foreground">
                    请选择或创建一个网关组
                  </CardContent>
                </Card>
              ) : (
                <div
                  key={selectedGroup.id}
                  className={cn(
                    "relative space-y-3 transition-opacity duration-200",
                    groupLoading ? "opacity-60" : "opacity-100",
                  )}
                >
                  {groupLoading ? (
                    <div className="pointer-events-none absolute inset-0 z-10 flex items-start justify-center rounded-xl bg-background/40 pt-24 backdrop-blur-[1px]">
                      <div className="flex items-center gap-2 rounded-full border border-border bg-background/95 px-3 py-1.5 text-xs text-muted-foreground shadow-sm">
                        <Loader2 className="size-3.5 animate-spin text-primary" />
                        加载 {selectedGroup.name}…
                      </div>
                    </div>
                  ) : null}

                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <h2 className="truncate text-base font-semibold">{selectedGroup.name}</h2>
                        {groupLoading ? (
                          <Loader2 className="size-3.5 animate-spin text-muted-foreground" />
                        ) : (
                          <Badge
                            variant={selectedGroup.status === "active" ? "default" : "secondary"}
                            className="h-5 px-1.5 text-[10px]"
                          >
                            {selectedGroup.status === "active" ? "启用" : "禁用"}
                          </Badge>
                        )}
                      </div>
                      {selectedGroup.description ? (
                        <p className="mt-0.5 text-xs text-muted-foreground">
                          {selectedGroup.description}
                        </p>
                      ) : null}
                    </div>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={groupLoading}
                      onClick={() => openEditGroup(selectedGroup)}
                    >
                      <Pencil className="size-3.5" /> 编辑组
                    </Button>
                  </div>

                  <Tabs
                    value={configTab}
                    onValueChange={(v) => setConfigTab(v as ConfigTab)}
                    className={cn(
                      "space-y-3 transition-all duration-200",
                      groupLoading
                        ? "pointer-events-none translate-y-0.5"
                        : "translate-y-0 animate-in fade-in-0 duration-200",
                    )}
                  >
                    <TabsList className="h-auto w-full justify-start rounded-lg border border-border bg-muted/30 p-1">
                      <TabsTrigger value="keys" className="flex-none gap-1.5 px-3 py-1.5">
                        <KeyRound className="size-3.5" /> 密钥
                      </TabsTrigger>
                      <TabsTrigger value="routes" className="flex-none gap-1.5 px-3 py-1.5">
                        <Route className="size-3.5" /> 渠道路由
                      </TabsTrigger>
                      <TabsTrigger value="models" className="flex-none gap-1.5 px-3 py-1.5">
                        <Layers className="size-3.5" /> 模型映射
                      </TabsTrigger>
                      <TabsTrigger value="service-tier" className="flex-none gap-1.5 px-3 py-1.5">
                        <SlidersHorizontal className="size-3.5" /> 请求策略
                      </TabsTrigger>
                      <TabsTrigger value="prompt" className="flex-none gap-1.5 px-3 py-1.5">
                        <MessageSquareText className="size-3.5" /> 提示词注入
                      </TabsTrigger>
                    </TabsList>

                    <TabsContent value="keys" className="mt-0 space-y-4">
                      <KeysPanel
                        keys={keys}
                        keySecrets={keySecrets}
                        busy={busy}
                        refreshing={keysRefreshing}
                        onRefresh={() => void refreshKeys()}
                        onCreate={openCreateKey}
                        onEdit={openEditKey}
                        onToggle={(k) => void toggleKey(k)}
                        onDelete={(id) => void deleteKey(id)}
                      />
                    </TabsContent>

                    <TabsContent value="routes" className="mt-0 space-y-4">
                      <RoutesPanel
                        busy={busy}
                        rateSort={rateSort}
                        onRateSortChange={setRateSort}
                        rateResortEnabled={!!selectedGroup.rate_resort_enabled}
                        onSaveSort={() => void saveGroupConfig()}
                        routeDrafts={routeDrafts}
                        setRouteDrafts={setRouteDrafts}
                        sortedRouteRows={sortedRouteRows}
                        channelList={channelList}
                        providerOptions={providerOptions}
                        sourceGroupsByChannel={sourceGroupsByChannel}
                        onLoadSourceGroups={(id) => void loadSourceGroups(id)}
                        onLoadProviderOptions={() => void loadProviderOptions()}
                        onSaveRoutes={() => void saveRoutes()}
                        onEnsureKeys={() => void ensureKeys()}
                        onClearRoutePause={(id) => void clearRoutePause(id)}
                        onShowPauseError={setPauseErrorRoute}
                      />
                    </TabsContent>

                    <TabsContent value="models" className="mt-0 space-y-4">
                      <ModelsPanel
                        busy={busy}
                        modelsMode={modelsMode}
                        onModelsModeChange={setModelsMode}
                        onSyncModels={() => void syncModels()}
                        onSave={() => void saveGroupConfig()}
                        customModel={customModel}
                        onCustomModelChange={setCustomModel}
                        onAddCustomModel={addCustomModel}
                        modelItems={modelItems}
                        setModelItems={setModelItems}
                        routeDrafts={routeDrafts}
                        setRouteDrafts={setRouteDrafts}
                        channelNameByID={channelNameByID}
                        providerNameByID={providerNameByID}
                        modelTestResults={modelTestResults}
                        modelTesting={modelTesting}
                        onRunModelTestFor={(id, rid) => void runModelTestFor(id, rid)}
                        onOpenModelTest={openModelTest}
                        onSaveRoutes={saveRoutes}
                        mappingRows={mappingRows}
                        onMappingRowsChange={setMappingRows}
                        modelSuggestions={modelSuggestions}
                      />
                    </TabsContent>

                    <TabsContent value="service-tier" className="mt-0 space-y-4">
                      <ServiceTierPolicyPanel
                        rules={serviceTierRules}
                        keys={keys}
                        busy={busy || groupLoading}
                        onChange={setServiceTierRules}
                        onSave={() => void saveGroupConfig()}
                      />
                    </TabsContent>

                    <TabsContent value="prompt" className="mt-0 space-y-4">
                      <PromptInjectionPanel
                        rules={systemPromptRules}
                        keys={keys}
                        busy={busy || groupLoading}
                        onChange={setSystemPromptRules}
                        onSave={() => void savePromptInjection()}
                      />
                    </TabsContent>
                  </Tabs>
                </div>
              )}
            </div>
          </div>
        </TabsContent>

        <TabsContent value="providers" className="mt-0">
          <GatewayProvidersPanel />
        </TabsContent>

        <TabsContent value="usage" className="mt-0 space-y-4">
          <UsagePanel
            usage={usage}
            usageStats={usageStats}
            usageLoading={usageLoading}
            groups={groups}
            usageKeys={usageKeys}
            usageGroupFilter={usageGroupFilter}
            setUsageGroupFilter={setUsageGroupFilter}
            usageKeyFilter={usageKeyFilter}
            setUsageKeyFilter={setUsageKeyFilter}
            usageModelFilter={usageModelFilter}
            setUsageModelFilter={setUsageModelFilter}
            usageModelOptions={usageModelOptions}
            usageRequestIDFilter={usageRequestIDFilter}
            setUsageRequestIDFilter={setUsageRequestIDFilter}
            usageSuccessFilter={usageSuccessFilter}
            setUsageSuccessFilter={setUsageSuccessFilter}
            usageFrom={usageFrom}
            setUsageFrom={setUsageFrom}
            usageTo={usageTo}
            setUsageTo={setUsageTo}
            usagePage={usagePage}
            setUsagePage={setUsagePage}
            usagePageSize={usagePageSize}
            setUsagePageSize={setUsagePageSize}
            usageQueryOpts={usageQueryOpts}
            loadUsage={(opts) => void loadUsage(opts)}
            refreshUsage={refreshUsage}
            goUsagePage={goUsagePage}
          />
        </TabsContent>

        <TabsContent value="prices" className="mt-0 space-y-4">
          <PricesPanel
            busy={busy}
            prices={prices}
            priceDialogOpen={priceDialogOpen}
            editingPrice={editingPrice}
            priceForm={priceForm}
            setPriceForm={setPriceForm}
            onPriceDialogOpenChange={setPriceDialogOpen}
            onCreatePrice={openCreatePrice}
            onSavePrice={() => void savePrice()}
            onOpenDefaults={() => void openDefaultsModal()}
            onEditPrice={openEditPrice}
            onDeletePrice={(p) => void deletePrice(p)}
          />
        </TabsContent>
      </Tabs>

      <ModelTestDialog
        open={modelTestOpen}
        onOpenChange={(open) => {
          setModelTestOpen(open)
          if (!open) {
            setModelTestTarget(null)
            setModelTesting(null)
          }
        }}
        modelTestTarget={modelTestTarget}
        modelTestDialogResults={modelTestDialogResults}
        modelTesting={modelTesting}
        routeDrafts={routeDrafts}
        channelNameByID={channelNameByID}
        providerNameByID={providerNameByID}
        onRunModelTest={(routeID) => void runModelTest(routeID)}
      />

      <RoutePauseErrorDialog
        pauseErrorRoute={pauseErrorRoute}
        onClose={() => setPauseErrorRoute(null)}
        onClearPause={(id) => void clearRoutePause(id)}
      />

      <EnsureKeysResultDialog
        open={ensureKeysOpen}
        onOpenChange={setEnsureKeysOpen}
        ensureKeysResult={ensureKeysResult}
      />

      <ModelSyncResultDialog
        open={modelSyncOpen}
        onOpenChange={setModelSyncOpen}
        modelSyncResult={modelSyncResult}
      />

      <GroupFormDialog
        open={groupDialogOpen}
        onOpenChange={setGroupDialogOpen}
        editingGroup={editingGroup}
        groupForm={groupForm}
        setGroupForm={setGroupForm}
        busy={busy}
        onSubmit={() => void submitGroupForm()}
      />

      <KeyFormDialog
        open={keyDialogOpen}
        onOpenChange={setKeyDialogOpen}
        editingKey={editingKey}
        keyForm={keyForm}
        setKeyForm={setKeyForm}
        busy={busy}
        onSubmit={() => void submitKeyForm()}
      />

      <DefaultsPriceDialog
        open={defaultsOpen}
        onOpenChange={setDefaultsOpen}
        defaultsQuery={defaultsQuery}
        setDefaultsQuery={setDefaultsQuery}
        defaultPrices={defaultPrices}
        defaultsLoading={defaultsLoading}
        onSearch={() => void searchDefaults()}
        onLoadIfEmpty={() => {
          if (defaultPrices.length === 0 && !defaultsLoading) {
            void loadDefaultPrices(defaultsQuery)
          }
        }}
        onApplyDefault={applyDefaultAsOverride}
      />

      <CreatedSecretDialog
        createdSecret={createdSecret}
        onClose={() => setCreatedSecret(null)}
      />
    </section>
  )
}
