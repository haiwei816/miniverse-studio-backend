import assert from'node:assert/strict';import{execFileSync}from'node:child_process';import{api,login}from'./client.mjs';
const a=await login(),t=a.access_token,id='memory-'+Date.now(),root='/v1/games/sky-island';
const sql=q=>execFileSync('docker',['compose','-f','deploy/compose.yaml','exec','-T','mysql','mysql','-uroot','-plocal-root-only','miniverse','-N','-e',q],{encoding:'utf8'});
await api(root+'/boards','PUT',{id,season:'test',mode:'max',starts_at:1,ends_at:4102444800,item:'wood',reward_amount:1},t);
try{
sql(`SET SESSION cte_max_recursion_depth=21000; INSERT INTO scores(game_id,board_id,season,player_id,score,reached_at) WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<20000) SELECT 'sky-island','${id}','test',CONCAT('fixture-',LPAD(n,5,'0')),n,n FROM seq; INSERT INTO scores VALUES('sky-island','${id}','test','${a.player_id}',10000,0);`);
const result=await api(root+`/boards/${id}/test`,'GET',undefined,t);assert.equal(result.top.length,50);assert.equal(result.top[0].score,20000);assert.equal(result.nearby.length,5);assert.equal(result.nearby.find(x=>x.player_id===a.player_id).rank,10001);assert.ok(Buffer.byteLength(JSON.stringify(result))<20000);
sql(`UPDATE boards SET ends_at=1 WHERE game_id='sky-island' AND id='${id}'`);await api(root+`/boards/${id}/test/settle`,'POST',{},t);assert.equal(Number(sql(`SELECT COUNT(*) FROM grants WHERE game_id='sky-island' AND business_key='season:${id}:test'`).trim()),3);
console.log('20001-entry board: Top50/nearby5, exact tie rank 10001, settlement creates only Top3 grants: PASS');
}finally{sql(`DELETE m FROM mail m JOIN grants g ON g.id=m.grant_id WHERE g.business_key='season:${id}:test'; DELETE FROM grants WHERE business_key='season:${id}:test'; DELETE FROM scores WHERE game_id='sky-island' AND board_id='${id}'; DELETE FROM boards WHERE game_id='sky-island' AND id='${id}';`)}
