/**
 * API response shapes for UpstreamOps backend.
 * Keep in sync with backend/storage/*.go and backend/api/*.go.
 */

export type ChannelType = "newapi" | "sub2api"

export type CredentialMode = "password" | "token"

export type RechargeMultiplierMode = "divide" | "multiply"

export type NotificationChannelType =
  | "telegram"
  | "webhook"
  | "email"
  | "wecom"
  | "dingtalk"
  | "feishu"
  | "serverchan3"

export type CaptchaProviderType =
  | "capsolver"
  | "2captcha"
  | "anticaptcha"
  | "yescaptcha"

export type MonitorJob = "login" | "balance" | "rates"

export type NotificationEvent =
  | "balance_low"
  | "rate_changed"
  | "rate_structure_changed"
  | "rate_added"
  | "rate_removed"
  | "announcement"
  | "login_failed"
  | "captcha_failed"
  | "monitor_failed"
  | "subscription_daily_remaining_low"
  | "subscription_weekly_remaining_low"
  | "subscription_monthly_remaining_low"
  | "subscription_expiring"

export interface Channel {
  id: number
  name: string
  type: ChannelType
  site_url: string
  username: string
  sort_order: number
  user_id?: string
  credential_mode: CredentialMode
  login_extra_params: string
  turnstile_enabled: boolean
  ignore_announcements: boolean
  subscription_enabled: boolean
  proxy_enabled: boolean
  captcha_config_id?: number | null
  balance_threshold: number
  recharge_multiplier?: number | null
  recharge_multiplier_mode: RechargeMultiplierMode
  monitor_enabled: boolean
  only_created_key_groups_enabled: boolean
  /** 自定义标签；旧后端可能缺失，按空数组处理 */
  tags?: string[]
  /** 备注；缺失按空字符串处理 */
  notes?: string
  last_balance?: number | null
  last_balance_at?: string | null
  today_cost?: number | null
  today_cost_at?: string | null
  total_cost?: number | null
  last_error?: string
  created_at: string
  updated_at: string
}

export interface ChannelPage {
  items: Channel[]
  total: number
  page: number
  page_size: number
  pages: number
}

/** GET /api/channels 分页模式的 status 筛选值（backend/storage/channel_list.go） */
export type ChannelListStatus = "healthy" | "low" | "failed" | "idle" | "paused"

/** GET /api/channels 分页模式的 sort 取值；不传即 default（sort_order DESC, id ASC） */
export type ChannelListSort = "default" | "name" | "username" | "health" | "balance"

export type ChannelListOrder = "asc" | "desc"

export interface CaptchaConfig {
  id: number
  name: string
  type: CaptchaProviderType
  endpoint?: string
  extra?: string
  enabled: boolean
  proxy_enabled: boolean
  last_balance?: number | null
  balance_unit?: string
  balance_at?: string | null
  balance_error?: string
  created_at: string
  updated_at: string
}

export interface RateSnapshot {
  id: number
  channel_id: number
  remote_group_id?: number | null
  model_name: string
  description?: string
  ratio: number
  completion_ratio: number
  first_seen_at: string
  last_seen_at: string
}

export interface RateChangeLog {
  id: number
  channel_id: number
  model_name: string
  old_ratio: number | null
  new_ratio: number
  old_completion_ratio?: number | null
  new_completion_ratio?: number
  changed_at: string
}

