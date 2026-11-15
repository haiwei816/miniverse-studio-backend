local G=require "Game"
local D=require "Data"
local B=D.balance
local n=0
local function check(condition,message) n=n+1;assert(condition,message) end

-- Build an arrange-phase game with an explicit pot.
local function setup(temp,items,round)
 local g=G.New(23);g.bag={};g.uid=0;g:ClearPot();g.turn=1;g.temperature=temp;g.phase="arrange"
 g.round=round or 1;g.left=5;g.draftContext="spin"
 for _,entry in ipairs(items) do local s=g:Add(entry[2]);g.board[entry[1]]=s;g.fresh[#g.fresh+1]=s.uid end
 return g
end
local function count(g,id) local n=0;for _,s in ipairs(g.bag) do if s.id==id then n=n+1 end end;return n end
-- Force the brew dice without touching the rest of the RNG path.
local function brewResult(g,explode)
 local value=explode and 0 or 0.999
 local real=g.Random
 g.Random=function() return value end
 local ok=g:StartBrew("")
 g.Random=real
 return ok
end

-- 1. Sleep potion: detected on the pot, committed only after a successful brew.
local g=setup(0,{{7,"rose"},{8,"milk"}});g:Resolve()
check(g.mainline.sleep==nil and #g.pendingMainline==1,"mainline waits for the brew, not the reaction")
check(g:MainlineCount()==0,"pending slots do not count as done")
brewResult(g,false)
check(g.mainline.sleep==g.round,"successful brew records the slot")
check(g:MainlineCount()==1,"slot counts once committed")

-- 2. Temperature gates the slot: the same pot at 2 is not a hit.
g=setup(2,{{7,"dream_potion"}});g:Resolve()
check(#g.pendingMainline==0,"sleep potion needs heat <= 1")

-- 3. A blast never records progress and drops the pending hit.
g=setup(0,{{7,"rose"},{8,"milk"}});g:Resolve()
check(#g.pendingMainline==1,"reaction staged before the blast")
brewResult(g,true)
check(g.mainline.sleep==nil and #g.pendingMainline==0,"explosion clears mainline progress")

-- 4. Acid needs heat >= 3.
g=setup(3,{{7,"toadstool"},{8,"puffer"}});g:Resolve()
check(g.pendingMainline[1]=="acid","acid slot detected at high heat")
g=setup(2,{{7,"poison_crystal"}});g:Resolve()
check(#g.pendingMainline==0,"acid needs heat >= 3")

-- 5. Mist has no temperature gate.
g=setup(4,{{7,"peel"},{8,"thief"}});g:Resolve()
check(g.pendingMainline[1]=="mist","mist works at any heat")

-- 6. Ultimate needs all three potions in the same pot.
g=setup(0,{{7,"dream_potion"},{8,"poison_crystal"},{9,"dream_essence"}});g:Resolve()
local hasUltimate=false
for _,id in ipairs(g.pendingMainline) do if id=="ultimate" then hasUltimate=true end end
check(hasUltimate,"three products in one pot unlock the ultimate")
g=setup(0,{{7,"dream_potion"},{8,"poison_crystal"}});g:Resolve()
for _,id in ipairs(g.pendingMainline) do check(id~="ultimate","two products are not enough") end

-- 7. Progress is permanent across rents but does not re-record.
g=setup(0,{{7,"dream_potion"}});g:Resolve();brewResult(g,false)
local firstRound=g.mainline.sleep
g:ClearPot();g.phase="arrange";g.temperature=0;g.board[7]=g:Add("dream_potion");g:Resolve()
check(#g.pendingMainline==0,"finished slots are not staged again")
brewResult(g,false)
check(g.mainline.sleep==firstRound,"finished slots keep their first round")

-- 8. Old saves without mainline load as empty progress.
local legacy=G.New(5);legacy.mainline=nil;legacy.pendingMainline=nil;legacy:Restore()
check(type(legacy.mainline)=="table" and legacy:MainlineCount()==0,"old saves start with an empty rack")

-- 9. Guarantee: past the assist round the missing materials get extra weight.
check(B.mainline and B.mainline.assist==3,"assist multiplier is configured")
local low=setup(0,{});low.round=1
check(low:MainlineBias()==nil,"no bias before the assist round")
local late=setup(0,{});late.round=9
local bias=late:MainlineBias()
check(bias and bias.rose==3 and bias.milk==3,"sleep materials are boosted late")
check(bias.peel==3 and bias.thief==3,"mist materials are boosted late")

-- 10. Bias actually shows up in the draft odds, and stays off earlier.
local function sampleBias(round,n)
 local hits=0;local g=G.New(7);g.round=round;g.draftContext="spin"
 for _=1,n do
  g:DrawChoices(false)
  for _,id in ipairs(g.choices) do if id=="rose" or id=="milk" then hits=hits+1 end end
 end
 return hits
end
local early,lateHits=sampleBias(1,120),sampleBias(9,120)
check(lateHits>early,"assist raises the chance of the missing materials")

-- 11. A finished rack stops biasing anything.
local done=setup(0,{});done.round=12;done.mainline={sleep=1,acid=2,mist=3,ultimate=4}
check(done:MainlineBias()==nil,"finished rack stops the assist")

-- 12. Legacy rules have no main line and must not break drafting.
local legacyData=require "legacy_heat_v3.Data"
check(legacyData.balance.mainline==nil,"legacy balance keeps no mainline table")

print("PASS: "..n.." mainline assertions")
