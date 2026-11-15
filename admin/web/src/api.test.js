import test from "node:test";
import assert from "node:assert/strict";
import { probeHealth } from "./api.js";

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
