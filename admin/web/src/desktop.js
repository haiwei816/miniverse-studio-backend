// Desktop shell bridge. The Tauri build exposes its JS APIs on
// window.__TAURI__ (withGlobalTauri), so the web build carries no Tauri
// dependency and the same files run in both places.
const BASE_KEY = "miniverse.api";
const DEFAULT_API_BASE = "http://127.0.0.1:18080";

export function validateApiBase(raw) {
  const fail = () => {
    const err = new Error("invalid_api_base");
    err.code = "invalid_api_base";
    throw err;
  };
  if (typeof raw !== "string") fail();
  const trimmed = raw.trim();
  if (!trimmed) fail();
  let url;
  try {
    url = new URL(trimmed);
  } catch {
    fail();
  }
  if (
    url.protocol !== "http:" ||
    (url.hostname !== "127.0.0.1" && url.hostname !== "localhost") ||
    (url.port !== "18080" && url.port !== "18081") ||
    url.username ||
    url.password ||
    (url.pathname !== "" && url.pathname !== "/") ||
    url.search ||
    url.hash
  ) {
    fail();
  }
  return url.origin;
}

export function shell() {
  return typeof window !== "undefined" ? window.__TAURI__ : null;
}

export function isDesktop() {
  return !!shell();
}

export function apiBase() {
  if (typeof localStorage === "undefined") return DEFAULT_API_BASE;
  const configured = localStorage.getItem(BASE_KEY);
  if (!configured) return DEFAULT_API_BASE;
  try {
    return validateApiBase(configured);
  } catch {
    return DEFAULT_API_BASE;
  }
}

export function setApiBase(value) {
  const normalized = validateApiBase(value);
  if (typeof localStorage !== "undefined") {
    localStorage.setItem(BASE_KEY, normalized);
  }
  return normalized;
}

export async function desktopFetch(path, init) {
  const tauri = shell();
  return tauri.http.fetch(apiBase() + path, init);
}

export function onTrayAction(handler) {
  const tauri = shell();
  if (!tauri) return () => {};
  const unlisten = tauri.event.listen("gm://action", (event) => handler(event.payload));
  return () => unlisten.then((off) => off());
}

export async function notifyNative(title, body) {
  const tauri = shell();
  if (!tauri || !tauri.notification) return;
  const { isPermissionGranted, requestPermission, sendNotification } = tauri.notification;
  let granted = await isPermissionGranted();
  if (!granted) granted = (await requestPermission()) === "granted";
  if (granted) sendNotification({ title, body });
}

// Keeps the tray tooltip useful without a second round trip.
export async function setShellStatus(text) {
  const tauri = shell();
  if (!tauri || !tauri.app) return;
  try {
    await tauri.app.setBadgeCount?.(0);
  } catch {
    /* badge counts are optional per platform */
  }
  document.title = text ? "MiniVerse GM · " + text : "MiniVerse GM";
}
