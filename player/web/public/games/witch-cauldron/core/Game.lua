local D=require "Data"
local B=D.balance
local R=B.rules
local BOARD_SIZE=B.boardSize or 21
-- 经济倍率：随夜数递增，收入 / 房租 / Boss 血量 / 初始金币同步放大，相对平衡不变。
-- 前期保持小数字（第 1 夜 ×1），后期租金涨到千万级；无尽模式封顶在 coinCapRound 夜。
local function coinMul(round)
 local e=B.economy
 if not e then return 1 end
 local n=math.max(1,math.min(round or 1,e.coinCapRound or 12))
 return (e.coinBase or 1)*((e.coinGrowth or 1)^(n-1))
end
local function uidKey(uid)
 local n=tonumber(uid)
 return n and n%1==0 and string.format("%.0f",n) or tostring(uid)
end
-- 21-slot circular board (3-5-5-5-3 rows centered in a 5x5 grid)
local COORDS={
 {x=1,y=0},{x=2,y=0},{x=3,y=0},
 {x=0,y=1},{x=1,y=1},{x=2,y=1},{x=3,y=1},{x=4,y=1},
 {x=0,y=2},{x=1,y=2},{x=2,y=2},{x=3,y=2},{x=4,y=2},
 {x=0,y=3},{x=1,y=3},{x=2,y=3},{x=3,y=3},{x=4,y=3},
 {x=1,y=4},{x=2,y=4},{x=3,y=4},
}
local G={};G.__index=G
function G.New(seed) local self=setmetatable({},G);self:Reset(seed);return self end
function G:Reset(seed)
 self.profile=B.id;self.rng=seed or math.random(1,2147483646);self.bag={};self.board={};self.relics={};self.uid=0
 self.coins=math.floor(B.startCoins*coinMul(1));self.round=1;self.paid=0;self.left=B.spins[1];self.turn=0;self.removal=0;self.reroll=0
 self.total=0;self.last=0;self.roundScores={};self.phase="ready";self.choices={};self.events={};self.values={};self.visualBoard={};self.won=false
 self.draftContext="spin";self.cardboardAge=0;self.lastOutcome=""
 self.blastChance=0;self.brewRewards=0;self.additiveUses={};self.relicContext="rent"
 self.temperature=0;self.selectedFuel=1;self.spinFuel=1;self.spinRaised=false;self.discoveryActive=false;self.selectedAdditive="";self.levels={};self.upgradeTokens=0;self.brewTally=0;self.familyBoost={};self.upgradeChoices={};self.rites={};self.pivotBonus=0;self.seenGroups={}
 self.mainline={};self.pendingMainline={};self.pickBias=nil
 self.roundEarned=0;self.boss=nil
 for _,id in ipairs({"rose","cat","milk","cultist","spade","amethyst"}) do self:Add(id) end
 self:ClearPot()
 local startSlots={6,10,11,12,16,7}
 for idx,s in ipairs(self.bag) do if startSlots[idx] then self.board[startSlots[idx]]=s end end
end
function G:ClearPot()
 self.incomeDetails=nil
 self.board={};for i=1,BOARD_SIZE do self.board[i]=false end
 self.visualBoard={};self.values={};self.drawn={};self.fresh={};self.buffSeen={}
 self.potValue=0;self.basePayout=0
 self.brewBoost=0;self.lostPayout=0;self.heat="";self.additive="";self.brewRiskAtOpen=0;self.blastChance=0
 self.brewRiskModifier=0;self.brewBoostModifier=0;self.brewRefund=0;self.pendingBrewReward=false;self.brewRewardOdds={}
 self.pendingMainline={}
end
function G:Restore()
 local legacy={low=0,medium=1,high=2}
 if type(self.temperature)~="number" then self.temperature=legacy[self.roundHeat] or legacy[self.heat] or 0 end
 self.temperature=math.max(0,math.floor(self.temperature))
 if self.selectedFuel~=0 and self.selectedFuel~=1 and self.selectedFuel~=2 then self.selectedFuel=1 end
 if self.spinFuel~=0 and self.spinFuel~=1 and self.spinFuel~=2 then self.spinFuel=1 end
 self.spinRaised=self.spinRaised==true;self.discoveryActive=self.discoveryActive==true
 self.pendingBrewReward=false
 if self.phase=="relic" and self.relicContext=="brew" then self.choices={};self:Deadline() end
 -- 升级改成手动选择；旧存档若停在升级阶段，退回可选状态重新出候选。
 if self.phase=="upgrade" then
  self.upgradeChoices=self:UpgradeChoices()
  if #self.upgradeChoices==0 then self.upgradeTokens=0;self.phase="relic";self.draftContext="rent";self:DrawChoices(true) end
 end
 -- The byproduct picker is retired; old saves resume at the rent reward step.
 if self.phase=="byproduct" then self.phase="reward";self.draftContext="rent" end
 self.selectedAdditive=B.brew.additives[self.selectedAdditive] and self.selectedAdditive or ""
 if type(self.levels)~="table" then self.levels={} end
 if type(self.familyBoost)~="table" then self.familyBoost={} end
 -- Older saves have no main line progress; start empty rather than failing the load.
 if type(self.mainline)~="table" then self.mainline={} end
 self.pendingMainline={};self.pickBias=nil
 if type(self.roundEarned)~="number" then self.roundEarned=0 end
 local scores={}
 if type(self.roundScores)=="table" then
  for key,value in pairs(self.roundScores) do
   local n,score=tonumber(key),tonumber(value)
   if n and score and n>=1 then scores[math.floor(n)]=math.max(0,math.floor(score)) end
  end
 end
 self.roundScores=scores
 self.upgradeTokens=math.max(0,math.floor(tonumber(self.upgradeTokens) or 0))
 self.brewTally=math.max(0,math.floor(tonumber(self.brewTally) or 0))
 -- JSON decodes references as separate tables; reconnect board entries to the bag.
 local byId={};for _,s in ipairs(self.bag) do byId[s.uid]=s end
 for i=1,BOARD_SIZE do local s=self.board[i];self.board[i]=s and byId[s.uid] or false end
 -- JSON numbers become doubles: 1 and 1.0 must identify the same material.
 if type(self.incomeDetails)=="table" and type(self.incomeDetails.byUid)=="table" then
  local normalized,slots={},{}
  for oldKey,entry in pairs(self.incomeDetails.byUid) do normalized[uidKey(entry.uid or oldKey)]=entry end
  for slot,key in pairs(self.incomeDetails.bySlot or {}) do slots[tonumber(slot) or slot]=uidKey(key) end
  self.incomeDetails.byUid=normalized;self.incomeDetails.bySlot=slots
  for _,instance in ipairs(self.bag) do
   local entry=normalized[uidKey(instance.uid)]
   if entry and entry.id==instance.id and (not instance.lastIncome or (instance.lastIncome.turn or -1)<entry.turn) then instance.lastIncome=entry end
  end
 end
end
function G:Multiplier() return (100+self.brewBoost)/100 end
-- Duplicate copies stack linearly: 1x base, +2x per copy, instead of 3^copies.
function G:MagnetMultiplier()
 local count=self:Count("magnet")
 if count<=0 then return 1 end
 return 1+(R.goldenCarrot-1)*count
end
function G:Estimate()
 if self.phase~="review" and self.phase~="brewed" then return 0 end
 return math.floor(self.basePayout*(100+self.brewBoost)/100)
end
function G:IsFresh(uid)
 for _,id in ipairs(self.fresh) do if id==uid then return true end end
 return false
end
function G:CanDraw()
 return self.phase=="ready" and self.left>0 and #self.bag>0
end
function G:Random(n)
 self.rng=(self.rng*48271)%2147483647
 if n then return math.floor(self.rng/2147483647*n)+1 end
 return self.rng/2147483647
