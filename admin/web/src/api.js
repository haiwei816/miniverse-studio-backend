// Thin transport: one place that knows about the GM bearer token, session
// expiry and the optional demo fixtures (?demo=1) used for UI work offline.

import { isDesktop, desktopFetch } from "./desktop.js";

let token = "";
let hasSession = false;
let unauthorized = () => {};
let demo = null;

export class ApiError extends Error {
  constructor(code, status) {
    super(code);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

export function setToken(value) {
  token = value || "";
}

export function setSessionActive(active) {
  hasSession = Boolean(active);
}

export function getToken() {
  return token;
}

export function onSessionLost(fn) {
  unauthorized = fn;
}

export function installDemo(handler) {
  demo = handler;
}

export async function request(path, { method = "GET", body, expectJson = false, signal, skipUnauthorized = false } = {}) {
  if (demo) return demo(path, { method, body });
  const upperMethod = String(method || "GET").toUpperCase();
  const desktop = isDesktop();
  const headers = {
    "Content-Type": "application/json",
    ...(token ? { Authorization: "Bearer " + token } : {}),
    ...(!desktop && upperMethod !== "GET" && upperMethod !== "HEAD" ? { "X-MiniVerse-CSRF": "1" } : {}),
  };
  let response;
  try {
    // Desktop shell: the request goes through Rust, so no CORS preflight and
    // the API stays on 127.0.0.1 only. Web build uses same-origin HttpOnly cookies.
    response = desktop
      ? await desktopFetch(path, {
          method: upperMethod,
          headers,
          body: body === undefined ? undefined : JSON.stringify(body),
          signal,
        })
      : await fetch(path, {
          method: upperMethod,
          credentials: "same-origin",
          headers,
          body: body === undefined ? undefined : JSON.stringify(body),
          signal,
        });
  } catch (e) {
    if (e && e.name === "AbortError") throw e;
    throw new ApiError("network_error", 0);
  }
  let payload = null;
  try {
    payload = await response.json();
  } catch {
    payload = null;
  }
  if (!response.ok) {
    if (response.status === 401 && !skipUnauthorized && (token || hasSession)) unauthorized();
    throw new ApiError(payload && payload.error ? payload.error : "http_" + response.status, response.status);
  }
  if (expectJson && (!payload || typeof payload !== "object")) {
    throw new ApiError("probe_invalid_response", response.status);
  }
  return payload;
}

export async function probeHealth({ signal } = {}) {
  const [h, r] = await Promise.allSettled([
    request("/health", { expectJson: true, signal }),
    request("/ready", { expectJson: true, signal }),
  ]);
  if (h.status === "rejected" && h.reason && h.reason.name === "AbortError") throw h.reason;
  if (r.status === "rejected" && r.reason && r.reason.name === "AbortError") throw r.reason;
  let apiState = "probe_failed";
  let apiDetail = "";
  if (h.status === "fulfilled") {
    if (h.value && (h.value.status === "ok" || h.value.ok === true)) {
      apiState = "ok";
    } else {
      apiState = "probe_failed";
      apiDetail = "健康端点返回格式异常";
    }
  } else {
    const err = h.reason;
    if (err && err.code === "network_error") {
      apiState = "offline";
      apiDetail = "无法连接 API 服务";
    } else {
      apiState = "probe_failed";
      apiDetail = err && err.code === "probe_invalid_response" ? "未到达 API 探针（返回非 JSON）" : `探测异常（${(err && err.code) || "unknown"}）`;
    }
  }

  let readyState = "probe_failed";
  let readyDetail = "";
  if (r.status === "fulfilled") {
    if (r.value && (r.value.status === "ready" || r.value.ok === true)) {
      readyState = "ok";
    } else {
      readyState = "probe_failed";
      readyDetail = "就绪端点返回格式异常";
    }
  } else {
    const err = r.reason;
    if (err && (err.status === 503 || err.code === "not_ready")) {
      readyState = "unready";
      readyDetail = "数据库或迁移尚未就绪";
    } else if (err && err.code === "network_error") {
      readyState = apiState === "offline" ? "offline" : "probe_failed";
      readyDetail = apiState === "offline" ? "API 服务离线" : "就绪探测网络失败";
    } else {
      readyState = "probe_failed";
      readyDetail = err && err.code === "probe_invalid_response" ? "未到达就绪探针（返回非 JSON）" : `探测异常（${(err && err.code) || "unknown"}）`;
    }
  }

  return {
    apiState,
    apiDetail,
    readyState,
    readyDetail,
    alive: apiState === "ok",
    ready: readyState === "ok",
    checked: Math.floor(Date.now() / 1000),
  };
}
