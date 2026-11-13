// Desktop shell bridge. The Tauri build exposes its JS APIs on
// window.__TAURI__ (withGlobalTauri), so the web build carries no Tauri
// dependency and the same files run in both places.
const BASE_KEY = "miniverse.api";

export function shell() {
  return typeof window !== "undefined" ? window.__TAURI__ : null;
}

export function isDesktop() {
  return !!shell();
}

export function apiBase() {
  const configured = localStorage.getItem(BASE_KEY);
  if (configured) return configured.replace(/\/$/, "");
  return "http://127.0.0.1:18080";
}

export function setApiBase(value) {
  localStorage.setItem(BASE_KEY, value.replace(/\/$/, ""));
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