export interface RateChangeLogPage {
  items: RateChangeLog[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface BalanceSnapshot {
  id: number
  channel_id: number
  balance: number
  sampled_at: string
}

export interface NotificationSubscription {
  channel_ids: number[]
  mode: "all" | "groups"
  groups?: string[]
  events?: NotificationEvent[]
}

export interface NotificationChannel {
  id: number
  name: string
  type: NotificationChannelType
  enabled: boolean
  proxy_enabled: boolean
  subscriptions?: string
  created_at: string
  updated_at: string
}

export interface NotificationLog {
  id: number
  channel_id: number
  upstream_channel_id?: number
  channel_name?: string
  channel_type?: string
  event: NotificationEvent
  subject: string
  body: string
  success: boolean
  error_message?: string
  sent_at: string
}

export interface UpstreamAnnouncement {
  id: number
  channel_id: number
  source_key: string
  title?: string
  content: string
  type?: string
  link?: string
  published_at?: string | null
  source_updated_at?: string | null
  first_seen_at: string
}

export interface MonitorLog {
  id: number
  channel_id: number
  job: MonitorJob
  success: boolean
  error_message?: string
  duration_ms: number
  started_at: string
  finished_at: string
}

export interface DashboardLowest {
  channel_id: number
  name: string
  balance: number | null
}

export interface DashboardChannelStat {
  id: number
  name: string
  type: string
  monitor_enabled: boolean
  last_balance?: number | null
  today_cost?: number | null
  total_cost?: number | null
  last_error?: string
}

export interface DashboardSummary {
  total_channels: number
  active_channels: number
  failed_channels: number
  total_balance: number
  today_total_cost: number
  total_cost: number
  lowest_balance: DashboardLowest | null
  channels: DashboardChannelStat[]
  recent_rate_changes: RateChangeLog[]
}

export interface BalanceTrendPoint {
  day: string
  balance: number
}

export interface CostTrendPoint {
  day: string
  cost: number
}

export interface SystemAuthConfig {
  enabled: boolean
  username: string
  password: string
  tokenSecret: string
  sessionTTLHours: number
}

export interface AppConfig {
  title: string
  notificationPrefix: string
}

export interface SystemSchedulerRetentionConfig {
  cron: string
  monitorLogsDays: number
  balanceSnapshotsDays: number
  notificationLogsDays: number
  announcementsDays: number
}

export interface SystemSchedulerConfig {
  balanceCron: string
  rateCron: string
  concurrency: number
  retention: SystemSchedulerRetentionConfig
}

export interface SystemNotificationsConfig {
  minChangePct: number
  balanceLowCooldownMinutes: number
  subscriptionDailyRemainingThresholdPct: number
  subscriptionWeeklyRemainingThresholdPct: number
  subscriptionMonthlyRemainingThresholdPct: number
  subscriptionExpiryThresholdHours: number
  subscriptionAlertCooldownMinutes: number
  sendMaxAttempts: number
}

export interface SystemProxyConfig {
  enabled: boolean
  versionCheckEnabled: boolean
  protocol: "http" | "https" | "socks5"
  host: string
  port: number
  username: string
  password: string
}

export interface SystemUpstreamConfig {
  timeoutSeconds: number
  userAgent: string
}

/** 网关运行时参数（设置页可改，Apply 后立即生效） */
export interface SystemGatewayConfig {
  tempPauseSeconds: number
  forwardTimeoutSeconds: number
  modelsCacheTTLSeconds: number
  maxFailoverSwitches: number
  routeBatchConcurrency: number
  usageErrorBodyBytes: number
  usageErrorMsgRunes: number
  usageErrorHeaderValueRunes: number
  usageErrorHeadersJSONBytes: number
  streamKeepaliveSeconds: number
  streamIdleTimeoutSeconds: number
  streamMaxLineBytes: number
}

export interface SystemConfig {
  app: AppConfig
  auth: SystemAuthConfig
  scheduler: SystemSchedulerConfig
  notifications: SystemNotificationsConfig
  proxy: SystemProxyConfig
  upstream: SystemUpstreamConfig
  gateway: SystemGatewayConfig
}

export interface SystemConfigResponse {
  config_path: string
  config: SystemConfig
}

export interface AppVersion {
  name: string
  title: string
  version: string
  latest_version?: string
  update_available?: boolean
  repo_url?: string
  release_url?: string
  update_error?: string
}

export interface ApplyConfigResult {
  applied_sections: string[]
  message: string
}

export interface ChannelRedeemResult {
  message: string
  type: string
  value: number
  new_balance?: number
  new_concurrency?: number
  group_name?: string
  validity_days?: number
}

export type RechargePaymentMethod = "alipay" | "wxpay"
export type SubscriptionPaymentMethod =
  | "balance"
  | "alipay"
  | "wxpay"
  | "stripe"
  | "creem"
  | "waffo_pancake"
  | string

export interface ChannelRechargeMethod {
  type: RechargePaymentMethod
  name: string
  min_amount: number
  max_amount: number
}

export interface ChannelRechargeInfo {
  amount_label: string
  amount_step: number
  min_amount: number
  max_amount: number
  preset_amounts: number[]
  help_text?: string
  help_image_url?: string
  alipay_force_qrcode: boolean
  methods: ChannelRechargeMethod[]
}

export interface ChannelRechargeLaunch {
  mode: "qrcode" | "redirect" | "form" | "success"
  qr_code?: string
  pay_url?: string
  form_action?: string
  form_fields?: Record<string, string>
  expires_at?: string
}

export interface ChannelSubscriptionMethod {
  type: SubscriptionPaymentMethod
  name: string
}

export interface ChannelSubscriptionPlan {
  id: string
  name: string
  description?: string
  price: number
  currency?: string
  validity?: string
  group_name?: string
  quota?: number
  daily_limit_usd?: number | null
  weekly_limit_usd?: number | null
  monthly_limit_usd?: number | null
  features?: string[]
  payment_methods?: string[]
}

export interface ChannelSubscriptionInfo {
  plans: ChannelSubscriptionPlan[]
  methods: ChannelSubscriptionMethod[]
}

export type ChannelSubscriptionLaunch = ChannelRechargeLaunch

export interface ChannelSubscriptionUsageWindow {
  limit_usd: number
  used_usd: number
  remaining_usd: number
  remaining_percent: number
  used_percent: number
  window_start?: string | null
  resets_at?: string | null
  resets_in_seconds: number
}

export interface ChannelSubscriptionUsage {
  id: number
  group_id: number
  group_name: string
  status: string
  starts_at?: string | null
  expires_at?: string | null
  expires_in_days: number
  daily?: ChannelSubscriptionUsageWindow | null
  weekly?: ChannelSubscriptionUsageWindow | null
  monthly?: ChannelSubscriptionUsageWindow | null
}

export interface ChannelSubscriptionUsageInfo {
  items: ChannelSubscriptionUsage[]
}

export type ChannelAPIKeyStatus = "active" | "disabled" | "expired" | "quota_exhausted" | "unknown"

export interface ChannelAPIKey {
  id: number
  key: string
  name: string
  status: ChannelAPIKeyStatus | string
  group?: string
  group_name?: string
  group_description?: string
  group_ratio: number
  group_id?: number | null
  quota: number
  quota_used: number
  unlimited_quota: boolean
  expired_time: number
  expires_at?: string | null
  created_at?: string | null
  updated_at?: string | null
  last_used_at?: string | null
  allow_ips?: string
  ip_whitelist?: string[]
  ip_blacklist?: string[]
  model_limits_enabled: boolean
  model_limits?: string
  cross_group_retry: boolean
  rate_limit_5h: number
  rate_limit_1d: number
  rate_limit_7d: number
  usage_5h: number
  usage_1d: number
  usage_7d: number
}

export interface ChannelAPIKeyPage {
  items: ChannelAPIKey[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface NotificationLogPage {
  items: NotificationLog[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface UpstreamAnnouncementPage {
  items: UpstreamAnnouncement[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface ChannelAPIKeyGroup {
  id?: number | null
  name: string
  description?: string
  ratio: number
}

export interface ChannelAPIKeyReveal {
  key: string
}

// ---------- Gateway ----------

export type GatewayKeyStatus = "active" | "disabled"
export type GatewayGroupStatus = "active" | "disabled"
export type GatewayRateSortDirection = "asc" | "desc"
export type GatewayRateConvertMode = "raw" | "multiply_100" | "divide_100" | "custom"
export type GatewayModelsMode = "auto" | "manual" | "hybrid"
export type GatewayServiceTier = "all" | "priority" | "flex"
export type GatewayServiceTierAction = "filter" | "passthrough" | "block" | "force_priority"
export type GatewayServiceTierKeyScope = "all" | "selected"

export interface GatewayServiceTierRule {
  tier: GatewayServiceTier
  action: GatewayServiceTierAction
  key_scope?: GatewayServiceTierKeyScope
  key_ids?: number[]
  /** 仅兼容旧版单 Key 规则；新规则请使用 key_ids。 */
  key_id?: number
  /** 仅兼容旧版按用户邮箱保存的规则。 */
  user_email?: string
  models?: string[]
}

export interface GatewaySystemPromptRule {
  enabled: boolean
  text: string
  override: boolean
  key_scope: "all" | "selected"
  key_ids: number[]
}
/** 上游协议：auto / OpenAI Chat / OpenAI Responses / Anthropic；openai 为 chat 历史别名 */
export type GatewayUpstreamProtocol =
  | "auto"
  | "openai"
  | "openai_chat"
  | "openai_responses"
  | "anthropic"

export interface GatewayGroup {
  id: number
  name: string
  description?: string
  /** 侧栏排序，越小越靠前 */
  position?: number
  status: GatewayGroupStatus
  rate_sort_direction: GatewayRateSortDirection
  /**
   * 渠道分组价格倍率重排：开启后，倍率扫描结束时按源分组实时倍率
   * 重写路由顺序与账号计费倍率。
   */
  rate_resort_enabled?: boolean
  model_mapping?: string
  models_json?: string
  models_mode: GatewayModelsMode
  service_tier_rules_json?: string
  system_prompt_rules_json?: string
  /** 重试总开关：关闭则失败直接回显，不重试不顺延 */
  retry_enabled?: boolean
  /** 同一路由额外重试次数（不含首次） */
  retry_count?: number
  /** 是否顺延下一条路由 */
  failover_enabled?: boolean
  /** 顺延次数（首条之后最多再换几条） */
  failover_max?: number
  /**
   * 4xx 也走重试/顺延。默认 false：仅网络错误、429、5xx；
   * 开启后 400/401/403/404 等 4xx 同样会重试与顺延（429 始终可顺延）。
   */
  failover_on_4xx?: boolean
  /** 失败冷却秒数，0=不冷却 */
  cooldown_seconds?: number
  /**
   * 首字/首字节超时（秒）。0=关闭；≥1 时超过该时间未收到首字节则主动断开并走重试/顺延。
   * 可能增加计费（上游已计费却换路由再请求）。
   */
  first_token_timeout_sec?: number
  /**
   * 组级统一 User-Agent。路由 user_agent_mode=group 时使用；
   * 转发 / 模型测试 / 拉模型渠道共用。
   */
  user_agent?: string
  created_at: string
  updated_at: string
}

export interface GatewayKey {
  id: number
  group_id: number
  name: string
  key_prefix: string
  status: GatewayKeyStatus
  quota: number
  quota_used: number
  ip_whitelist?: string
  ip_blacklist?: string
  last_used_at?: string | null
  created_at: string
  updated_at: string
}

export interface GatewayKeyCreateResult {
  key: GatewayKey
  secret: string
}

/** monitor = 监控渠道；provider = 直连 BaseURL+Key */
export type GatewayRouteSourceKind = "monitor" | "provider"

export interface GatewayProvider {
  id: number
  name: string
  base_url: string
  api_key_hint: string
  upstream_protocol: GatewayUpstreamProtocol
  default_billing_rate: number
  auth_style?: string
  enabled: boolean
  /** 与监控渠道一致：全局代理开启且本开关打开时，转发走系统代理 */
  proxy_enabled?: boolean
  extra_headers?: string
  notes?: string
  created_at: string
  updated_at: string
}

/** 路由 User-Agent：透传客户端 / 用组 UA / 路由自定义 */
export type GatewayUserAgentMode = "passthrough" | "group" | "custom"

export interface GatewayProviderPage {
  items: GatewayProvider[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface GatewayProviderOption {
  id: number
  name: string
  base_url: string
  api_key_hint: string
  upstream_protocol: GatewayUpstreamProtocol
  default_billing_rate: number
  enabled: boolean
}

export interface GatewayRoute {
  id: number
  gateway_group_id: number
  position: number
  source_kind?: GatewayRouteSourceKind
  source_channel_id: number
  gateway_provider_id?: number
  source_group_id?: number | null
  source_group_name?: string
  weight: number
  rate_convert_mode: GatewayRateConvertMode
  rate_convert_value: number
  billing_rate_multiplier: number
  enabled: boolean
  model_mapping?: string
  upstream_protocol: GatewayUpstreamProtocol
  concurrency: number
  /** 透传 / 组 UA / 自定义；默认透传 */
  user_agent_mode?: GatewayUserAgentMode
  /** mode=custom 时的 User-Agent；转发/模型测试/拉模型共用 */
  user_agent_custom?: string
  source_api_key_id: number
  source_api_key_name: string
  temp_unschedulable_until?: string | null
  temp_unschedulable_reason?: string
  /** 最近一次触发暂停的失败请求时间 */
  temp_unschedulable_at?: string | null
  /** 最近一次触发暂停的网关 request_id（与使用记录关联） */
  temp_unschedulable_request_id?: string
  created_at: string
  updated_at: string
}

export interface GatewayModelSource {
  route_id?: number
  channel_id: number
  channel_name?: string
  source_group_id?: number | null
  source_group_name?: string
}

export interface GatewayModelListItem {
  id: string
  source: "sync" | "custom"
  channel_ids?: number[]
  sources?: GatewayModelSource[]
}

export interface GatewayModelTestResult {
  route_id: number
  channel_id: number
  channel_name?: string
  source_group_id?: number | null
  source_group_name?: string
  label: string
  requested_model: string
  upstream_model: string
  upstream_path?: string
  ok: boolean
  status_code: number
  latency_ms: number
  error?: string
}

export interface GatewayModelSyncRouteResult {
  route_id: number
  source_kind: string
  channel_id?: number
  channel_name?: string
  provider_id?: number
  provider_name?: string
  label: string
  ok: boolean
  skipped: boolean
  model_count: number
  error?: string
  skip_reason?: string
}

export interface GatewayModelSyncResult {
  group: GatewayGroup
  model_count: number
  routes: GatewayModelSyncRouteResult[]
  ok_count: number
  fail_count: number
  skip_count: number
}

export interface GatewayEnsureKeyRouteResult {
  route_id: number
  source_kind: string
  channel_id?: number
  channel_name?: string
  provider_id?: number
  provider_name?: string
  label: string
  key_name?: string
  ok: boolean
  skipped: boolean
  error?: string
  skip_reason?: string
}

export interface GatewayEnsureKeysResult {
  items: GatewayRoute[]
  routes: GatewayEnsureKeyRouteResult[]
  ok_count: number
  fail_count: number
  skip_count: number
}

export interface GatewayModelTestResponse {
  items: GatewayModelTestResult[]
  ok_count: number
  total: number
  all_ok: boolean
}

export interface GatewayUsageLog {
  id: number
  gateway_group_id: number
  gateway_key_id: number
  route_id: number
  channel_id: number
  gateway_provider_id?: number
  provider_name?: string
  request_id: string
  /** 同一 request_id 下第几次尝试（从 1 起） */
  attempt?: number
  /** primary | retry | failover */
  attempt_kind?: string
  /** 本条失败触发的冷却截止 */
  cooldown_until?: string | null
  requested_model: string
  upstream_model?: string
  model_mapping_chain?: string
  inbound_endpoint?: string
  upstream_endpoint?: string
  inbound_protocol?: string
  upstream_protocol?: string
  protocol_converted?: boolean
  request_type: number
  service_tier?: string
  reasoning_effort?: string
  billing_mode?: string
  /** 不含缓存的新鲜输入（sub2api 互斥桶） */
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  cache_creation_5m_tokens?: number
  cache_creation_1h_tokens?: number
  image_output_tokens?: number
  /** 推理 token（含在 output 计费内，仅展示） */
  reasoning_tokens?: number
  input_cost: number
  output_cost: number
  cache_creation_cost: number
  cache_read_cost: number
  image_output_cost?: number
  total_cost: number
  actual_cost: number
  account_stats_cost?: number
  rate_multiplier: number
  billing_rate_multiplier: number
  account_rate_multiplier?: number
  stream: boolean
  status_code: number
  success: boolean
  /** 短摘要 */
  error_message?: string
  /** transport | http | config | internal */
  error_type?: string
  /** 人类可读详情（含 method/url/status） */
  error_detail?: string
  upstream_url?: string
  /** 上游原始错误 body（截断） */
  upstream_error_body?: string
  /** 上游响应头 JSON（脱敏） */
  upstream_error_headers?: string
  duration_ms: number
  first_token_ms?: number | null
  ip_address?: string
  user_agent?: string
  created_at: string
  /** 列表 enrich 字段 */
  gateway_key_name?: string
  gateway_group_name?: string
  channel_name?: string
  source_group_name?: string
  source_api_key_name?: string
}

export interface GatewayUsagePage {
  items: GatewayUsageLog[]
  total: number
  page: number
  page_size: number
  pages: number
  sum_cost: number
}

export interface GatewayUsageStats {
  total_requests: number
  success_count: number
  error_count: number
  total_input_tokens: number
  total_output_tokens: number
  total_cache_creation_tokens: number
  total_cache_read_tokens: number
  total_tokens: number
  total_cost: number
  total_actual_cost: number
  average_duration_ms: number
  /** 近 5 分钟平均每分钟请求数 */
  rpm?: number
  /** 近 5 分钟平均每分钟 Token 数（input+output） */
  tpm?: number
  endpoints: { endpoint: string; requests: number }[]
}

/** 使用记录模型下拉：后端按 requested_model 聚合 */
export interface GatewayUsageModelOption {
  model: string
  count: number
}

export interface ModelPriceOverride {
  id: number
  model_name: string
  billing_mode: "token" | "per_request" | "image" | "video"
  input_price_per_token: number
  output_price_per_token: number
  cache_creation_price_per_token: number
  cache_read_price_per_token: number
  per_request_price: number
  pricing_tiers_json: string
  image_price_1k: number
  image_price_2k: number
  image_price_4k: number
  video_price_480p: number
  video_price_720p: number
  video_price_1080p: number
  enabled: boolean
  created_at: string
  updated_at: string
}

/** 内置默认价（只读），字段与覆盖表 per-token 一致 */
export interface ModelDefaultPrice {
  model_name: string
  input_price_per_token: number
  output_price_per_token: number
  cache_creation_price_per_token: number
  cache_read_price_per_token: number
}
