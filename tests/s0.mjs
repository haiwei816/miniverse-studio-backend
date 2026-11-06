import assert from 'node:assert/strict';
const base=process.env.API_URL||'http://127.0.0.1:18080';
for(const path of ['/health','/ready']){const r=await fetch(base+path);assert.equal(r.status,200);assert.ok(r.headers.get('x-request-id'));}
await Promise.all(Array.from({length:100},async()=>assert.equal((await fetch(base+'/health')).status,200)));
console.log('S0 HTTP health, readiness, request IDs, 100 concurrent requests: PASS');
