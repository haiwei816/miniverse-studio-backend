local G=require "Game"
-- 这套用例验证的是炼金/反应本身；关掉新增的流派共鸣/共振/跨流派倍率，保持单品基准。
local BAL=require "AlchemyBalance"
local savedSet,savedRes,savedPivot,savedDiv=BAL.set,BAL.resonance,BAL.pivot,BAL.diversity
BAL.set=nil;BAL.resonance=nil;BAL.pivot=nil;BAL.diversity=nil
local D=require "Data"
local R=(require "AlchemyBalance").rules
local n=0
local function check(condition,message) n=n+1;assert(condition,message) end
local function setup(temp,items)
 local g=G.New(19);g.bag={};g.uid=0;g:ClearPot();g.turn=1;g.temperature=temp;g.phase="arrange"
 for _,entry in ipairs(items) do local s=g:Add(entry[2]);g.board[entry[1]]=s;g.fresh[#g.fresh+1]=s.uid end
 return g
end
local function amount(g,id) local n=0;for _,s in ipairs(g.bag) do if s.id==id then n=n+1 end end;return n end
for _,temp in ipairs({0,1}) do
 local g=setup(temp,{{7,"rose"},{8,"milk"},{9,"cat"}});check(g:Resolve(),"low-temperature resolve")
 check(amount(g,"dream_potion")==1 and amount(g,"rose")==0 and amount(g,"milk")==0,"low-temperature recipe and priority")
 check(g.visualBoard[7].id=="rose" and g.visualBoard[8].id=="milk","reaction animation snapshot")
 check(#g.bag==2 and g.board[7].id=="dream_potion","product in bag and board")
 check(not g:Resolve() and amount(g,"dream_potion")==1,"resolve cannot repeat")
end
local g=setup(2,{{7,"rose"},{8,"milk"}});g:Resolve();check(amount(g,"dream_potion")==0,"recipe excludes temperature 2")
g=setup(3,{{7,"toadstool"},{8,"puffer"}});g:Resolve()
check(amount(g,"poison_crystal")==1 and #g.bag==1,"poison ingredients consumed")
check(g.brewRiskModifier==3 and g.brewBoostModifier==8,"product risk counted")
g=setup(3,{{7,"toadstool"},{8,"puffer"},{6,"dame"}});g:Resolve()
check(g.values[7]==D.symbols.poison_crystal.value,"fresh product does not receive other reactions")
local crystal=g.board[7];local queen=g.board[6];g:ClearPot();g.board[7]=crystal;g.board[6]=queen;g.fresh={crystal.uid,queen.uid};g.phase="arrange";g.turn=2;g:Resolve()
check(g.values[7]==D.symbols.poison_crystal.value*R.dame,"product participates in reactions on later draws")
g=setup(2,{{7,"toadstool"},{8,"puffer"}});g:Resolve();check(amount(g,"poison_crystal")==0,"poison threshold")
g=setup(3,{{7,"ore"},{9,"urn"}});g:Resolve()
check(amount(g,"ore")==0 and amount(g,"urn")==0 and amount(g,"spirit")==1,"heat cracks urn and ore")
check(#g.bag==2 and g.board[9].id=="spirit","ore gives one product")
g=setup(2,{{7,"ore"},{9,"urn"}});g:Resolve();check(amount(g,"ore")==1 and amount(g,"urn")==1,"heat crack threshold")
g=setup(0,{{7,"rose"},{8,"milk"},{9,"rose"}});g:Resolve();check(amount(g,"dream_potion")==1 and amount(g,"rose")==1,"one ingredient cannot react twice")
g=setup(0,{{7,"peel"},{8,"thief"}});g:Resolve();check(amount(g,"dream_essence")==1 and #g.bag==1,"dream binding")
g=setup(0,{{7,"banana"}});g.board[7].age=1;g:Resolve();check(amount(g,"peel")==1,"dream thread weaves a seal on its second appearance")
g=setup(0,{{7,"dame"},{8,"martini"}});local cupUid=g.board[8].uid;g:Resolve();check(amount(g,"blood_essence")==1 and amount(g,"dame")==1,"queen condenses cup")
check(g.bonus==R.martini,"queen's cold transmutation grants the configured bonus")
local cupEntry=g:MaterialEarningsByUid(cupUid)
check(cupEntry and cupEntry.consumed and cupEntry.directReward==R.martini,"cup's fixed reward is retained in the income ledger")
local animation=require('IncomeAnimation').Build(g);local animated=0
for _,e in ipairs(animation.events) do
 if e.incomeDelta then animated=animated+e.incomeDelta end
 if e.kind=='income' then for _,item in ipairs(e.items) do animated=animated+item.delta end end
end
check(math.abs(animated-g.basePayout)<1e-8,"queen reward is accounted exactly once in the payout animation")
g=setup(0,{{7,"dame"},{6,"martini"},{8,"martini"}});g:Resolve()
check(amount(g,"blood_essence")==2 and g.bonus==2*R.martini,"queen grants one reward per adjacent cup")
g=setup(2,{{7,"dame"},{8,"martini"}});g:Resolve();check(amount(g,"martini")==1 and g.bonus==0,"cup requires cold and grants no bonus otherwise")
g=setup(0,{{7,"billionaire"}});g.relics.guillotine=1;g:Resolve();check(amount(g,"purified_ash")==1 and #g.bag==1,"purification")
g=setup(3,{{7,"heart"},{8,"spade"}});g:Resolve()
check(g.values[7]==D.symbols.heart.value+(R.elementPair or 1)+(R.elementBonus or 2) and g.values[8]==D.symbols.spade.value+(R.elementPair or 1),"different elements resonate and fire responds to heat")
local resonances={};for _,event in ipairs(g.events) do if event.kind=="bonus" then resonances[event.a..":"..event.b]=event end end
check(resonances["7:8"] and resonances["8:7"] and resonances["7:8"].value==(R.elementPair or 1),"each element bonus shows source and receiving material")
g=setup(0,{{7,"diamond_suit"},{8,"diamond_suit"}});g:Resolve()
check(g.values[7]==D.symbols.diamond_suit.value+(R.elementBonus or 2) and g.values[8]==D.symbols.diamond_suit.value+(R.elementBonus or 2),"same elements do not resonate; water responds to cold")
for _,event in ipairs(g.events) do check(event.kind~="bonus","same elements do not show a resonance bonus") end
g=setup(2,{{7,"rose"},{8,"bee"}});g:Resolve()
local beeBonus=false;for _,event in ipairs(g.events) do if event.kind=="bonus" and event.a==7 and event.b==8 then beeBonus=true end end
check(beeBonus,"flower bonus animation points to the bee")
g=setup(0,{{7,"rose"},{8,"milk"}});g:Resolve();g.Random=function() return 0 end
check(g:StartBrew(),"brew after fusion");check(g.phase=="exploded","forced explosion");check(g:FinishExplosion(),"finish explosion");check(amount(g,"dream_potion")==1,"explosion preserves product")
g=setup(0,{{7,"rose"},{8,"milk"}});g:Resolve()
local function copy(value) if type(value)~="table" then return value end;local t={};for k,v in pairs(value) do t[k]=copy(v) end;return t end
local loaded=setmetatable(copy(g),G);loaded:Restore()
check(loaded.board[7]==loaded.bag[1] and loaded.board[7].id=="dream_potion","restore reconnects product identity")
check(not loaded:Resolve() and amount(loaded,"dream_potion")==1,"restore cannot replay reaction")
g=G.New(1);g.phase="tax";g.coins=g:Rent();check(g:Pay(),"payment")
check(g.phase=="reward","rent opens the reward step")
check(g.removal==0 and g.reroll==0,"rent hands out no tokens before claiming")
check(g:ClaimRentReward(),"reward claimed");check(g.removal==1 and g.reroll==0,"reward grants one removal token")
check(g.phase=="relic" and #g.choices>0,"reward opens the relic pick")
loaded=setmetatable(copy(g),G);loaded:Restore();check(loaded.phase=="relic" and #loaded.choices>0,"rent relic pick survives reload")
-- 升级令牌 = 手动三选一：有令牌时先出候选，选完才进遗物。
local up=G.New(9);up.phase="tax";up.coins=up:Rent();up.upgradeTokens=1
check(up:Pay() and up:ClaimRentReward(),"token rent flows")
check(up.phase=="upgrade" and #up.upgradeChoices>0,"tokens open the manual upgrade step")
local pickId=up.upgradeChoices[1];local lvBefore=up:LevelOf(pickId)
check(up:PickUpgrade(pickId),"upgrade pick applies")
check(up:LevelOf(pickId)==lvBefore+1 and (up.upgradeTokens or 0)==0,"level rises and the token is spent")
check(up.phase=="relic" and #up.choices>0,"upgrade flows into the relic pick")
local skipUp=G.New(10);skipUp.phase="tax";skipUp.coins=skipUp:Rent();skipUp.upgradeTokens=1
check(skipUp:Pay() and skipUp:ClaimRentReward(),"skip rent flows")
check(skipUp:SkipUpgrade() and skipUp.phase=="relic" and (skipUp.upgradeTokens or 0)==1,"skipping banks the tokens for the next rent")

-- A save stranded on the retired byproduct picker resumes at the reward step.
local stranded=G.New(1);stranded.phase="byproduct"
loaded=setmetatable(copy(stranded),G);loaded:Restore();check(loaded.phase=="reward","retired byproduct phase recovers")
for _,id in ipairs({"dream_potion","poison_crystal","dream_essence","blood_essence","purified_ash"}) do
 check(D.symbols[id]~=nil,"defined product "..id)
 for _,draft in ipairs(D.order) do check(draft~=id,"product absent from drafts") end
end
for _,id in ipairs({"dream_potion","blood_essence","purified_ash","apple","orange","peach","coconut_half","sapphire","antidote","hex","ronin","colossus","astrologer","berserker"}) do
 local card=D.symbols[id]
 check(card.desc:find(tostring(card.value),1,true)~=nil,"description uses final base value: "..id)
end
-- The printed base values must also match actual isolated payouts, not just
-- the shared definition used to construct the descriptions.
for _,id in ipairs({"ronin","colossus","astrologer","berserker"}) do
 local one=setup(2,{{7,id}});one.coins=one:Rent();one:Resolve()
 check(one.values[7]==D.symbols[id].value,"isolated payout matches described base value: "..id)
end
local lone=setup(2,{{7,"ronin"},{8,"cat"}});lone:Resolve()
check(lone.values[7]==2,"ronin's described nearby familiar penalty applies")
local giant=setup(2,{{7,"colossus"},{8,"candy"}});giant:Resolve()
check(giant.values[8]==math.max(0,D.symbols.candy.value-1),"colossus's described neighbor penalty applies")
local cornered=setup(2,{{7,"berserker"}});cornered.coins=0;cornered:Resolve()
check(cornered.values[7]==D.symbols.berserker.value+12,"berserker's described low-coin bonus applies")
check(D.relics.fertilizer.desc:find(tostring(math.floor(R.fertilizerBoost*100+0.5)),1,true)~=nil,"fertilizer description uses configured probability increase")
local sprout=setup(2,{{7,"seed"}});sprout.relics.fertilizer=1
sprout.Random=function(_,limit) return limit and 1 or R.seedChance+R.fertilizerBoost-0.01 end
sprout:Resolve()
check(sprout.board[7].id~="seed" and D.symbols[sprout.board[7].id].rarity>=3,"fertilizer raises seed chance and grown product is at least rare")
local noSprout=setup(2,{{7,"seed"}});noSprout.relics.fertilizer=1
noSprout.Random=function(_,limit) return limit and 1 or R.seedChance+R.fertilizerBoost+0.01 end
noSprout:Resolve()
check(noSprout.board[7].id=="seed","one fertilizer alone does not guarantee seed germination")
check(D.symbols.dame.desc:find(tostring(R.martini),1,true)~=nil and D.symbols.martini.desc:find(tostring(R.martini),1,true)~=nil,"queen and cup descriptions share configured reward")
check(D.symbols.fruitking.desc:find(table.concat(R.fruitGain,"/"),1,true)~=nil,"fruit king describes all rarity growth tiers")
g=setup(2,{{7,"fruitking"},{8,"pumpkin"}});g:Resolve()
check(g.board[7].growth==R.fruitGain[3],"fruit king can consume legendary fruit at high-rarity growth")
check(amount(g,"pumpkin")==0 and amount(g,"seed")==1,"fruit king consumes fruit and produces a seed when fresh")
print("PASS: "..n.." transmutation assertions")
BAL.set=savedSet;BAL.resonance=savedRes;BAL.pivot=savedPivot;BAL.diversity=savedDiv
