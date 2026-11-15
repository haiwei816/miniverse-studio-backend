import test from 'node:test';
import assert from 'node:assert/strict';
import { request, setToken, onSessionLost } from './api.js';

test('a delayed 401 from an old account cannot sign out the new account', async () => {
  const originalFetch=globalThis.fetch;
  let complete, lost=0, captured;
  globalThis.fetch=async (_path, options)=>{captured=options.headers.Authorization;return new Promise(resolve=>complete=resolve);};
  onSessionLost(()=>lost++);
  try {
    setToken('account-a');
    const old=request('/v1/platform/library');
    setToken('account-b');
    complete(new Response(JSON.stringify({error:'unauthorized'}),{status:401}));
    await assert.rejects(old,/请先登录/);
    assert.equal(captured,'Bearer account-a');
    assert.equal(lost,0);
  } finally {globalThis.fetch=originalFetch;setToken('');onSessionLost(()=>{});}
});

test('a current-token 401 invalidates the current session exactly once', async () => {
  const originalFetch=globalThis.fetch;
  let lost=0;
  globalThis.fetch=async()=>new Response(JSON.stringify({error:'unauthorized'}),{status:401});
  setToken('current-account');
  onSessionLost(()=>{lost++;setToken('');});
  try {await assert.rejects(request('/v1/platform/me'));assert.equal(lost,1);}
  finally {globalThis.fetch=originalFetch;setToken('');onSessionLost(()=>{});}
});

test('anonymous authentication failures do not invalidate a signed-in session', async () => {
  const originalFetch=globalThis.fetch;
  let lost=0;
  globalThis.fetch=async (_path,options)=>{assert.equal(options.headers.Authorization,undefined);return new Response(JSON.stringify({error:'invalid_credentials'}),{status:401});};
  setToken('signed-in');onSessionLost(()=>lost++);
  try {await assert.rejects(request('/v1/platform/auth/login','POST',{}, {anonymous:true}));assert.equal(lost,0);}
  finally {globalThis.fetch=originalFetch;setToken('');onSessionLost(()=>{});}
});