end
function G:Add(id)
 assert(D.symbols[id],"unknown symbol "..tostring(id));self.uid=self.uid+1
 local s={id=id,uid=self.uid,growth=0,age=0,bank=0};self.bag[#self.bag+1]=s
 -- 灵活变通：第一次拿到某个流派的材料，永久提高全场产出
 local grp=D.symbols[id].group
 if grp then
  self.seenGroups=self.seenGroups or {}
  if not self.seenGroups[grp] then
   self.seenGroups[grp]=true
   local P=B.pivot
   if P and not self:Has(id) then self.pivotBonus=math.min(P.cap or 0.6,(self.pivotBonus or 0)+(P.gain or 0.06)) end
  end
 end
 return s
end
function G:Rent()
 local r=B.rents[self.round]
 if not r then
  local last=B.rents[#B.rents]
  r=math.floor(last*(B.endlessGrowth^(self.round-#B.rents))/50)*50
 end
 return math.floor(r*coinMul(self.round)*((B.economy and B.economy.rentBonus) or 1))
end
-- Current economy multiplier for this night (the UI uses it for pre-resolution card values).
function G:CoinMul(round) return coinMul(round or self.round) end
function G:Spins() return B.spins[self.round] or B.endlessSpins end
function G:Count(id) local n=self.relics[id];return n==true and 1 or (n or 0) end
-- 流派共鸣：场上同流派张数越多，该流派全体产出越高（前 2 张不结算）。
function G:GroupCount(grp)
 if not grp then return 0 end
 local n=0
 for i=1,BOARD_SIZE do local s=self.board[i];if s and D.symbols[s.id].group==grp then n=n+1 end end
 return n
end
function G:SetMultiplier(grp)
 local S=B.set;if not S or not grp or grp==0 then return 1 end
 local n=self:GroupCount(grp)
 if (self.round or 1)>=3 and n==1 then return S.orphan or 0.45 end
 if n<=(S.base or 0) then return 1 end
 return math.min(S.cap or 99,1+(S.step or 0)*(n-(S.base or 0)))
end
function G:RiteMultiplier(grp)
 if not grp or not self.rites then return 1 end
 return 1+(self.rites[grp] or 0)
end
function G:Has(id) return self:Count(id)>0 end
-- Main line potions: detected on the finished pot, committed only when the brew succeeds.
function G:MainlineSlots() return (B.mainline and B.mainline.slots) or {} end
function G:MainlineDone(slot) return self.mainline ~= nil and self.mainline[slot.id] ~= nil end
function G:MainlineCount()
 local n=0
 for _,slot in ipairs(self:MainlineSlots()) do if self:MainlineDone(slot) then n=n+1 end end
 return n
end
function G:DetectMainline()
 self.pendingMainline={}
 if not B.mainline then return end
 local have={}
 for i=1,BOARD_SIZE do local s=self.board[i];if s then have[s.id]=true end end
 for _,slot in ipairs(B.mainline.slots) do
  if not self:MainlineDone(slot) then
   local ok=true
   if slot.potion then
    if not have[slot.potion] then ok=false end
    if slot.minTemp and self.temperature<slot.minTemp then ok=false end
    if slot.maxTemp and self.temperature>slot.maxTemp then ok=false end
   elseif slot.needs then
    for _,id in ipairs(slot.needs) do if not have[id] then ok=false end end
   end
   if ok then self.pendingMainline[#self.pendingMainline+1]=slot.id end
  end
 end
end
function G:CommitMainline()
 local added={}
 for _,id in ipairs(self.pendingMainline or {}) do
  self.mainline=self.mainline or {}
  if self.mainline[id]==nil then self.mainline[id]=self.round;added[#added+1]=id end
 end
 self.pendingMainline={}
 return added
end
-- Guarantee path: bias candidate weights toward an unfilled slot's materials.
function G:MainlineBias()
 local M=B.mainline
 if not M or not M.assist or M.assist<=1 then return nil end
 local bias
 for _,slot in ipairs(M.slots) do
  if slot.materials and not self:MainlineDone(slot) and self.round>=(slot.assistFrom or math.huge) then
   for _,id in ipairs(slot.materials) do bias=bias or {};bias[id]=M.assist end
  end
 end
 return bias
end
-- Final night: shields come from the potion rack, damage from this night's earnings.
-- Pure and deterministic: the outcome is fixed before any presentation runs.
function G:BossPlan()
 local M=B.mainline;local cfg=B.boss
 if not M or not cfg then return nil end
 local broken=self:MainlineCount()
 local factor=cfg.shieldFactorBase+cfg.shieldFactorStep*broken
 local ultimate=self.mainline~=nil and self.mainline.ultimate~=nil
 local earned=math.max(0,self.roundEarned or 0)
 local damage=math.floor(earned*cfg.damagePerEarned*factor*(ultimate and cfg.ultimateMultiplier or 1))
 local hp=math.floor(cfg.hp*coinMul(B.winRound or 12))
 return {hp=hp,maxHp=hp,broken=broken,shields=cfg.shieldCount,
  earned=earned,damage=damage,ultimate=ultimate,
  result=damage>=hp and "victory" or "lost"}
end
function G:BossResolve()
 local plan=self:BossPlan()
 if not plan then return nil end
 self.boss=plan
 self.phase=plan.result
 if plan.result=="victory" then self.won=true end
 return plan
end
function G:Emit(kind,a,b,text,value)
 self.events[#self.events+1]={kind=kind,a=a or 0,b=b or a or 0,text=text or "",value=value or 0}
 local event=self.events[#self.events]
 event.actorUid=self.board[a or 0] and self.board[a or 0].uid or nil
 event.targetUid=self.board[b or a or 0] and self.board[b or a or 0].uid or nil
end
-- Explanations are captured during the real calculation, never recalculated by UI.
function G:IncomeEntry(slot,instance)
 if not self.incomeDetails or not instance then return nil end
 local key=uidKey(instance.uid)
 local entry=self.incomeDetails.byUid[key]
 if not entry then
  entry={id=instance.id,uid=instance.uid,slot=slot,growth=instance.growth or 0,
   steps={},reactions={},reactionTotal=0,value=0,directReward=0,turn=self.turn}
  self.incomeDetails.byUid[key]=entry
 end
 self.incomeDetails.bySlot[slot]=key
 return entry
end
function G:MaterialEarnings(slot)
 local details=self.incomeDetails
 if not details or details.turn~=self.turn or type(details.byUid)~="table" or type(details.bySlot)~="table" then return nil end
 local current=self.board[slot]
 local key=current and uidKey(current.uid) or details.bySlot[slot] or details.bySlot[tostring(slot)]
 return key and details.byUid[uidKey(key)] or nil
end
function G:MaterialEarningsByUid(uid)
 local d=self.incomeDetails
 if not d or d.turn~=self.turn or type(d.byUid)~="table" then return nil end
 return d.byUid[uidKey(uid)]
end
function G:LastMaterialEarnings(uid)
 local current=self:MaterialEarningsByUid(uid)
 if current then return current end
 for _,instance in ipairs(self.bag) do
  if uidKey(instance.uid)==uidKey(uid) then
   local last=instance.lastIncome
   if last and last.id==instance.id and uidKey(last.uid)==uidKey(uid) then return last end
   return nil
  end
 end
end
function G:AddGrowth(slot,instance,amount,label,nextTurn)
 -- A material consumed by the same reaction cannot carry growth into later rounds.
 if amount==0 or self.board[slot]~=instance then return end
 local cap=R.growCap and R.growCap[instance.id]
 if cap then amount=math.min(amount,cap-(instance.growth or 0)) end
 if amount<=0 then return end
 local before=instance.growth
 instance.growth=before+amount
 local entry=self:IncomeEntry(slot,instance)
 if entry then
  entry.growth=instance.growth
  entry.permanentChanges=entry.permanentChanges or {}
  entry.permanentChanges[#entry.permanentChanges+1]={label=label,amount=amount,before=before,after=instance.growth,nextTurn=nextTurn or false}
 end
end
function G:GrowFromBuff(slot,source)
 local target,actor=self.board[slot],self.board[source]
 if not target or not actor or not D.symbols[target.id].tags.growOnBuff then return end
 local pair=uidKey(actor.uid)..":"..uidKey(target.uid)
 for _,seen in ipairs(self.buffSeen) do if seen==pair then return end end
 self.buffSeen[#self.buffSeen+1]=pair
 self:AddGrowth(slot,target,R.growth,D.symbols[actor.id].name.."增益")
end
function G:Adjacent(i)
 local out={};local ci=COORDS[i];if not ci then return out end
 for j=1,BOARD_SIZE do
  local cj=COORDS[j]
  if i~=j and cj and math.abs(cj.x-ci.x)<=1 and math.abs(cj.y-ci.y)<=1 then out[#out+1]=j end
 end;return out
end
function G:Neighbors(i)
 local out={};for _,j in ipairs(self:Adjacent(i)) do if self.board[j] then out[#out+1]=j end end;return out
end
function G:BuffTargets(i)
 if self.board[i].id=="sun" and self:Has("hat") then
  local out={};for j=1,BOARD_SIZE do if j~=i and self.board[j] then out[#out+1]=j end end;return out
 end
 return self:Neighbors(i)
end
function G:Weights(relic)
 local weights=B.Odds(self.paid,relic)
 if relic then return weights end
 local luck=self:MagnetMultiplier()
 for _,s in ipairs(self.board) do if s then
  if s.id=="cat" then luck=luck*R.luckyCat^self:Count("paw")
  elseif s.id=="pebble" then luck=luck*R.shinyPebble
  elseif s.id=="hex" then luck=luck/R.hexTedium
  elseif s.id=="astrologer" then luck=luck*1.5 end
 end end
 -- Give higher rarities their share first; take the remainder from Common.
 local remaining=100
 for rarity=4,2,-1 do weights[rarity]=math.min(remaining,weights[rarity]*luck);remaining=remaining-weights[rarity] end
 weights[1]=remaining;return weights
end
-- Weighted pick inside one rarity bucket; mainline assist biases specific materials.
function G:PickFromList(list)
 local bias=self.pickBias
 if not bias then return list[self:Random(#list)] end
 local total=0
 for _,id in ipairs(list) do total=total+(bias[id] or 1) end
 if total<=0 then return list[self:Random(#list)] end
 local roll=self:Random()*total
 for _,id in ipairs(list) do
  roll=roll-(bias[id] or 1)
  if roll<0 then return id end
 end
 return list[#list]
end
function G:Pick(pool,weights,minimum,excluded,relic,maximum)
 local defs=relic and D.relics or D.symbols;local available={};local unowned={}
 for _,id in ipairs(pool) do local def=defs[id]
  if def.rarity>=minimum and def.rarity<=(maximum or 4) and not (excluded and excluded[id]) then
   local r=def.rarity;available[r]=available[r] or {};available[r][#available[r]+1]=id
   if relic and not self:Has(id) then unowned[r]=unowned[r] or {};unowned[r][#unowned[r]+1]=id end
  end
 end
 if relic then for r,list in pairs(unowned) do available[r]=list end end
 local total=0;for r=1,4 do if available[r] then total=total+weights[r] end end
 -- A forced minimum can have zero base probability (e.g. early Fertilizer).
 -- In that case choose the lowest permitted available rarity.
 if total<=0 then
  for r=minimum,4 do local list=available[r];if list then return self:PickFromList(list) end end
  return nil
 end
 local roll=self:Random()*total;local last
 for r=1,4 do local list=available[r]
  if list and weights[r]>0 then
   last=list;roll=roll-weights[r];if roll<0 then return self:PickFromList(list) end
  end
 end
 return last and self:PickFromList(last) or nil
end
function G:DrawChoices(relic)
 local chosen={};self.choices={};local weights=self:Weights(relic)
 self.pickBias=not relic and self:MainlineBias() or nil
 for slot=1,3 do
  local minimum=(not relic and self.draftContext=="rent") and B.Guarantee(self.paid,slot) or 1
  local id=self:Pick(relic and D.relicOrder or D.order,weights,minimum,chosen,relic)
  if id then chosen[id]=true;self.choices[#self.choices+1]=id end
 end
 if not relic and self.draftContext=="spin" and self.discoveryActive then
  local maximum=self.paid<2 and 2 or self.paid<4 and 3 or 4
  local id=self:Pick(D.order,weights,2,chosen,false,maximum)
  if id then self.choices[#self.choices+1]=id end
 end
 self.pickBias=nil
end
function G:Destroy(i,source,bonus,label,deathMultiplier,noDrop)
 local s=self.board[i];if not s then return false end
 local consumed=self:IncomeEntry(i,s)
 local producer=source~=i and self.board[source] and self:IncomeEntry(source,self.board[source]) or nil
 local def=D.symbols[s.id];self.board[i]=false
 -- Keep earlier reaction rewards even if their producer is consumed later.
 self.bonus=self.bonus+(self.actorBonus[i] or 0);self.actorBonus[i]=nil
 for k=#self.bag,1,-1 do if self.bag[k].uid==s.uid then table.remove(self.bag,k);break end end
 local reward=R.blackPepper*self:Count("pig");local intrinsic=def.tags.chest or 0
 if def.tags.void then intrinsic=R.void end
 reward=reward+intrinsic*(deathMultiplier or 1)
 if bonus and bonus~=0 then
  if source~=i and self.board[source] then self.actorBonus[source]=(self.actorBonus[source] or 0)+bonus
  else reward=reward+bonus end
 end
 if def.tags.ore and not noDrop then
  reward=reward+R.miningPick*self:Count("pick")
  for _=1,def.tags.ore do self:Add(assert(self:Pick(D.orePool,self:Weights(false),1))) end
 elseif s.id=="coconut" then self:Add("coconut_half");self:Add("coconut_half")
 elseif s.id=="peach" then self:Add("seed")
 elseif s.id=="urn" or s.id=="bigurn" then
  self.board[i]=self:Add("spirit");if s.id=="bigurn" then self:Add("spirit") end
 end
 self.bonus=self.bonus+reward
 if consumed then
  consumed.consumed=true;consumed.reason=label or "消耗"
  consumed.directReward=consumed.directReward+reward
  if producer and bonus and bonus~=0 then
   producer.reactions[#producer.reactions+1]={label=(label or "消耗").." · "..def.name,raw=bonus,amount=bonus,sourceUid=s.uid}
   producer.reactionTotal=producer.reactionTotal+bonus
   consumed.creditUid=producer.uid;consumed.creditName=D.symbols[producer.id].name;consumed.creditIndex=#producer.reactions
  end
 end
 self:Emit("devour",source,i,(label or "销毁")..((reward+(bonus or 0))~=0 and " +"..(reward+(bonus or 0)) or ""),reward+(bonus or 0))
 local event=self.events[#self.events]
 event.afterId=self.board[i] and self.board[i].id or ""
 event.actorId=producer and producer.id or s.id;event.targetId=s.id
 event.actorUid=producer and producer.uid or s.uid;event.targetUid=s.uid
 event.reactionIndex=producer and #producer.reactions or nil
 if event.actorId=="cat" and s.id=="milk" then event.effect="drink" end
 return true
end
-- Transformations consume instances without triggering destruction side effects.
function G:Transmute(i,j,product,label)
 local a,b=self.board[i],j and self.board[j]
 if not a or (j and not b) then return false end
 for _,pair in ipairs({{i,a},{j,b}}) do
  if pair[1] and pair[2] then
   local entry=self:IncomeEntry(pair[1],pair[2])
   if entry then entry.consumed=true;entry.reason=(label or "转化").." → "..D.symbols[product].name end
  end
 end
 for k=#self.bag,1,-1 do
  local uid=self.bag[k].uid
  if uid==a.uid or (b and uid==b.uid) then table.remove(self.bag,k) end
 end
 self.board[i]=self:Add(product)
 self.transmuted=self.transmuted or {};self.transmuted[tostring(self.board[i].uid)]=true
 if j then
  self.board[j]=false
  self:Emit("devour",i,j,label,0);self.events[#self.events].afterId=""
 end
 self:Emit("devour",i,i,label.." → "..D.symbols[product].name,0)
 self.events[#self.events].afterId=product
 return true
end
function G:Age(s)
 if s.countedTurn~=self.turn then s.age=s.age+1;s.countedTurn=self.turn end
end
function G:RollDie(sides) return self:Has("glasses") and sides or self:Random(sides) end
function G:SelectFuel(amount)
 if self.phase~="ready" or (amount~=0 and amount~=1 and amount~=2) then return false end
 self.selectedFuel=amount;return true
end
function G:NextTemperature() return math.max(0,self.temperature+self.selectedFuel-1) end
function G:DiscoveryChance(temperature)
 return math.min(B.brew.temperature.discoveryMax,(temperature or self.temperature)*B.brew.temperature.discoveryPerLevel)
end
function G:Spin()
 if not self:CanDraw() then return false end
 if not self:SelectFuel(self.selectedFuel) then return false end
 local nextTemperature=self:NextTemperature()
 self.spinFuel=self.selectedFuel;self.spinRaised=nextTemperature>self.temperature
 self.temperature=nextTemperature;self.discoveryActive=false
 self:ClearPot();self.events={}
 self.left=self.left-1;self.turn=self.turn+1
 if self:Has("broom") then
  self.cardboardAge=self.cardboardAge+1
  if self.cardboardAge%R.cardboardEvery==0 then self.removal=self.removal+self:Count("broom") end
 end
 local draw={};for _,s in ipairs(self.bag) do draw[#draw+1]=s end
 for i=#draw,2,-1 do local j=self:Random(i);draw[i],draw[j]=draw[j],draw[i] end
 local empty={};for i=1,BOARD_SIZE do if not self.board[i] then empty[#empty+1]=i end end
 for i=#empty,2,-1 do local j=self:Random(i);empty[i],empty[j]=empty[j],empty[i] end
 for i=1,math.min(B.boardSize,#draw,#empty) do local s=draw[i];self.board[empty[i]]=s;self.drawn[#self.drawn+1]=s.uid;self.fresh[#self.fresh+1]=s.uid end
 self.visualBoard={};for i=1,BOARD_SIZE do self.visualBoard[i]=self.board[i] end
 self.phase="arrange";return true
end
function G:LevelOf(id) return self.levels[id] or 1 end
function G:DesignMult(id)
 local U=B.upgrade;if not U then return 1 end
 local lv=self.levels[id] or 1
 local tbl=U.levels
 if lv>#tbl then lv=#tbl end
 local m=tbl[lv] or 1
 local grp=D.symbols[id].group
 return m*(1+(self.familyBoost[grp] or 0))
end
function G:CanUpgrade(id)
 if not D.symbols[id] then return false end
 if (self.upgradeTokens or 0)<1 then return false end
 return (self.levels[id] or 1)<B.upgrade.maxLevel
end
function G:Upgrade(id)
 if not self:CanUpgrade(id) then return false end
 self.levels[id]=(self.levels[id] or 1)+1
 self.upgradeTokens=self.upgradeTokens-1
 return true
end
function G:InvestFamily(grp)
 local U=B.upgrade
 if not U or (self.upgradeTokens or 0)<1 then return false end
 if (self.familyBoost[grp] or 0)>=U.familyCap-1e-9 then return false end
 self.familyBoost[grp]=math.min(U.familyCap,(self.familyBoost[grp] or 0)+U.familyRate)
 self.upgradeTokens=self.upgradeTokens-1
 return true
end
function G:UpgradeChoices()
 local seen,list={},{}
 for _,s in ipairs(self.bag) do
  if not seen[s.id] and self:CanUpgrade(s.id) then
   seen[s.id]=true
   local n=0;for _,t in ipairs(self.bag) do if t.id==s.id then n=n+1 end end
   local def=D.symbols[s.id]
   list[#list+1]={id=s.id,w=(def.value+1.2*def.rarity)*n}
  end
 end
 table.sort(list,function(a,b) return a.w>b.w end)
 local out={}
 for i=1,math.min(B.upgrade.choiceCount,#list) do out[#out+1]=list[i].id end
 return out
end
function G:AutoUpgrade()
 local ch=self:UpgradeChoices()
 if ch[1] then return self:Upgrade(ch[1]) end
 return false
end
function G:OnBrewSuccess()
 local hasStone=false
 for _,s in ipairs(self.bag) do if s.id=="philosophers_stone" then hasStone=true;break end end
 if hasStone then
  self.removal=(self.removal or 0)+1
  self:Emit("bonus",0,0,"贤者之石恩赐：驱魔符+1",0)
 end
 local U=B.upgrade;if not U then return end
 self.brewTally=(self.brewTally or 0)+1
 if self.brewTally%U.tokenEvery==0 then self.upgradeTokens=(self.upgradeTokens or 0)+1 end
 if U.auto then
  local guard=0
  while (self.upgradeTokens or 0)>0 and guard<8 do guard=guard+1;if not self:AutoUpgrade() then break end end
 end
end
function G:Resolve()
 if self.phase~="arrange" then return false end
 self.events={};self.values={};self.bonus=0;self.actorBonus={};self.transmuted={}
 -- Buff growth is capped per source per spin ("轮" = one spin), so the record resets every Resolve.
 self.buffSeen={}
 self.incomeDetails={turn=self.turn,byUid={},bySlot={}}
 -- Snapshot before reactions: changing a bag instance must not mutate the animation.
 self.visualBoard={}
 for i=1,BOARD_SIZE do local s=self.board[i];if s then local copy={};for k,v in pairs(s) do copy[k]=v end;self.visualBoard[i]=copy else self.visualBoard[i]=false end end
 if self.temperature>=3 then
  for i=1,BOARD_SIZE do local s=self.board[i]
   if s and (s.id=="ore" or s.id=="bigore" or s.id=="urn" or s.id=="bigurn") then self:Destroy(i,i,0,"高温裂解") end
  end
 end
 local recipes={{a="rose",b="milk",product="dream_potion",max=1},{a="toadstool",b="puffer",product="poison_crystal",min=3},{a="peel",b="thief",product="dream_essence"}}
 local used={}
 for _,recipe in ipairs(recipes) do
  if (not recipe.min or self.temperature>=recipe.min) and (not recipe.max or self.temperature<=recipe.max) then
   for i=1,BOARD_SIZE do local s=self.board[i]
    if s and s.id==recipe.a and not used[i] then
     for _,j in ipairs(self:Neighbors(i)) do local t=self.board[j]
      if t and t.id==recipe.b and not used[j] then
       used[i]=true;used[j]=true
       if recipe.product=="dream_essence" then self.bonus=self.bonus+(R.essenceBonus or 25);self:Emit("bonus",i,j,"恶念超度！+"..(R.essenceBonus or 25).."币",R.essenceBonus or 25) end
       self:Transmute(i,j,recipe.product,"炼金合成");break
      end
     end
    end
   end
  end
 end
 if self:Has("guillotine") then
  for i=1,BOARD_SIZE do if self.board[i] and self.board[i].id=="billionaire" then self:Transmute(i,nil,"purified_ash","净化") end end
 end
 local function boardHas(id)
  for i=1,BOARD_SIZE do if self.board[i] and self.board[i].id==id then return true end end
  return false
 end
 for i=1,BOARD_SIZE do local s=self.board[i]
  if s and s.id=="seed" and self:IsFresh(s.uid) then
   local chance=R.seedChance+R.fertilizerBoost*self:Count("fertilizer")
   local ci=COORDS[i]
   for j,t in ipairs(self.board) do if t and j~=i then
    local cj=COORDS[j]
    local nearby=ci and cj and math.abs(cj.x-ci.x)<=1 and math.abs(cj.y-ci.y)<=1
    if (nearby and (t.id=="rain" or t.id=="sun")) or (t.id=="sun" and self:Has("hat")) then chance=chance+R.seedBoost end
   end end
     if (ci and ci.y==4) or boardHas("fruitking") or self:Random()<math.min(1,chance) then
    s.id=assert(self:Pick(D.seedPool,self:Weights(false),self:Has("fertilizer") and 3 or 1))
    s.age=0;s.countedTurn=nil;self:Emit("grow",i,i,"生长！")
   end
  end
 end
 local dice={};local triggerDie
 for i=1,BOARD_SIZE do local s=self.board[i]
  if s then
   if self:IsFresh(s.uid) then self:Age(s) end;local sides=D.symbols[s.id].tags.die
   if s.id=="banana" and self:IsFresh(s.uid) and s.age%2==0 then self:Add("peel");self:Emit("grow",i,i,"织成封印符") end
   if sides then if self:IsFresh(s.uid) then s.roll=self:RollDie(sides) end;dice[i]=s.roll or 1;if dice[i]==1 then triggerDie=i end end
   if s.id=="gambler" and self:IsFresh(s.uid) then s.bank=s.bank+R.gamblerPerSpin end
   if s.id=="astrologer" and self:IsFresh(s.uid) then
    if self:Random()<0.5 then self.reroll=self.reroll+1;self:Emit("bonus",i,i,"星轨变易！重抽+1",0) end
   end
  end
 end
 -- Fixed actor order is a prototype rule; see the source/audit document.
 for _,actor in ipairs({"abyss_heart","necro","key","magic_key","cat","digger","miner","geo","fruitking","troll","dame","peel","taotie","gambler"}) do
  for i=1,BOARD_SIZE do local s=self.board[i]
   if s and s.id==actor then
    if actor=="fruitking" and self:IsFresh(s.uid) then self:Add("seed");self:Emit("grow",i,i,"御赐种子") end
    if actor=="gambler" and triggerDie then self:Destroy(i,i,s.bank,"赌命兑现！")
    else for _,j in ipairs(self:Neighbors(i)) do
     if not self.board[i] then break end
     local target=self.board[j]
     if target and not self.transmuted[tostring(target.uid)] then local id=target.id;local def=D.symbols[id];local tags=def.tags
      if actor=="abyss_heart" and (tags.human or id=="cultist") then
       self:Destroy(j,i,40,"深渊献祭！");self:AddGrowth(i,s,2,"献祭增幅");self.bonus=self.bonus+20
      elseif actor=="necro" and tags.human then self:Destroy(j,i,R.zaroff,"收割")
      elseif (actor=="key" or actor=="magic_key") and tags.chest then
       self:Destroy(j,i,0,"开箱暴富！",actor=="magic_key" and R.magicKey or 1);if actor=="key" then self:Destroy(i,i,0,"钥匙耗尽") end;break
      elseif actor=="cat" and id=="milk" then self:Destroy(j,i,R.milk,"畅饮幽灵奶")
      elseif actor=="digger" and (id=="urn" or id=="bigurn") then self:Destroy(j,i,R.hooligan,"掘墓")
      elseif actor=="miner" and tags.ore then self:Destroy(j,i,R.miner,"采矿")
      elseif actor=="fruitking" and tags.fruit and def.rarity<=4 then
       local gain=4
       if R.fruitGain then
        local tier=(def.rarity>=3) and 3 or ((def.rarity>=2) and 2 or 1)
        gain=R.fruitGain[tier] or R.fruitGain[1] or 4
       elseif def.rarity>=3 then gain=6 else gain=(def.rarity>=2) and 5 or 4 end
       local payout = def.value * 3 + 16
       self:Destroy(j,i,payout,"大胃吞噬！");self:AddGrowth(i,s,gain,"吞噬"..def.name)
      elseif (actor=="geo" and (tags.geo or (R.geoEatsOre and tags.ore))) then
       -- Swallowed ore does not crack into a gem: the miner owns that conversion.
       self:Destroy(j,i,4,"吞噬成长！永久+1",nil,not R.geoOreDrop)
       self:AddGrowth(i,s,R.growth,"吞噬"..def.name)
      elseif actor=="peel" and id=="billionaire" then
       local purReward = R.billionaire or 32
       self:Transmute(j,i,"purified_ash","辟邪超度！");self.bonus=self.bonus+purReward;self:Emit("bonus",i,j,"+"..purReward.."超度",purReward)
       elseif actor=="taotie" and not def.tags.mainline and not (tags.fruit and boardHas("fruitking")) and def.rarity<=2 then
        local targetIsMain = (id=="rose" or id=="milk" or id=="toadstool" or id=="puffer" or id=="peel" or id=="thief" or id=="dream_potion" or id=="poison_crystal" or id=="dream_essence" or id=="fruitking")
        if not targetIsMain then
         local payout = def.value * 2 + 6
         self:Destroy(j,i,payout,"饕餮暴食！");self:AddGrowth(i,s,1,"吞噬"..def.name);break
        end
       elseif actor=="troll" and tags.monkey then self:Destroy(j,i,R.monkey*(def.value+target.growth),"吞噬水果")
      elseif actor=="dame" and id=="martini" and self.temperature<=1 then
       -- The cup turns into essence rather than being destroyed; grant its
       -- fixed reaction reward once, and retain it on the consumed cup ledger.
       local entry=self:IncomeEntry(j,target)
       if self:Transmute(j,nil,"blood_essence","月露凝萃") then
        local reward=R.martini or 35
        self.bonus=self.bonus+reward
        if entry then entry.directReward=(entry.directReward or 0)+reward end
        self:Emit("bonus",i,j,"月露凝萃 +"..reward.."币",reward)
       end
      end
     end
    end end
   end
  end
 end
 local count={};local suits={};local extra={};local mult={};local emptyBonus={};local voidExpire={}
 local dameUsed={};local sunUsed={};local jokerUsed={}
 local additions,multipliers={},{}
 for i=1,BOARD_SIZE do additions[i]={};multipliers[i]={} end
 local function addSource(slot,source,amount)
  additions[slot][#additions[slot]+1]={label=D.symbols[self.board[source].id].name,amount=amount,sourceUid=self.board[source].uid}
  if amount>0 then self:GrowFromBuff(slot,source) end
 end
 for i=1,BOARD_SIZE do local s=self.board[i];extra[i]=0;mult[i]=1
  if s then
   count[s.id]=(count[s.id] or 0)+1;local suit=D.symbols[s.id].tags.suit;if suit then suits[suit]=(suits[suit] or 0)+1 end
  end
 end
 -- Positive global additions are real buffs too, including the pear's fruit-king synergy.
 for source,actor in ipairs(self.board) do
  if actor and actor.id=="fruitking" then
   for slot,target in ipairs(self.board) do
    if target and target.id~="fruitking" and D.symbols[target.id].tags.fruit then self:GrowFromBuff(slot,source) end
   end
  end
 end
 for i,s in ipairs(self.board) do if s then
  if s.id=="colossus" then
    for _,j in ipairs(self:Neighbors(i)) do
     local target=self.board[j]
     if target and target.id~="colossus" then extra[j]=extra[j]-1;addSource(j,i,-1) end
    end
   end
   if D.symbols[s.id].tags.void then
   local n=0;for _,j in ipairs(self:Adjacent(i)) do if not self.board[j] then n=n+1;emptyBonus[j]=(emptyBonus[j] or 0)+1 end end
   if n==0 then voidExpire[#voidExpire+1]=i end
  end
  if self.board[i] then for _,j in ipairs(self:BuffTargets(i)) do local target=self.board[j];local tags=D.symbols[target.id].tags;local factor=1
   if not self.transmuted[tostring(s.uid)] and not self.transmuted[tostring(target.uid)] then
   if s.id=="witch" and tags.witch then factor=R.witch
   elseif (s.id=="bee" or s.id=="rain") and target.id=="rose" then factor=s.id=="bee" and R.bee or R.rain
   elseif s.id=="sun" and target.id=="rose" then
    -- 多张血月递减叠乘：第 2/3 张仍有收益，但不再连乘
    local used=sunUsed[j] or 0
    if used<(R.sunStack or 1) then sunUsed[j]=used+1;factor=(used==0) and R.sun or (used==1 and (R.sun2 or R.sun) or (R.sun3 or R.sun2 or R.sun)) else factor=1 end
   elseif s.id=="midas" and target.id=="candy" then factor=R.midas
   elseif s.id=="joker" and tags.element then
    local used=jokerUsed[j] or 0
    if used<(R.jokerStack or 99) then jokerUsed[j]=used+1;factor=(used==0) and R.joker or (used==1 and (R.joker2 or R.joker) or (R.joker3 or R.joker2 or R.joker)) else factor=1 end
   elseif s.id=="dame" and tags.gem then
    -- 多位女王递减叠乘：第 2/3 张还有收益，不会直接废掉，也不至于失控
    local used=dameUsed[j] or 0
    if used<(R.dameStack or 1) then dameUsed[j]=used+1;factor=(used==0) and R.dame or (used==1 and (R.dame2 or R.dame) or (R.dame3 or R.dame2 or R.dame)) else factor=1 end
   end
   if s.id=="rose" and target.id=="bee" then
    extra[j]=extra[j]+1;addSource(j,i,1);self:Emit("bonus",i,j,"+1",1)
   end
   if D.symbols[s.id].tags.element and tags.element and tags.element~=D.symbols[s.id].tags.element then
    local gain=R.elementPair or 1
    extra[j]=extra[j]+gain;addSource(j,i,gain);self:Emit("bonus",i,j,"+"..gain,gain)
   end
   if D.symbols[s.id].tags.gem and tags.gem and (R.gemPair or 0)>0 then
    local gain=R.gemPair
    extra[j]=extra[j]+gain;addSource(j,i,gain);self:Emit("bonus",i,j,"+"..gain,gain)
   end
   if factor>1 then
    self:GrowFromBuff(j,i)
    local before=mult[j]
    local after=before*factor
    -- 蔓陀罗花的连乘上限：0 表示不限（Lua 里 0 是真值，必须显式判 > 0）
    if R.roseCap and R.roseCap>0 and target.id=="rose" and after>R.roseCap then after=R.roseCap end
    if after>before then
     multipliers[j][#multipliers[j]+1]={label=D.symbols[s.id].name,factor=after/before,sourceUid=s.uid}
     mult[j]=after
     if self:IsFresh(s.uid) or self:IsFresh(target.uid) then self:Emit("multiply",i,j,"×"..mult[j],mult[j]) end
    end
   end
   end
  end end
 end end
 local income=self.bonus;local expire={}
 -- 场上流派种类数：跨流派灵活度的直接度量（百花齐放）
 local groupTypes=0
 do
  local seen={}
  for i=1,BOARD_SIZE do local s=self.board[i];if s then local g=D.symbols[s.id].group;if g and not seen[g] then seen[g]=true;groupTypes=groupTypes+1 end end end
 end
 local omniCount=0
 do
  for i=1,BOARD_SIZE do local s=self.board[i];if s and D.symbols[s.id].tags.universal then omniCount=omniCount+1 end end
 end
 local omniMul=1
 do
  local U=B.universal
  if U then
   omniMul=math.min(U.mulCap or 4,1+(U.mulStep or 0)*omniCount)
   local maxG=0
   for g=1,6 do local c=self:GroupCount(g);if c>maxG then maxG=c end end
   if maxG<=3 and omniCount>=1 then omniMul=omniMul*1.25 end
  end
 end
 local bloatMul=1
 do
  local BL=B.bloat
  if BL then bloatMul=math.max(1-(BL.cap or 0.6),(1-(BL.step or 0)*math.max(0,#self.bag-(BL.free or 24)))) end
 end
 local diversityMul=1
 do
  local V=B.diversity
  if V then diversityMul=math.min(V.cap or 1.5,1+(V.step or 0)*math.max(0,groupTypes-1)) end
 end
 for i=1,BOARD_SIZE do local s=self.board[i]
  if s then
   local def=D.symbols[s.id];local value=def.value+s.growth+extra[i]
   local entry=self:IncomeEntry(i,s);entry.steps={}
   local running=0
   local function step(label,after,sourceUid,factor)
    if after~=running or #entry.steps==0 or factor then
     entry.steps[#entry.steps+1]={label=label,before=running,after=after,amount=after-running,sourceUid=sourceUid,factor=factor}
    end
    running=after
   end
   step("材料基础",def.value)
   step("永久成长（含本次成长）",def.value+s.growth)
   for _,gain in ipairs(additions[i]) do step(gain.label,running+gain.amount,gain.sourceUid) end
   if (count.fruitking or 0)>0 and def.tags.fruit and s.id~="fruitking" then value=value+5*(count.fruitking or 1);step("灵果之王 · 果实加成",value) end
   if s.id=="fruitking" then
    local totalFruits = 0
    for _, item in ipairs(self.board) do if item and D.symbols[item.id].tags.fruit and item.id~="fruitking" then totalFruits=totalFruits+1 end end
    value=value+totalFruits*3;step("同场果实（"..totalFruits.."个）",value)
   end
   if (count.dream_essence or 0)>0 and (def.tags.witch or def.group==3 or s.id=="purified_ash") then value=value+(R.dreamEssenceBoost or 1)*(count.dream_essence or 1);step("织梦雾 · 同场加成",value) end
   if dice[i] then value=dice[i]+s.growth+extra[i];step("本次骰子点数 "..dice[i],value) end
   if s.id=="cat" then value=value+R.pizza*self:Count("pizza");step("遗物 · 幽灵小鱼干",value) end
   if s.id=="owl" and s.age%R.owlEvery==0 then value=value+R.owlBonus;step("周期产出",value) end
   if s.id=="crow" and s.age%R.crowEvery==0 then value=value-math.max(0,R.crowPenalty-R.taxEvasion*self:Count("candle"));step("周期扣减（含遗物减免）",value) end
   if s.id=="thief" then value=value+math.min(1,R.taxEvasion*self:Count("candle"));step("遗物 · 避税加成",value) end
   if s.id=="cultist" then
    value=value+count.cultist-1;step("同场其他信徒（"..(count.cultist-1).."名）",value)
    if count.cultist>=3 then value=value+1;step("三人集会奖励",value) end
   end
   if def.tags.ritual then value=value+R.ritualCandle*self:Count("bible");step("遗物 · 仪式加成",value) end
   if s.id=="diamond" or s.id=="pumpkin" then
    local same=math.min(count[s.id]-1,R.sameSymbolCap or 5)
    value=value+same;step("同种材料（"..count[s.id].."件，上限+"..(R.sameSymbolCap or 5).."）",value)
   end
   if (s.id=="emerald" or s.id=="ruby" or s.id=="strawberry") and count[s.id]>=2 then value=value+1;step("同种材料成组奖励",value) end
   if def.tags.element and ((def.tags.element=="water" and self.temperature<=1) or (def.tags.element=="fire" and self.temperature>=3)) then value=value+(R.elementBonus or 2);step("元素与炉温匹配",value) end
   if s.id=="midas" and self:IsFresh(s.uid) then self:Add("candy") end
    -- 高阶宝箱若无钥匙：沉重锁死
    if (s.id=="safe" or s.id=="chest" or s.id=="mega") and not ((count.key or 0)>0 or (count.magic_key or 0)>0) then
     value=1;step("无钥匙锁死",value)
    end
    -- 女王若无宝石且无马天尼供奉：饥渴减产
    if s.id=="dame" then
     local hasFeed=false
     for _,item in ipairs(self.board) do if item and (D.symbols[item.id].tags.gem or item.id=="martini") then hasFeed=true;break end end
     if not hasFeed then value=math.max(0,value-3);step("缺少宝石或美酒供奉",value) end
    end
    -- 贤者之石（纯中立运营核心：必须流派平衡且牌库精炼<=21张，无脑乱点牌库膨胀无法激活）
    if s.id=="philosophers_stone" then
     local maxGrp=0
     for g=1,6 do local c=self:GroupCount(g);if c>maxGrp then maxGrp=c end end
     if maxGrp<=3 and #self.bag<=21 then
      mult[i]=mult[i]*2.5;multipliers[i][#multipliers[i]+1]={label="真理法则 · 完美平衡",factor=2.5}
     else
      value=2;step("牌库臃肿或流派失衡 · 顽石无光",value)
     end
    end
    -- 炼金天平（中立运营过渡）
    if s.id=="alchemy_scale" then
     if groupTypes>=3 then value=value+2;step("天平衡定",value) end
     if #self.bag<=21 then mult[i]=mult[i]*1.3;multipliers[i][#multipliers[i]+1]={label="精炼无赘",factor=1.3} end
    end
    -- 九命灵猫（使魔流传奇：只庇护使魔与女巫，单挂野猫不给加成）
    if (count.nine_lives_cat or 0)>0 and (def.tags.witch or def.tags.cat or s.id=="nine_lives_cat") then
     if (count.cat or 0)>0 or (count.witch or 0)>0 or (count.owl or 0)>0 or (count.nine_lives_cat or 0)>=2 then
      mult[i]=mult[i]*2;multipliers[i][#multipliers[i]+1]={label="九命仙灵庇护",factor=2}
     elseif s.id=="nine_lives_cat" then
      value=2;step("孤猫独行无伴",value)
     end
    end
    -- 万毒九头蛇（毒素流传奇：需毒素材料>=3才激活扩散，否则休眠）
    if (count.hydra or 0)>0 and (s.id=="hydra" or def.tags.poison) then
     local pCount=0
     for _,item in ipairs(self.board) do if item and (D.symbols[item.id].tags.poison or item.id=="toadstool" or item.id=="puffer" or item.id=="poison_crystal" or item.id=="antidote") then pCount=pCount+1 end end
      if pCount>=(R.hydraMin or 3) then
       value=value+pCount*(R.hydraPerPoison or 1);step("剧毒浸润（"..pCount.."件）",value)
       local factor=R.hydraMultiplier or 1.2
       mult[i]=mult[i]*factor;multipliers[i][#multipliers[i]+1]={label="九头蛇剧毒扩散",factor=factor}
     elseif s.id=="hydra" then
      value=2;step("毒沼未成 · 巨蛇休眠",value)
     end
    end
    -- 冥魂大巫妖（死灵流传奇：只统御死灵与骨灰，单挂无亡灵时无光）
    if (count.lich_emperor or 0)>0 and (def.group==3 or def.tags.undead or s.id=="lich_emperor") then
     local undeadCount=0
     for _,item in ipairs(self.board) do if item and (item.id=="urn" or item.id=="bigurn" or item.id=="spirit" or item.id=="necro" or item.id=="digger") then undeadCount=undeadCount+1 end end
     if undeadCount>=2 then
      mult[i]=mult[i]*2;multipliers[i][#multipliers[i]+1]={label="冥魂巫妖统御",factor=2}
     elseif s.id=="lich_emperor" then
      value=2;step("幽冥荒寂 · 无灵可御",value)
     end
    end
    -- 全能大魔导（元素/奥术流传奇：需4元素大爆发，若单挂元素不足2种则受困）
    if (count.archmage or 0)>0 then
     local hasEarth=(count.spade or 0)>0
     local hasAir=(count.club or 0)>0
     local hasFire=(count.heart or 0)>0
     local hasWater=(count.diamond_suit or 0)>0
     local elTypes=(hasEarth and 1 or 0)+(hasAir and 1 or 0)+(hasFire and 1 or 0)+(hasWater and 1 or 0)
     if elTypes>=4 and (def.tags.element or s.id=="archmage") then
      value=value+8;step("四大元素齐聚",value)
      mult[i]=mult[i]*2.5;multipliers[i][#multipliers[i]+1]={label="元素大爆发",factor=2.5}
     elseif s.id=="archmage" and elTypes<2 then
      value=1;step("元素匮乏 · 魔导受困",value)
     end
    end
   if s.id=="ronin" then
     local isAlone=true
     for _,j in ipairs(self:Neighbors(i)) do
      local neighbor=self.board[j]
      if neighbor and neighbor.uid~=s.uid then
       local nDef=D.symbols[neighbor.id]
       if nDef and (nDef.tags.human or nDef.tags.witch) then isAlone=false;break end
      end
     end
     if not isAlone then value=2+s.growth+extra[i];step("独行条件未满足",value) end
    end
    if s.id=="berserker" then
     local rent=self:Rent()
     if self.coins<rent*0.5 then value=value+12;step("背水一战",value);self:Emit("bonus",i,i,"背水一战！+12币",12) end
    end
    value=math.max(0,value);step("最低归零",value)
    if s.id=="rabbit" then
    -- The first ten payouts use the old value; the upgrade starts next payout.
    if self:IsFresh(s.uid) and s.age==R.rabbitAfter then self:AddGrowth(i,s,R.rabbitBonus,"玉兔陪伴觉醒",true) end
    value=value*self:MagnetMultiplier();step("遗物 · 磁力加成",value,nil,self:MagnetMultiplier())
   end
   if (COORDS[i] and COORDS[i].y==0) and (def.tags.volatile or s.id=="spirit") then mult[i]=mult[i]*2;multipliers[i][#multipliers[i]+1]={label="最上排位置加成",factor=2} end
   for _,gain in ipairs(multipliers[i]) do step(gain.label,running*gain.factor,gain.sourceUid,gain.factor) end
   local designMult=self:DesignMult(s.id)
   if designMult~=1 then step("材料等级与流派投资",running*designMult,nil,designMult) end
   -- 流派共鸣：同流派张数 → 该流派全体倍率
   local setMul=self:SetMultiplier(def.group)
   if setMul~=1 then step("流派共鸣（"..self:GroupCount(def.group).."张）",running*setMul,nil,setMul) end
   -- 连锁共振：每多一个倍率来源，额外放大（叠乘越多越爆）
   local resCfg=B.resonance
   local resMul=1
   if resCfg then resMul=math.min(resCfg.cap or 9,1+(resCfg.step or 0)*#multipliers[i]) end
   if resMul~=1 then step("连锁共振 ×"..#multipliers[i],running*resMul,nil,resMul) end
   -- 流派仪式：每晚结算后累积的永久增幅
   local riteMul=self:RiteMultiplier(def.group)
   if riteMul~=1 then step("流派仪式",running*riteMul,nil,riteMul) end
   -- 灵活变通：每开一个新流派拿一次永久全局增幅
   -- 灵活变通：每开一个新流派拿一次永久全局增幅，支持百花齐放与纯中立运营
   local pivotMul=(1+(self.pivotBonus or 0))*diversityMul*omniMul
   if pivotMul~=1 then step("跨流派领悟 / 百花齐放（"..groupTypes.."派·万能"..omniCount.."）",running*pivotMul,nil,pivotMul) end
   if bloatMul~=1 then step("牌袋臃肿（"..#self.bag.."张）",running*bloatMul,nil,bloatMul) end
   value=value*mult[i]*designMult*setMul*resMul*riteMul*pivotMul*bloatMul;self.values[i]=value;income=income+value
   entry.value=value;entry.growth=s.growth
   -- Reaction rewards use adjacency multipliers only, exactly as income below.
   entry.reactionTotal=0
   for _,gain in ipairs(entry.reactions) do
    gain.amount=gain.raw*mult[i];gain.factor=mult[i]
    entry.reactionTotal=entry.reactionTotal+gain.amount
   end
   if self.actorBonus[i] then
    -- 吞噬/开箱/喝奶这类“行动收益”也要吃材料等级，否则成长流后期完全跟不上房租
    local act=self.actorBonus[i]*mult[i]*self:DesignMult(s.id)
    income=income+act;self.bonus=self.bonus+act
   end
   if self:IsFresh(s.uid) and s.id=="spirit" and s.age>=R.spiritLife then expire[#expire+1]=i end
  elseif emptyBonus[i] then self.values[i]=emptyBonus[i];income=income+emptyBonus[i] end
 end
 local ritualSlots={}
 for i=1,BOARD_SIZE do if self.board[i] and self.board[i].id=="cultist" then ritualSlots[#ritualSlots+1]=i end end
 if #ritualSlots>=2 then
  local gain=#ritualSlots-1+(#ritualSlots>=3 and 1 or 0)
  self:Emit("ritual",ritualSlots[1],ritualSlots[1],"信徒共鸣 +"..gain,gain)
  self.events[#self.events].peers=ritualSlots
 end
 local before=self.bonus
 for _,i in ipairs(expire) do self:Destroy(i,i,0,"怨灵消散") end
 for _,i in ipairs(voidExpire) do self:Destroy(i,i,0,"虚空消散",mult[i]) end
 income=income+self.bonus-before
 -- 经济倍率：整笔收益与逐卡数值一起放大，卡面、流水账、飘字与结算总额始终对得上。
 local scale=coinMul(self.round)
 if scale~=1 then
  income=income*scale
  for i=1,BOARD_SIZE do if self.values[i] then self.values[i]=self.values[i]*scale end end
  for _,entry in pairs(self.incomeDetails.byUid) do
   entry.value=entry.value*scale
   entry.reactionTotal=entry.reactionTotal*scale
   entry.directReward=(entry.directReward or 0)*scale
   for _,gain in ipairs(entry.reactions) do gain.amount=gain.amount*scale end
   for _,st in ipairs(entry.steps) do
    st.before=st.before*scale;st.after=st.after*scale
    if st.amount then st.amount=st.amount*scale end
   end
   for _,pc in ipairs(entry.permanentChanges or {}) do
    pc.before=pc.before*scale;pc.after=pc.after*scale
   end
  end
  for _,event in ipairs(self.events) do
   if (event.kind=="bonus" or event.kind=="devour") and event.value then event.value=event.value*scale end
   if event.incomeDelta then event.incomeDelta=event.incomeDelta*scale end
  end
 end
 self.basePayout=math.max(0,math.floor(income));self.potValue=self.basePayout
 for _,entry in pairs(self.incomeDetails.byUid) do
  if entry.creditUid then
   local producer=self.incomeDetails.byUid[uidKey(entry.creditUid)]
   local gain=producer and producer.reactions[entry.creditIndex]
   entry.creditedAmount=gain and gain.amount or 0
  end
 end
 for _,event in ipairs(self.events) do
  if event.effect=="drink" then
   local entry=self.incomeDetails.byUid[uidKey(event.actorUid)]
   if entry then
    event.afterValue=entry.value
    for index,gain in ipairs(entry.reactions) do if index<=(event.reactionIndex or 0) then event.afterValue=event.afterValue+gain.amount end end
   end
  end
 end
 for _,instance in ipairs(self.bag) do
  local entry=self.incomeDetails.byUid[uidKey(instance.uid)]
  if entry then entry.round=self.round;instance.lastIncome=entry end
 end
 self:PrepareBrew();self:DetectMainline();self.phase="review"
 return true
end
-- Freeze board modifiers once. Preview never advances the RNG.
function G:PrepareBrew()
 self.brewRiskModifier=0;self.brewBoostModifier=0
 for _,s in ipairs(self.board) do if s then
  local modifier=B.brew.materials[s.id]
  if modifier then
   self.brewRiskModifier=self.brewRiskModifier+modifier.risk
   self.brewBoostModifier=self.brewBoostModifier+modifier.boost
  end
 end end
end
function G:AdditiveRemaining(id)
 if not B.brew.additives[id] then return 0 end
 return math.max(0,self:Count(id)-(self.additiveUses[id] or 0))
end
function G:PreviewBrew(additiveId,temperature)
 temperature=temperature or self.temperature
 if type(temperature)~="number" or temperature<0 then return nil end
 local heat=B.brew.temperature
 additiveId=additiveId or ""
 local additive=B.brew.additives[additiveId]
 if additiveId~="" and (not additive or self:AdditiveRemaining(additiveId)<1) then return nil end
 local risk=math.max(0,math.min(B.brew.maxRisk,heat.baseRisk+temperature*heat.riskPerLevel+self.brewRiskModifier-R.pressureBonus*self:Count("pressure")+(additive and additive.risk or 0)))
 local boost=heat.baseBoost+temperature*heat.boostPerLevel+self.brewBoostModifier+(additive and additive.boost or 0)
 if risk>=R.outletThreshold then boost=boost+R.outletBoost*self:Count("outlet") end
 return {risk=risk,boost=boost,payout=math.floor(self.basePayout*(100+boost)/100),
  refund=math.floor(self.basePayout*(additive and additive.refund or 0)/100)}
end
function G:StartBrew(additiveId)
 if self.phase~="review" or self.basePayout<=0 then return false end
 local plan=self:PreviewBrew(additiveId);if not plan then return false end
 self.additive=additiveId or "";self.brewBoost=plan.boost
 self.blastChance=plan.risk;self.brewRiskAtOpen=plan.risk;self.brewRefund=plan.refund
 if self.additive~="" then self.additiveUses[self.additive]=(self.additiveUses[self.additive] or 0)+1 end
 -- Commit one outcome before presentation; reload cannot re-roll the brew.
 -- blastChance only records this brew for the HUD; it is never fed back into the next roll.
 if self:Random()*100<plan.risk then
   local catUid=nil
   for _,s in ipairs(self.bag) do
    if s and s.id=="nine_lives_cat" and (s.lives or 9)>0 then
     catUid=s.uid;s.lives=(s.lives or 9)-1;break
    end
   end
   if catUid then
    self.phase="brewed"
    self:CommitMainline()
    self:Emit("bonus",0,0,"九命灵猫替死！免除炸锅",0)
   else
    self.lostPayout=plan.payout;self.phase="exploded";self.pendingMainline={}
   end
 else
  self.phase="brewed"
  self:CommitMainline()
  self.discoveryActive=self.spinFuel==2 and self.spinRaised and self:Random()*100<self:DiscoveryChance()
 end
 return true
end
function G:Deadline()
 self.phase=self.left<=0 and (self.coins>=self:Rent() and "tax" or "lost") or "ready"
end
local function finishSpin(self,payout,outcome)
 self.coins=self.coins+payout;self.total=self.total+payout;self.last=payout
 self.roundEarned=(self.roundEarned or 0)+payout
 self.lastOutcome=outcome;self.draftContext="spin";self.phase="draft";self:DrawChoices(false);return true
end
function G:Bottle()
 if self.phase~="review" and self.phase~="brewed" then return false end
 if self.phase=="brewed" then self:OnBrewSuccess() end
 return finishSpin(self,self:Estimate(),self.phase=="review" and "collected" or "bottled")
end
function G:FinishExplosion()
 if self.phase~="exploded" then return false end
 return finishSpin(self,self.brewRefund,"exploded")
end
function G:RecoverExplosion()
 if self.phase~="exploded" then return false end
 -- Replace the explosion settlement with the original board payout. No brew
 -- multiplier, refund stacking, success upgrades or discovery reward applies.
 self.discoveryActive=false
 return finishSpin(self,self.basePayout,"rescued")
end
function G:AfterDraft()
 self.choices={};self.discoveryActive=false
 if self.draftContext=="rent" then self.relicContext="rent";self.phase="relic";self:DrawChoices(true)
 else self:Deadline() end
end
function G:FinishRent()
 self.choices={};self.phase=((not B.boss or self.paid<(B.winRound or math.huge)) and self.paid>=#B.rents) and "victory" or "ready"
end
function G:Choose(index)
 local id=self.choices[index];if not id then return false end
 if self.phase=="draft" then self:Add(id);self:AfterDraft()
 elseif self.phase=="relic" then
  if id=="broom" and not self:Has(id) then self.cardboardAge=0 end
  self.relics[id]=self:Count(id)+1
  self:FinishRent()
 else return false end;return true
end
function G:Skip()
 if self.phase=="draft" and #self.bag>0 then self:AfterDraft();return true end
 return false
end
function G:Reroll()
 if self.phase~="draft" or self.reroll<1 then return false end
 self.reroll=self.reroll-1;self:DrawChoices(false);return true
end
function G:RewardReroll()
 if self.phase~="draft" then return false end
 -- Use the same pool/guarantees as a bone-dice reroll, without spending a die.
 self:DrawChoices(false);return true
end
function G:RewardRemoval()
 if self.phase~="ready" and self.phase~="draft" and self.phase~="tax" then return false end
 self.removal=(self.removal or 0)+1;return true
end
function G:RewardRerollToken()
 if self.phase~="ready" and self.phase~="draft" and self.phase~="tax" then return false end
 self.reroll=(self.reroll or 0)+1;return true
end
function G:Pay()
 if self.phase~="tax" or self.coins<self:Rent() then return false end
 local mulBefore=coinMul(self.round)
 -- 流派仪式：交租时结算，场上该流派达标的永久累积增幅（复利，越晚越爆）
 do
  local RITE=B.rite
  if RITE then
   self.rites=self.rites or {}
   local counts={}
   local omni=0
   for i=1,BOARD_SIZE do local s=self.board[i];if s then
    local g=D.symbols[s.id].group
    if D.symbols[s.id].tags.universal then omni=omni+1 elseif g then counts[g]=(counts[g] or 0)+1 end
   end end
   local credit=omni*((B.universal and B.universal.riteCredit) or 0)
   -- 门槛随夜数上升：3 → 4 → 5，逼玩家真的把某个流派堆起来
   local need=(RITE.minBase or 3)+math.floor((self.round-1)/(RITE.minStep or 4))
   local qualified={}
   for grp,n in pairs(counts) do if n+credit>=need then qualified[#qualified+1]=grp end end
   table.sort(qualified)
   -- 单流派独走只吃 1.45；两个流派同时立起来才拿到 1.8 的复利
   -- 复利倍率是分档的：基础 1.55，每多一个达标流派 +0.25，场上有万能卡再 +0.10
   -- 真正主副双流派门槛：必须场上有2个流派张数>=4，或有1个主力流派>=4且配合>=2张万能牌
   -- 单流派死磕单一色、无副修互补者，严格判定为单流派独走
   -- 流派仪式自然复利：任何达标流派均享受仪式深化，兼修多流派或融入万能卡时进一步放大
   local growth=1
   if #qualified>=1 then
    growth=(RITE.base or 1.42)+(RITE.perGroup or 0.12)*(#qualified-1)+((omni>0) and (RITE.perOmni or 0.15) or 0)
   end
   for _,grp in ipairs(qualified) do
    local cur=self.rites[grp] or 0
    local power=RITE.familyGrowthPower and RITE.familyGrowthPower[grp] or 1
    self.rites[grp]=math.min(RITE.cap or 3000,(1+cur)*growth^power-1)
   end
  end
 end
 self.roundScores=self.roundScores or {}
 self.roundScores[self.round]=math.max(0,math.floor(self.total))
 self.coins=self.coins-self:Rent();self.paid=self.round
 self.blastChance=0;self.brewRewards=0;self.additiveUses={};self.temperature=0;self.selectedFuel=1;self.spinFuel=1;self.spinRaised=false;self.discoveryActive=false
 -- The showdown settles once at the final night; every later rent is a plain rent.
 if B.boss and self.paid==B.winRound then
  self.round=self.round+1;self.left=self:Spins()
  self:BossResolve()
 else
  if not B.boss and self.paid>=(B.winRound or #B.rents) then self.won=true end
  self.roundEarned=0;self.round=self.round+1;self.left=self:Spins()
  -- Rent no longer opens a material draft; it grants its reward, then a relic.
  self.phase="reward"
 end
 -- 跨夜通胀：存款按同一经济倍率换算，避免“早存的钱”被新倍率稀释。
 local mulAfter=coinMul(self.round)
 if mulAfter~=mulBefore then self.coins=math.floor(self.coins*mulAfter/mulBefore+0.5) end
 return true
end
-- Rent reward gives one exorcism token and one bone die, never a material pick.
function G:ClaimRentReward()
 if self.phase~="reward" then return false end
 local reward=B.rentReward or {}
 self.removal=self.removal+(reward.removal or 1)
 self.reroll=self.reroll+(reward.reroll or 0)
 -- 升级令牌变成“手动选卡”：先出三张候选，选完才进遗物。
 local offers=(self.upgradeTokens or 0)>0 and self:UpgradeChoices() or {}
 if #offers>0 then
  self.upgradeChoices=offers;self.phase="upgrade"
  return true
 end
 self.upgradeTokens=0
 self.phase="relic";self.relicContext="rent";self.draftContext="rent"
 self:DrawChoices(true)
 return true
end
-- 手动升级：一次一枚令牌，选完仍有令牌就继续出候选。
function G:FinishUpgrade()
 self.upgradeChoices={}
 self.phase="relic";self.relicContext="rent";self.draftContext="rent"
 self:DrawChoices(true)
end
function G:PickUpgrade(id)
 if self.phase~="upgrade" then return false end
 local found=false
 for _,v in ipairs(self.upgradeChoices or {}) do if v==id then found=true end end
 if not found or not self:Upgrade(id) then return false end
 if (self.upgradeTokens or 0)>0 then
  local ch=self:UpgradeChoices()
  if #ch>0 then self.upgradeChoices=ch;return true end
  self.upgradeTokens=0
 end
 self:FinishUpgrade()
 return true
end
function G:SkipUpgrade()
 if self.phase~="upgrade" then return false end
 -- 令牌保留下来，下次交租再选；只跳过本次弹窗。
 self:FinishUpgrade()
 return true
end
-- 顶栏令牌入口随时能打开：优先续用上次还没选完的候选，否则按当前坩埚袋重新出牌。
function G:OpenUpgrade()
 if (self.upgradeTokens or 0)<1 then return false end
 if self.phase=="upgrade" and #(self.upgradeChoices or {})>0 then return true end
 local offers=self:UpgradeChoices()
 if #offers==0 then self.upgradeTokens=0;return false end
 self.upgradeChoices=offers
 self.phase="upgrade"
 return true
end
function G:Remove(uid)
 if self.phase~="ready" or self.removal<1 or #self.bag<=1 then return false end
 for i,s in ipairs(self.bag) do if s.uid==uid then
  table.remove(self.bag,i);self.removal=self.removal-1
  for j,t in ipairs(self.board) do if t and t.uid==uid then self.board[j]=false end end
  return true
 end end;return false
end
function G:Continue() if self.phase=="victory" then self.phase="ready";return true end;return false end
return G
