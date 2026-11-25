const ERROR_TEXT = {
  invalid_credentials: "账号或密码不正确",
  auth_rate_limited: "登录尝试过于频繁，请稍后再试",
  auth_busy: "认证服务繁忙，请稍后重试",
  sandbox_admin_disabled: "当前环境已禁用本地测试管理员账号",
  invalid_api_base: "桌面端仅允许连接本机 API 地址（127.0.0.1 / localhost 的 18080 或 18081 端口）",
  gm_unauthorized: "登录已过期，请重新登录",
  game_forbidden: "当前账号没有这款游戏的权限",
  permission_denied: "角色权限不足",
  reason_required: "请填写原因 / 工单（至少 3 个字符）",
  request_conflict: "同一申请 ID 的载荷不同，请换一个新的申请 ID",
  player_not_found: "未找到该玩家",
  activity_incomplete: "活动尚未完成，不能补发",
  insufficient_inventory: "玩家库存不足",
  unknown_item: "道具不存在",
  rate_limited: "操作过于频繁，请稍后再试",
  network_error: "网络不可用，请检查服务是否启动",
  invalid_json: "请求格式不正确",
  trailing_json: "请求里有多余内容",
  game_not_found: "游戏不存在或已停用",
  independent_review_required: "需要由另一位有审核权限的成员发布",
  listing_not_submitted: "请先保存并提交草稿，再进行审核",
  listing_draft_required: "请先保存游戏资料草稿",
  invalid_draft_hash: "请刷新后重新提交或审核草稿",
  listing_draft_changed: "草稿已被修改，请刷新并重新核对内容",
  listing_not_found: "尚未创建游戏发行资料",
  invalid_listing: "请检查名称、分类、版本和网页入口路径",
  invalid_listing_accent: "请选择有效的封面主题",
  invalid_listing_icon: "封面符号需为 1–4 个字符",
  invalid_companion_level: "请选择有效的伙伴接入等级",
  probe_invalid_response: "探测接口未返回有效 JSON（请检查代理配置）",
  not_ready: "数据库或迁移尚未就绪",
  ops_agent_not_configured: "当前环境未配置智能排障 AI 模型服务",
  ops_agent_unavailable: "智能排障模型服务暂时不可用（请检查地址、网络或 API Key）",
  ops_agent_configuration_invalid: "智能排障模型配置无效",
  ops_agent_invalid_response: "模型接口未返回标准 OpenAI Chat Completions 响应",
  ops_agent_busy: "智能排障并发请求过多，请稍后重试",
  invalid_ops_agent_url: "请填写合法的接口地址（生产环境仅允许 https://，沙盒允许 http://）",
  ops_agent_ssrf_blocked: "目标模型地址位于受限私网或元数据网段，已被安全策略拦截",
  invalid_ops_agent_protocol: "不支持的接口协议，请选择 openai 或 anthropic",
  ops_agent_model_required: "配置外部接口时必须填写模型名称（Model）",
  csrf_rejected: "安全校验未通过，请刷新页面后重试",
  forbidden: "仅 admin 管理员可执行此操作",
  invalid_activity_stop: "请填写有效的活动 ID",
  activity_not_found: "未找到目标活动",
  mcp_tool_forbidden: "当前角色无权调用该 MCP 工具",
  invalid_question: "请输入 2–1000 个字符的排障问题描述",
  match_ticket_required: "该榜单要求先获取服务端对局票据",
  match_ticket_not_found: "对局票据不存在",
  invalid_match_ticket: "对局票据与当前游戏、玩家或榜单不匹配",
  match_ticket_consumed: "该对局票据已被使用，无法重复提交",
  match_ticket_expired: "对局票据已过期，请重新开局",
  seed_mismatch: "回放随机种子与服务端下发票据不一致",
  account_suspended: "账号已被封禁或冻结",
  board_not_found: "未找到目标排行榜或赛季",
  season_still_open: "当前赛季尚未结束，无法结算",
  season_already_settled: "该赛季已完成结算",
  invalid_board: "排行榜配置参数无效",
  invalid_ban_expiry: "封禁到期时间必须晚于当前时间",
  ban_not_found: "该玩家当前未被封禁",
};

