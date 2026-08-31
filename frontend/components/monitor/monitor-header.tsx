import { useEffect, useMemo, useState } from "react"
import { useNavigate } from "react-router-dom"
import { useTheme } from "next-themes"
import {
  Activity,
  Github,
  Home,
  LogOut,
  Menu,
  Monitor,
  Moon,
  Network,
  RefreshCw,
  Settings,
  Sun,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { useAuth } from "@/lib/auth-context"
import { apiFetch } from "@/lib/api"
import { useTriggerRefresh } from "@/lib/refresh-context"
import { useAppVersion, useChannels } from "@/lib/queries"
import type { AppVersion } from "@/lib/api-types"
import { relativeTime } from "@/lib/format"
import { toast } from "sonner"

export function MonitorHeader() {
  const navigate = useNavigate()
  const { theme, setTheme, resolvedTheme } = useTheme()
  const { username, authDisabled, logout } = useAuth()
  const refresh = useTriggerRefresh()
  const channels = useChannels()
  const appVersion = useAppVersion()
  const [mounted, setMounted] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [checkingVersion, setCheckingVersion] = useState(false)

  const appTitle = appVersion.data?.title?.trim() || "UpstreamOps"
  const version = appVersion.data?.version?.trim()
  const latestVersion = appVersion.data?.latest_version?.trim()
  const updateAvailable = Boolean(appVersion.data?.update_available && latestVersion)
  const updateURL = appVersion.data?.release_url?.trim() || appVersion.data?.repo_url?.trim()

  useEffect(() => setMounted(true), [])

  useEffect(() => {
    document.title = appTitle
  }, [appTitle])

  /**
   * 找出所有渠道中最近一次采集时间——这是"上次采集"展示的依据，
   * 让用户知道页面上的余额到底是多新的快照（区别于"我刚点了刷新"）。
   */
  const lastCollectedAt = useMemo(() => {
    const list = channels.data ?? []
    let best: string | null = null
    let bestT = -Infinity
    for (const c of list) {
      if (!c.last_balance_at) continue
      const t = new Date(c.last_balance_at).getTime()
      if (Number.isFinite(t) && t > bestT) {
        bestT = t
        best = c.last_balance_at
      }
    }
    return best
  }, [channels.data])

  function handleRefresh() {
    setSyncing(true)
    refresh()
    setTimeout(() => setSyncing(false), 800)
  }

  async function handleCheckVersion() {
    setCheckingVersion(true)
    try {
      const result = await apiFetch<AppVersion>("/version?force=1")
      appVersion.setData(result)
      if (result.update_error) {
        toast.error(result.update_error)
      } else if (result.update_available && result.latest_version) {
        toast.warning(`发现新版本 ${result.latest_version}`)
      } else {
        toast.success("当前已是最新版本")
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "检测更新失败")
    } finally {
      setCheckingVersion(false)
    }
  }

  const isDark = mounted && resolvedTheme === "dark"
  const themeLabel = theme === "system" ? "自动" : theme === "dark" ? "深色" : "浅色"

  return (
    <header className="sticky top-0 z-20 border-b border-border bg-background/95 backdrop-blur-sm">
      <div className="mx-auto flex h-12 max-w-[120rem] items-center justify-between gap-2 px-3 sm:h-14 sm:gap-4 sm:px-6 lg:px-8">
        {/* left: logo + title */}
        <div className="flex min-w-0 flex-1 items-center gap-2 sm:gap-2.5">
          <div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-foreground text-background sm:size-8">
            <Activity className="size-3.5 sm:size-4" strokeWidth={2.5} />
          </div>
          <div className="min-w-0">
            <h1 className="truncate text-sm font-semibold tracking-tight text-foreground sm:text-base">
              {appTitle}
            </h1>
            {version ? (
              <p className="truncate text-[10px] leading-3 text-muted-foreground sm:text-[11px]">
                <button
                  type="button"
                  className="font-medium underline-offset-2 hover:text-foreground hover:underline"
                  onClick={handleCheckVersion}
                  disabled={checkingVersion}
                  title="点击检测更新"
                >
                  {checkingVersion ? "检测中..." : `v${version}`}
                </button>
                {updateAvailable ? (
                  <a
                    href={updateURL || "https://github.com/bejix/upstream-ops"}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="ml-1.5 font-medium text-emerald-600 underline-offset-2 hover:text-emerald-700 hover:underline sm:ml-2"
                  >
                    有新版本 {latestVersion}
                  </a>
                ) : null}
              </p>
            ) : null}
          </div>
        </div>

        {/* right: actions */}
        <div className="flex shrink-0 items-center gap-1 sm:gap-3">
          {/* desktop: last collected + refresh */}
          <div className="hidden items-center gap-2 sm:flex">
            <span className="text-xs text-muted-foreground">
              {"上次采集 "}
              <span className="font-medium text-foreground">{relativeTime(lastCollectedAt)}</span>
            </span>
            <Tooltip delayDuration={200}>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleRefresh}
                  disabled={syncing}
                  className="gap-1.5 border-border bg-background text-foreground hover:bg-muted"
                  aria-label="刷新视图"
                >
                  <RefreshCw className={cn("size-3.5", syncing && "animate-spin")} />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom" className="max-w-xs text-xs">
                <p>{"重新拉取最新的快照数据。"}</p>
                <p className="mt-1 text-muted-foreground">
                  {"提示：实际采集由后台定时任务执行，如需立即采集请到具体渠道点 \"同步\"。"}
                </p>
              </TooltipContent>
            </Tooltip>
          </div>

          {/* mobile: refresh only (keeps one-tap access) */}
          <Button
            variant="outline"
            size="icon"
            onClick={handleRefresh}
            disabled={syncing}
            className="size-8 border-border bg-background text-foreground hover:bg-muted sm:hidden"
            aria-label="刷新视图"
          >
            <RefreshCw className={cn("size-3.5", syncing && "animate-spin")} />
          </Button>

          {/* mobile: collapse nav + secondary actions into a menu */}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="outline"
                size="icon"
                className="size-8 border-border bg-background text-foreground hover:bg-muted sm:hidden"
                aria-label="更多菜单"
              >
                <Menu className="size-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-52">
              <DropdownMenuLabel className="font-normal text-muted-foreground">
                上次采集{" "}
                <span className="font-medium text-foreground">{relativeTime(lastCollectedAt)}</span>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={() => navigate("/")}>
                <Home className="size-4" />
                主页
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => navigate("/gateway")}>
                <Network className="size-4" />
                请求网关
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => navigate("/settings")}>
                <Settings className="size-4" />
                系统设置
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem asChild>
                <a
                  href="https://github.com/bejix/upstream-ops"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <Github className="size-4" />
                  GitHub 仓库
                </a>
              </DropdownMenuItem>
              <DropdownMenuLabel className="font-normal text-muted-foreground">
                主题：{themeLabel}
              </DropdownMenuLabel>
              <DropdownMenuItem onSelect={() => setTheme("light")}>
                <Sun className="size-4" />
                浅色模式
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => setTheme("dark")}>
                <Moon className="size-4" />
                深色模式
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => setTheme("system")}>
                <Monitor className="size-4" />
                自动
              </DropdownMenuItem>
              {authDisabled ? null : (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onSelect={logout}>
                    <LogOut className="size-4" />
                    {username ? `${username} · 退出` : "退出登录"}
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>

          {/* desktop: full action row */}
          <div className="hidden items-center gap-1.5 sm:flex sm:gap-3">
            <Tooltip delayDuration={200}>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => navigate("/")}
                  className="size-8 border-border bg-background text-foreground hover:bg-muted"
                  aria-label="主页"
                >
                  <Home className="size-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom" className="text-xs">
                {"主页"}
              </TooltipContent>
            </Tooltip>

            <Tooltip delayDuration={200}>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => navigate("/gateway")}
                  className="size-8 border-border bg-background text-foreground hover:bg-muted"
                  aria-label="请求网关"
                >
                  <Network className="size-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom" className="text-xs">
                {"请求网关"}
              </TooltipContent>
            </Tooltip>

            <Tooltip delayDuration={200}>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => navigate("/settings")}
                  className="size-8 border-border bg-background text-foreground hover:bg-muted"
                  aria-label="系统设置"
                >
                  <Settings className="size-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom" className="text-xs">
                {"系统设置"}
              </TooltipContent>
            </Tooltip>

            <Tooltip delayDuration={200}>
              <TooltipTrigger asChild>
                <Button
                  asChild
                  variant="outline"
                  size="icon"
                  className="size-8 border-border bg-background text-foreground hover:bg-muted"
                  aria-label="GitHub 仓库"
                >
                  <a
                    href="https://github.com/bejix/upstream-ops"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <Github className="size-3.5" />
                  </a>
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom" className="text-xs">
                {"GitHub · bejix/upstream-ops"}
              </TooltipContent>
            </Tooltip>

            <DropdownMenu>
              <Tooltip delayDuration={200}>
                <TooltipTrigger asChild>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="outline"
                      size="icon"
                      className="size-8 border-border bg-background text-foreground hover:bg-muted"
                      aria-label={`主题：${themeLabel}`}
                    >
                      {theme === "system" ? <Monitor className="size-3.5" /> : isDark ? <Moon className="size-3.5" /> : <Sun className="size-3.5" />}
                    </Button>
                  </DropdownMenuTrigger>
                </TooltipTrigger>
                <TooltipContent side="bottom" className="text-xs">
                  {`主题：${themeLabel}`}
                </TooltipContent>
              </Tooltip>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => setTheme("light")}>
                  <Sun className="size-4" />
                  浅色模式
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setTheme("dark")}>
                  <Moon className="size-4" />
                  深色模式
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setTheme("system")}>
                  <Monitor className="size-4" />
                  自动
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            {authDisabled ? null : (
              <Tooltip delayDuration={200}>
                <TooltipTrigger asChild>
                  <Button
                    variant="outline"
                    size="icon"
                    onClick={logout}
                    className="size-8 border-border bg-background text-foreground hover:bg-muted"
                    aria-label="退出登录"
                  >
                    <LogOut className="size-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom" className="text-xs">
                  {username ? `${username} · 退出登录` : "退出登录"}
                </TooltipContent>
              </Tooltip>
            )}
          </div>
        </div>
      </div>
    </header>
  )
}
