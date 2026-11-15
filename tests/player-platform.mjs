import assert from 'node:assert/strict';
import {createHash, randomBytes} from 'node:crypto';
import {base} from './client.mjs';

async function req(path, method='GET', body, token, status=200) {
  const response=await fetch(base+path,{method,headers:{'Content-Type':'application/json',...(token?{Authorization:'Bearer '+token}:{})},body:body===undefined?undefined:JSON.stringify(body)});
  const value=await response.json();
  assert.equal(response.status,status,`${method} ${path}: ${JSON.stringify(value)}`);
  return value;
}
const username='platform_'+Date.now(),password='Integration-password-7';
const account=await req('/v1/platform/auth/register','POST',{username,password});
await req('/v1/platform/auth/register','POST',{username,password},undefined,409);
await req('/v1/platform/auth/login','POST',{username,password:'bad-pass'},undefined,401);
const session=await req('/v1/platform/auth/login','POST',{username,password}), token=session.access_token;
assert.equal((await req('/v1/platform/me','GET',undefined,token)).player_id,account.player_id);
const games=(await req('/v1/platform/games')).items;
assert.ok(games.some(g=>g.game_id==='sky-island'));
const firstPage=await req('/v1/platform/games?limit=1');
assert.equal(firstPage.items.length,1);assert.ok(firstPage.next_cursor);assert.ok(firstPage.categories.includes('探索'));
const secondPage=await req('/v1/platform/games?limit=1&cursor='+encodeURIComponent(firstPage.next_cursor));
assert.equal(secondPage.items.length,1);assert.notEqual(secondPage.items[0].game_id,firstPage.items[0].game_id);
await req('/v1/platform/games?limit=101','GET',undefined,undefined,400);
const game=await req('/v1/platform/games/sky-island');
assert.equal(game.entry_url,'/games/sky-island/');
await req('/v1/platform/library/sky-island','PUT',{favorite:true},token);
assert.ok((await req('/v1/platform/library','GET',undefined,token)).items.find(g=>g.game_id==='sky-island').favorite);
const verifier=randomBytes(48).toString('base64url'),code_challenge=createHash('sha256').update(verifier).digest('base64url');
const launch=await req('/v1/platform/games/sky-island/launch','POST',{code_challenge},token);
await req('/v1/platform/launch/exchange','POST',{launch_code:launch.launch_code,game_id:'card-game',code_verifier:verifier},undefined,401);
await req('/v1/platform/launch/exchange','POST',{launch_code:launch.launch_code,game_id:'sky-island',code_verifier:'a'.repeat(64)},undefined,401);
let gameSession=await req('/v1/platform/launch/exchange','POST',{launch_code:launch.launch_code,game_id:'sky-island',code_verifier:verifier});
await req('/v1/platform/launch/exchange','POST',{launch_code:launch.launch_code,game_id:'sky-island',code_verifier:verifier},undefined,401);
await req('/v1/games/sky-island/profile','GET',undefined,gameSession.access_token);
await req('/v1/games/card-game/profile','GET',undefined,gameSession.access_token,403);
await req('/v1/platform/me','GET',undefined,gameSession.access_token,401);
await req('/v1/platform/library','GET',undefined,gameSession.access_token,401);
assert.equal(gameSession.expires_at,session.expires_at);
const nextLaunch=await req('/v1/platform/games/sky-island/launch','POST',{code_challenge},token);
assert.ok(nextLaunch.epoch>launch.epoch);
await req('/v1/games/sky-island/profile','GET',undefined,gameSession.access_token,401);
const latestLaunch=await req('/v1/platform/games/sky-island/launch','POST',{code_challenge},token);
await req('/v1/platform/launch/exchange','POST',{launch_code:nextLaunch.launch_code,game_id:'sky-island',code_verifier:verifier},undefined,401);
gameSession=await req('/v1/platform/launch/exchange','POST',{launch_code:latestLaunch.launch_code,game_id:'sky-island',code_verifier:verifier});
assert.equal(gameSession.epoch,latestLaunch.epoch);
const library=(await req('/v1/platform/library','GET',undefined,token)).items;
assert.ok(library.find(g=>g.game_id==='sky-island').last_played_at>0);
await req('/v1/platform/auth/logout','POST',{},token);
await req('/v1/games/sky-island/profile','GET',undefined,gameSession.access_token,401);
await req('/v1/platform/me','GET',undefined,token,401);

const admin=await req('/gm/login','POST',{username:'admin',password:'sandbox-admin'}),reviewer=await req('/gm/login','POST',{username:'reviewer',password:'sandbox-reviewer'});
const listing='/gm/games/sky-island/listing',original=await req('/v1/platform/games/sky-island');
try {
  await req(listing,'PUT',{...original,entry_url:'/games/'+'a'.repeat(249)+'/'},admin.access_token,400);
  await req(listing,'PUT',{...original,summary:'Integration draft — not published'},admin.access_token);
  assert.notEqual((await req('/v1/platform/games/sky-island')).summary,'Integration draft — not published');
  const seen=await req(listing,'GET',undefined,reviewer.access_token);
  assert.match(seen.draft_hash,/^[a-f0-9]{64}$/);
  await req(listing+'/submit','POST',{},admin.access_token,400);
  // A second editor replaces the draft after the review page was opened.
  await req(listing,'PUT',{...original,summary:'Integration replacement — latest draft'},admin.access_token);
  await req(listing+'/submit','POST',{draft_hash:seen.draft_hash},admin.access_token,409);
  const latest=await req(listing,'GET',undefined,reviewer.access_token),review={draft_hash:latest.draft_hash};
  assert.notEqual(latest.draft_hash,seen.draft_hash);
  await req(listing+'/submit','POST',review,admin.access_token);
  await req(listing+'/publish','POST',review,admin.access_token,403);
  await req(listing+'/publish','POST',{draft_hash:seen.draft_hash},reviewer.access_token,409);
  assert.equal((await req('/v1/platform/games/sky-island')).summary,original.summary);
  await req(listing+'/publish','POST',review,reviewer.access_token);
  assert.equal((await req('/v1/platform/games/sky-island')).summary,'Integration replacement — latest draft');
  await req(listing+'/unpublish','POST',{},reviewer.access_token);
  await req('/v1/platform/games/sky-island','GET',undefined,undefined,404);
} finally {
  await req(listing,'PUT',original,admin.access_token);
  const saved=await req(listing,'GET',undefined,admin.access_token),review={draft_hash:saved.draft_hash};
  await req(listing+'/submit','POST',review,admin.access_token);
  await req(listing+'/publish','POST',review,reviewer.access_token);
}
console.log('Player platform: register/login, catalog/library, PKCE one-time launch, game isolation, parent revocation and version-bound independent publication: PASS');
