// Thin transport: one place that knows about the GM bearer token, session
// expiry and the optional demo fixtures (?demo=1) used for UI work offline.

import { isDesktop, desktopFetch } from "./desktop.js";

let token = "";
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

export function getToken() {
  return token;
}

export function onSessionLost(fn) {
  unauthorized = fn;
}

export function installDemo(handler) {
  demo = handler;
}

export async function request(path, { method = "GET", body } = {}) {
  if (demo) return demo(path, { method, body });
  let response;
  try {
    // Desktop shell: the request goes through Rust, so no CORS preflight and
    // the API stays on 127.0.0.1 only. Web build keeps the same-origin fetch.
    response = isDesktop()
      ? await desktopFetch(path, {
          method,
          headers: {
            "Content-Type": "application/json",
            ...(token ? { Authorization: "Bearer " + token } : {}),
          },
          body: body === undefined ? undefined : JSON.stringify(body),
        })
      : await fetch(path, {
          method,
          headers: {
            "Content-Type": "application/json",
            ...(token ? { Authorization: "Bearer " + token } : {}),
          },
          body: body === undefined ? undefined : JSON.stringify(body),
        });
  } catch {
    throw new ApiError("network_error", 0);
  }
  let payload = null;
  try {
    payload = await response.json();
  } catch {
    payload = null;
  }
  if (!response.ok) {
    if (response.status === 401 && token) unauthorized();
    throw new ApiError(payload && payload.error ? payload.error : "http_" + response.status, response.status);
  }
  return payload;
}
