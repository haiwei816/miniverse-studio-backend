// Isolated games never receive platform or game bearer tokens. Every request
// goes through the parent's exact-source, channel-bound, endpoint allowlist.
(() => {
  const pending = new Map();
  let identity = null, readyResolve;
  const ready = new Promise(resolve => { readyResolve = resolve; });
  const errors = { revision_conflict:'云端进度已经更新。为保留双方进度，请先读取云端再继续。', companion_context_changed:'伙伴已进入另一个世界，请返回平台重新进入。', rate_limited:'请求过于频繁，请稍后再试。', unauthorized:'游戏会话已过期，请返回平台重新进入。', game_session_expired:'游戏会话已过期，请返回平台重新进入。', network_unavailable:'暂时无法连接服务，进度尚未同步。' };
  window.addEventListener('message', event => {
    if(event.source !== parent || event.origin === 'null') return;
    const data=event.data;
    if(data?.type==='miniverse:launch' && !identity && typeof data.channel==='string') { identity=data; readyResolve(data); parent.postMessage({type:'miniverse:started',channel:identity.channel},'*'); }
    else if(data?.type==='miniverse:response' && data.channel===identity?.channel && pending.has(data.id)) {
      const {resolve,reject,timer}=pending.get(data.id); clearTimeout(timer); pending.delete(data.id);
      if(data.status>=200&&data.status<300) resolve(data.data);
      else { const error=new Error(errors[data.data?.error]||`请求失败（${data.data?.error||data.status}）`); error.status=data.status; error.code=data.data?.error; reject(error); }
    }
  });
  parent.postMessage({type:'miniverse:ready'},'*');
  setTimeout(()=> { if(!identity) { const node=document.getElementById('status'); if(node) { node.textContent='请从 MiniVerse 玩家平台进入此游戏。'; node.dataset.state='error'; } } },8000);
  async function api(path,method='GET',body) {
    await ready;
    const id=crypto.randomUUID();
    return new Promise((resolve,reject)=> {
      const timer=setTimeout(()=>{pending.delete(id);reject(new Error('请求超时，请返回平台重试。'));},50000);
      pending.set(id,{resolve,reject,timer});
      parent.postMessage({type:'miniverse:request',channel:identity.channel,id,path,method,body},'*');
    });
  }
  async function checksum(text) { const bytes=await crypto.subtle.digest('SHA-256',new TextEncoder().encode(text)); return Array.from(new Uint8Array(bytes),b=>b.toString(16).padStart(2,'0')).join(''); }
  function status(text,state='') {const node=document.getElementById('status');node.textContent=text;node.dataset.state=state;}
  let saveRevision=0,saveConflict=false,queue=Promise.resolve(),savePending=0;
  async function load() {
    try {const data=await api('/saves/web-main');saveRevision=data.revision;saveConflict=false; status('云端进度已读取','ok');return data.deleted?null:JSON.parse(data.data);}
    catch(error){if(error.code==='save_not_found'){saveRevision=0;saveConflict=false;status('新冒险 · 操作后自动保存','ok');return null;}throw error;}
  }
  function save(state) {
    const data=JSON.stringify(state);savePending++;status('正在同步进度…');
    const task=queue.then(async()=>{if(saveConflict)throw new Error('存在云端冲突，请先读取云端。');const result=await api('/saves/web-main','PUT',{revision:saveRevision,schema_version:1,data,checksum:await checksum(data),encoding:'identity'});saveRevision=result.revision;});
    queue=task.catch(error=>{if(error.status===409)saveConflict=true;throw error;}).catch(()=>{});
    return task.then(()=>{if(--savePending===0)status('进度已保存到云端','ok');},error=>{savePending--;status(error.message,'error');throw error;});
  }
  async function context(scene,stage) {return api('/companion/context','PUT',{epoch:identity.epoch,scene_id:scene,quest_stage:stage,visible_targets:[]});}
  function event(type){return api('/events','POST',{event_id:crypto.randomUUID(),event_type:type,amount:1});}
  async function setupHint(){const button=document.getElementById('hint');if(!button)return;button.addEventListener('click',async()=>{button.disabled=true;const output=document.getElementById('hint-text');output.textContent='伙伴正在想一想…';try{const result=await api('/companion/chat','POST',{message:'请给我一个不直接揭晓答案的小提示。'});output.textContent=`${result.mode==='demo'?'[演示建议] ':''}${result.reply}`;}catch(error){output.textContent=error.message;}finally{button.disabled=false;}});}
  window.MiniVerseGame={ready,api,load,save,context,event,status,setupHint};
})();
