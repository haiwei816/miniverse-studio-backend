const ERROR_TEXT = {
  invalid_credentials: "账号或密码不正确",
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
