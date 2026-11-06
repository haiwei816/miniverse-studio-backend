import assert from'node:assert/strict';import{api,login}from'./client.mjs';
export async function gm(name='admin'){return(await api('/gm/login','POST',{username:name,password:'sandbox-'+name})).access_token;}
const a=await login();await api('/v1/games/sky-island/profile','PATCH',{name:'gm-test'},a.access_token);const t=await gm('support');
const rows=await api('/gm/games/sky-island/players?q=gm-test','GET',undefined,t);assert.ok(rows.some(x=>x.player_id===a.player_id));
assert.equal((await api('/gm/games/sky-island/players/'+a.player_id,'GET',undefined,t)).name,'gm-test');
await api('/gm/games/not-authorized/players','GET',undefined,t,403);await api('/gm/login','POST',{username:'admin',password:'wrong'},undefined,401);await api('/gm/games/sky-island/players','GET',undefined,a.access_token,401);
console.log('S7 GM login, read role, game scope, player/grant/save queries: PASS');
