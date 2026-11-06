import assert from'node:assert/strict';import{execFileSync}from'node:child_process';import{api,login,base}from'./client.mjs';import{createHash}from'node:crypto';
const compose=(...args)=>execFileSync('docker',['compose','-f','deploy/compose.yaml',...args],{stdio:'pipe'});
async function waitReady(){for(let i=0;i<60;i++){try{if((await fetch(base+'/ready')).status===200)return}catch{}await new Promise(r=>setTimeout(r,500));}throw Error('readiness recovery timed out')}
const a=await login(),t=a.access_token,p='/v1/games/sky-island/saves/restart',data='{"persistent":42}';await api(p,'PUT',{revision:0,schema_version:1,data,checksum:createHash('sha256').update(data).digest('hex')},t);
compose('restart','api');await waitReady();assert.equal((await api(p,'GET',undefined,t)).data,data);
try{compose('stop','mysql');assert.equal((await fetch(base+'/health')).status,200);assert.equal((await fetch(base+'/ready')).status,503);}finally{compose('start','mysql');await waitReady()}
assert.equal((await api(p,'GET',undefined,t)).data,data);
try{compose('--profile','rules','stop','rulecore');await api('/v1/games/card-game/replays/validate','POST',{version:'card-v1',seed:123,actions:[2]},t,503);assert.equal((await fetch(base+'/ready')).status,200);assert.equal((await api(p,'GET',undefined,t)).data,data);}finally{compose('--profile','rules','start','rulecore')}
console.log('Recovery: API restart preserves session/save; MySQL health split/recovery; RuleCore outage fails closed and ordinary APIs remain available: PASS');
