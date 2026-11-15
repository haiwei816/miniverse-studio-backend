local G=require 'Game'
-- 单价/逐步骤账本用例：临时关掉流派共鸣、连锁共振与跨流派倍率，保持单品基准。
local BAL=require "AlchemyBalance"
local savedSet,savedRes,savedPivot,savedDiv=BAL.set,BAL.resonance,BAL.pivot,BAL.diversity
BAL.set=nil;BAL.resonance=nil;BAL.pivot=nil;BAL.diversity=nil
local D=require 'Data'
local B=D.balance
local checks=0
local function eq(a,b,message) checks=checks+1;assert(math.abs(a-b)<0.000001,message or (tostring(a)..' ~= '..tostring(b))) end
local function arrange(placements,relics)
 local g=G.New(42);g.bag={};g.uid=0;g:ClearPot();g.phase='arrange';g.turn=1;g.temperature=2;g.relics=relics or {}
 for _,p in ipairs(placements) do local s=g:Add(p[2]);g.board[p[1]]=s;g.fresh[#g.fresh+1]=s.uid end
 assert(g:Resolve());return g
end
local g=arrange({{6,'cat'},{7,'milk'}})
local cat=g:MaterialEarnings(6);local milk=g:MaterialEarnings(7)
eq(cat.value,1);eq(cat.reactionTotal,B.rules.milk);eq(cat.value+cat.reactionTotal,g.basePayout)
assert(milk.consumed and type(milk.reason)=="string")
assert(g.events[1].effect=='drink');eq(g.events[1].afterValue,cat.value+cat.reactionTotal)
eq(milk.creditedAmount,B.rules.milk)
local savedSlots={};for key,value in pairs(g.incomeDetails.bySlot) do savedSlots[tostring(key)]=value end
g.incomeDetails.bySlot=savedSlots;g:Restore();assert(g:MaterialEarnings(7).consumed)
eq(g:MaterialEarnings(7).creditedAmount,B.rules.milk)
g=arrange({{6,'cat'},{7,'milk'},{10,'witch'}})
cat=g:MaterialEarnings(6)
eq(cat.reactionTotal,B.rules.milk*B.rules.witch)
assert(cat.reactions[1].factor==B.rules.witch)
g=arrange({{7,'cat'},{6,'milk'},{8,'milk'}})
local drinks={};for _,e in ipairs(g.events) do if e.effect=='drink' then drinks[#drinks+1]=e end end
assert(#drinks==2)
eq(drinks[1].afterValue,1+B.rules.milk);eq(drinks[2].afterValue,1+2*B.rules.milk)
g=arrange({{6,'cat'},{7,'milk'},{8,'cat'}})
local consumedCount=0;for _,e in ipairs(g.events) do if e.effect=='drink' then consumedCount=consumedCount+1 end end
eq(consumedCount,1,'one bottle cannot be consumed twice')
local two=arrange({{1,'cultist'},{21,'cultist'}})
eq(two:MaterialEarnings(1).value,1);eq(two:MaterialEarnings(21).value,1)
g=arrange({{1,'cultist'},{11,'cultist'},{21,'cultist'}},{bible=2})
for _,slot in ipairs({1,11,21}) do
 local entry=g:MaterialEarnings(slot)
 eq(entry.value,3+2*B.rules.ritualCandle)
 assert(entry.steps[2].label:find('同场其他信徒',1,true))
 eq(entry.steps[2].amount,2);eq(entry.steps[3].amount,1)
 local last=0;for _,step in ipairs(entry.steps) do eq(step.before,last);last=step.after end;eq(last,entry.value)
end
local ritual=g.events[#g.events];assert(ritual.kind=='ritual' and #ritual.peers==3);eq(ritual.value,3)
local stringSlots={};for key,value in pairs(g.incomeDetails.bySlot) do stringSlots[tostring(key)]=value end
g.incomeDetails.bySlot=stringSlots;g:Restore();eq(g:MaterialEarnings(11).value,3+2*B.rules.ritualCandle)
g.incomeDetails=nil;g:Restore();assert(g:MaterialEarnings(11)==nil,'old saves have no fabricated details')
g=arrange({{6,'rose'},{7,'bee'},{10,'rain'}})
local entry=g:MaterialEarnings(6);eq(entry.steps[#entry.steps].after,entry.value)
g.phase='ready';g.left=5;assert(g:Spin());assert(g:MaterialEarnings(6)==nil,'old turn must not leak')
-- A real save round trip turns uids into doubles and map keys into strings.
local function jsonRoundTrip(g)
 local refs={}
 for i,s in ipairs(g.board) do refs[i]=s and s.uid or nil;g.board[i]=false end
 for _,s in ipairs(g.bag) do s.uid=s.uid+0.0 end
 local byId={};for _,s in ipairs(g.bag) do byId[s.uid]=s end
 for i,uid in pairs(refs) do local s=byId[uid+0.0];g.board[i]={id=s.id,uid=s.uid,growth=s.growth,age=s.age,bank=s.bank} end
 for key,entry in pairs(g.incomeDetails.byUid) do
  entry.uid=entry.uid+0.0
  if entry.creditUid then entry.creditUid=entry.creditUid+0.0 end
 end
 local slots={};for key,value in pairs(g.incomeDetails.bySlot) do slots[tostring(key)]=value end
 g.incomeDetails.bySlot=slots
 g:Restore()
end
g=arrange({{6,'cat'},{7,'milk'}})
local catUid=g.board[6].uid
jsonRoundTrip(g)
local reloaded=g:MaterialEarnings(6)
assert(reloaded,'cat earnings must survive a reload')
eq(reloaded.value,1);eq(reloaded.reactionTotal,B.rules.milk)
assert(g:MaterialEarnings(7).consumed,'milk stays marked consumed after reload')
assert(g:LastMaterialEarnings(catUid)==reloaded,'the latest record stays attached to the cat')
assert(g:LastMaterialEarnings(catUid).reactionTotal==B.rules.milk)
local plan=require('IncomeAnimation').Build(g)
local animated=0;for _,event in ipairs(plan.events) do
 if event.incomeDelta then animated=animated+event.incomeDelta end
 if event.kind=='income' then for _,item in ipairs(event.items) do animated=animated+item.delta end end
end
local payout=g.basePayout
g.phase='ready';g.left=5;g:Spin()
eq(animated,payout,'reloaded ledger must still explain every coin')
-- Permanent growth is recorded on its owner, once per source per round.
g=arrange({{1,'pear'},{11,'fruitking'}})
local pear=g:MaterialEarnings(1)
assert(pear.permanentChanges and #pear.permanentChanges==1,'pear growth must be explained')
eq(pear.growth,B.rules.growth)
eq(pear.permanentChanges[1].after-pear.permanentChanges[1].before,B.rules.growth)
assert(pear.permanentChanges[1].label:find('果王',1,true))
eq(pear.steps[#pear.steps].after,pear.value)
g.phase='arrange';g.turn=2;assert(g:Resolve())
eq(g:MaterialEarnings(1).growth,2*B.rules.growth,'growth repeats in later rounds')
g=arrange({{6,'amethyst'},{7,'dame'}})
local amethyst=g:MaterialEarnings(6)
eq(amethyst.growth,B.rules.growth)
assert(amethyst.permanentChanges and amethyst.permanentChanges[1].label:find('血族女王',1,true))
g=arrange({{7,'rabbit'}})
for turn=2,B.rules.rabbitAfter do g.phase='arrange';g.turn=turn;assert(g:Resolve()) end
local rabbit=g:MaterialEarnings(7)
eq(rabbit.growth,B.rules.rabbitBonus)
assert(rabbit.permanentChanges and rabbit.permanentChanges[1].nextTurn)
eq(rabbit.value,D.symbols.rabbit.value,'the upgrade starts next payout')
g.phase='arrange';g.turn=B.rules.rabbitAfter+1;assert(g:Resolve())
eq(g:MaterialEarnings(7).value,D.symbols.rabbit.value+B.rules.rabbitBonus)
print('PASS: '..checks..' income detail checks')
BAL.set=savedSet;BAL.resonance=savedRes;BAL.pivot=savedPivot;BAL.diversity=savedDiv
