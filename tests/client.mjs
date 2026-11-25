import assert from 'node:assert/strict';

export const base = process.env.API_URL || 'http://127.0.0.1:18080';

export async function api(path, method = 'GET', body, token, status = 200) {
  const r = await fetch(base + path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const v = await r.json();
  assert.equal(r.status, status, `${method} ${path}: ${JSON.stringify(v)}`);
  return v;
}

export const login = () => api('/v1/auth/guest', 'POST', {});