export function errorText(code) {
  if (!code) return "操作失败";
  return ERROR_TEXT[code] || code;
}

const pad = (n) => String(n).padStart(2, "0");

export function stamp(seconds) {
  if (!seconds) return "—";
  const d = new Date(seconds * 1000);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  const time = pad(d.getHours()) + ":" + pad(d.getMinutes());
  if (sameDay) return time;
  const sameYear = d.getFullYear() === now.getFullYear();
  return (sameYear ? "" : d.getFullYear() + "-") + pad(d.getMonth() + 1) + "-" + pad(d.getDate()) + " " + time;
}

export function relative(seconds) {
  if (!seconds) return "—";
  const diff = Math.floor(Date.now() / 1000 - seconds);
  if (diff < 45) return "刚刚";
  if (diff < 3600) return Math.floor(diff / 60) + " 分钟前";
  if (diff < 86400) return Math.floor(diff / 3600) + " 小时前";
  if (diff < 86400 * 30) return Math.floor(diff / 86400) + " 天前";
  return stamp(seconds);
}

export function count(value) {
  if (value === null || value === undefined) return "—";
  return Number(value).toLocaleString("zh-CN");
}

export function dayLabel(iso) {
  if (!iso) return "";
  const [, month, day] = iso.split("-");
  return month + "/" + day;
}

const REQUEST_STATE = {
  pending: ["待审核", "warn"],
  executed: ["已执行", "ok"],
  cancelled: ["已撤销", "info"],
  approved: ["已批准", "ok"],
};

export function requestState(state) {
  const found = REQUEST_STATE[state];
  return found ? { label: found[0], tone: found[1] } : { label: state || "—", tone: "info" };
}

const ACTIONS = {
  request_created: "创建申请",
  request_approve: "批准执行",
  request_cancel: "撤销申请",
  announcement_updated: "更新公告",
  activity_stopped: "停止活动",
  activity_published: "发布活动",
  reward_granted: "补发奖励",
  operation_failed: "操作失败",
  player_anomaly: "玩家异常",
  listing_draft: "更新发行草稿",
  listing_submit: "提交游戏审核",
  listing_publish: "发布游戏",
  listing_unpublish: "下架游戏",
  ops_agent_diagnose: "AI 智能排障",
  ops_agent_config_updated: "更新排障模型配置",
  player_banned: "封禁玩家账号",
  player_unbanned: "解除玩家封禁",
};

export function actionLabel(action) {
  return ACTIONS[action] || action || "—";
}

export function isFailure(action) {
  return action === "operation_failed" || action === "player_anomaly";
}

export function ago(seconds) {
  if (!seconds) return "从未";
  const diff = Math.max(0, Math.floor(Date.now() / 1000 - seconds));
  if (diff < 60) return "刚刚";
  if (diff < 3600) return Math.floor(diff / 60) + " 分钟前";
  if (diff < 86400) return Math.floor(diff / 3600) + " 小时前";
  return Math.floor(diff / 86400) + " 天前";
}

export function healthBadges(health, loading) {
  if (loading && !health) {
    return { api: { tone: "info", label: "探测中" }, ready: { tone: "info", label: "探测中" } };
  }
  if (!health) {
    return { api: { tone: "warn", label: "未探测" }, ready: { tone: "warn", label: "未探测" } };
  }
  const api =
    health.apiState === "ok"
      ? { tone: "ok", label: "在线" }
      : health.apiState === "offline"
        ? { tone: "danger", label: "离线" }
        : { tone: "warn", label: "探测失败" };
  const ready =
    health.readyState === "ok"
      ? { tone: "ok", label: "就绪" }
      : health.readyState === "unready"
        ? { tone: "warn", label: "未就绪" }
        : health.readyState === "offline"
          ? { tone: "danger", label: "离线" }
          : { tone: "warn", label: "探测失败" };
  return { api, ready };
}
