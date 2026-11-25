import test from "node:test";
import assert from "node:assert/strict";
import { probeHealth, request, setSessionActive, onSessionLost } from "./api.js";
import { validateApiBase } from "./desktop.js";

test("probeHealth reports ok when both /health and /ready return JSON ok/ready", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (path) => {
    if (path === "/health") return new Response(JSON.stringify({ status: "ok" }), { status: 200 });
    if (path === "/ready") return new Response(JSON.stringify({ status: "ready" }), { status: 200 });
    return new Response("not found", { status: 404 });
  };
  try {
    const res = await probeHealth();
    assert.equal(res.apiState, "ok");
    assert.equal(res.readyState, "ok");
    assert.equal(res.alive, true);
    assert.equal(res.ready, true);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("probeHealth keeps API online when /ready returns 503 not_ready", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (path) => {
    if (path === "/health") return new Response(JSON.stringify({ status: "ok" }), { status: 200 });
    if (path === "/ready") return new Response(JSON.stringify({ error: "not_ready" }), { status: 503 });
    return new Response("not found", { status: 404 });
  };
  try {
    const res = await probeHealth();
    assert.equal(res.apiState, "ok");
    assert.equal(res.readyState, "unready");
    assert.equal(res.alive, true);
    assert.equal(res.ready, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("probeHealth flags HTML fallback 200 responses as probe_failed", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response("<!doctype html><html></html>", { status: 200, headers: { "Content-Type": "text/html" } });
  try {
    const res = await probeHealth();
    assert.equal(res.apiState, "probe_failed");
    assert.equal(res.readyState, "probe_failed");
    assert.equal(res.alive, false);
    assert.equal(res.ready, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("probeHealth reports offline on network error", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => {
    throw new TypeError("fetch failed");
  };
  try {
    const res = await probeHealth();
    assert.equal(res.apiState, "offline");
    assert.equal(res.readyState, "offline");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("probeHealth forwards AbortSignal to fetch", async () => {
  const originalFetch = globalThis.fetch;
  const controller = new AbortController();
  const signals = [];
  globalThis.fetch = async (_path, init) => {
    signals.push(init && init.signal);
    return new Response(JSON.stringify({ status: "ok" }), { status: 200 });
  };
  try {
    await probeHealth({ signal: controller.signal });
    assert.deepEqual(signals, [controller.signal, controller.signal]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("validateApiBase restricts desktop API base to local loopback ports", () => {
  assert.equal(validateApiBase("http://127.0.0.1:18080"), "http://127.0.0.1:18080");
  assert.equal(validateApiBase(" http://localhost:18081/ "), "http://localhost:18081");
  for (const bad of [
    "",
    "https://evil.example",
    "http://192.168.1.5:18080",
    "http://user:pass@127.0.0.1:18080",
    "http://127.0.0.1:18080/gm",
    "http://127.0.0.1:9090",
    "http://127.0.0.1:18080/?x=1",
  ]) {
    assert.throws(() => validateApiBase(bad), (err) => err && err.code === "invalid_api_base", `accepted ${bad}`);
  }
});

test("web requests include same-origin credentials and CSRF header on writes", async () => {
  const originalFetch = globalThis.fetch;
  let capturedInit = null;
  let lost = 0;
  globalThis.fetch = async (_path, init) => {
    capturedInit = init;
    return new Response(JSON.stringify({ error: "gm_unauthorized" }), { status: 401 });
  };
  setSessionActive(true);
  onSessionLost(() => lost++);
  try {
    await assert.rejects(request("/gm/logout", { method: "POST" }), (err) => err && err.status === 401);
    assert.equal(capturedInit.credentials, "same-origin");
    assert.equal(capturedInit.headers["X-MiniVerse-CSRF"], "1");
    assert.equal(lost, 1);
  } finally {
    globalThis.fetch = originalFetch;
    setSessionActive(false);
    onSessionLost(() => {});
  }
});
