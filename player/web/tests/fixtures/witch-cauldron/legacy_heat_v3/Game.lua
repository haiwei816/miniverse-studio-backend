local D=require "legacy_heat_v3.Data"
local B=D.balance
local R=B.rules
local BOARD_SIZE=B.boardSize or 21
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
 self.coins=B.startCoins;self.round=1;self.paid=0;self.left=B.spins[1];self.turn=0;self.removal=0;self.reroll=0
 self.total=0;self.last=0;self.phase="ready";self.choices={};self.events={};self.values={};self.visualBoard={};self.won=false
 self.draftContext="spin";self.lastGrant={removal=0,reroll=0};self.cardboardAge=0;self.lastOutcome=""
 self.blastChance=0;self.brewRewards=0;self.additiveUses={};self.relicContext="rent"
 self.temperature=0;self.selectedFuel=1;self.spinFuel=1;self.spinRaised=false;self.discoveryActive=false;self.selectedAdditive="";self.levels={};self.upgradeTokens=0;self.brewTally=0;self.familyBoost={}
 for _,id in ipairs({"rose","cat","eye","cultist","spade","amethyst"}) do self:Add(id) end
 self:ClearPot()
end
function G:ClearPot()
 self.board={};for i=1,BOARD_SIZE do self.board[i]=false end
 self.visualBoard={};self.values={};self.drawn={};self.fresh={};self.buffSeen={}
 self.potValue=0;self.basePayout=0
 self.brewBoost=0;self.lostPayout=0;self.heat="";self.additive="";self.brewRiskAtOpen=0;self.blastChance=0
 self.brewRiskModifier=0;self.brewBoostModifier=0;self.brewRefund=0;self.pendingBrewReward=false;self.brewRewardOdds={}
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
 self.selectedAdditive=B.brew.additives[self.selectedAdditive] and self.selectedAdditive or ""
 if type(self.levels)~="table" then self.levels={} end
 if type(self.familyBoost)~="table" then self.familyBoost={} end
 self.upgradeTokens=math.max(0,math.floor(tonumber(self.upgradeTokens) or 0))
 self.brewTally=math.max(0,math.floor(tonumber(self.brewTally) or 0))
 -- JSON decodes references as separate tables; reconnect board entries to the bag.
 local byId={};for _,s in ipairs(self.bag) do byId[s.uid]=s end
 for i=1,BOARD_SIZE do local s=self.board[i];self.board[i]=s and byId[s.uid] or false end
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
 local s={id=id,uid=self.uid,growth=0,age=0,bank=0};self.bag[#self.bag+1]=s;return s
end
function G:Rent()
 local r=B.rents[self.round]
 if r then return r end
 local last=B.rents[#B.rents]
 return math.floor(last*(B.endlessGrowth^(self.round-#B.rents))/50)*50
end
function G:Spins() return B.spins[self.round] or B.endlessSpins end
function G:Count(id) local n=self.relics[id];return n==true and 1 or (n or 0) end
function G:Has(id) return self:Count(id)>0 end
function G:Emit(kind,a,b,text,value)
 self.events[#self.events+1]={kind=kind,a=a or 0,b=b or a or 0,text=text or "",value=value or 0}
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
  elseif s.id=="hex" then luck=luck/R.hexTedium end
 end end
 -- Give higher rarities their share first; take the remainder from Common.
 local remaining=100
 for rarity=4,2,-1 do weights[rarity]=math.min(remaining,weights[rarity]*luck);remaining=remaining-weights[rarity] end
 weights[1]=remaining;return weights
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
  for r=minimum,4 do local list=available[r];if list then return list[self:Random(#list)] end end
  return nil
 end
 local roll=self:Random()*total;local last
 for r=1,4 do local list=available[r]
  if list and weights[r]>0 then
   last=list;roll=roll-weights[r];if roll<0 then return list[self:Random(#list)] end
  end
 end
 return last and last[self:Random(#last)] or nil
end
function G:DrawChoices(relic)
 local chosen={};self.choices={};local weights=self:Weights(relic)
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
end
function G:Destroy(i,source,bonus,label,deathMultiplier)
 local s=self.board[i];if not s then return false end
 local def=D.symbols[s.id];self.board[i]=false
 -- Keep earlier reaction rewards even if their producer is consumed later.
 self.bonus=self.bonus+(self.actorBonus[i] or 0);self.actorBonus[i]=nil
 for k=#self.bag,1,-1 do if self.bag[k].uid==s.uid then table.remove(self.bag,k);break end end
 local reward=R.blackPepper*self:Count("pig");local intrinsic=def.tags.chest or 0
 if s.id=="thief" then intrinsic=R.thiefPerSpin*s.age
 elseif s.id=="billionaire" then intrinsic=R.billionaire
 elseif def.tags.void then intrinsic=R.void end
 reward=reward+intrinsic*(deathMultiplier or 1)
 if bonus and bonus~=0 then
  if source~=i and self.board[source] then self.actorBonus[source]=(self.actorBonus[source] or 0)+bonus
  else reward=reward+bonus end
 end
 if def.tags.ore then
  reward=reward+R.miningPick*self:Count("pick")
  for _=1,def.tags.ore do self:Add(assert(self:Pick(D.orePool,self:Weights(false),1))) end
 elseif s.id=="banana" then self:Add("peel")
 elseif s.id=="coconut" then self:Add("coconut_half");self:Add("coconut_half")
 elseif s.id=="peach" then self:Add("seed")
 elseif s.id=="urn" or s.id=="bigurn" then
  self.board[i]=self:Add("spirit");if s.id=="bigurn" then self:Add("spirit") end
 end
 self.bonus=self.bonus+reward
 self:Emit("devour",source,i,(label or "销毁")..((reward+(bonus or 0))~=0 and " +"..(reward+(bonus or 0)) or ""),reward+(bonus or 0))
 self.events[#self.events].afterId=self.board[i] and self.board[i].id or ""
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
 self.events={};self.values={};self.bonus=0;self.actorBonus={}
 for i=1,BOARD_SIZE do local s=self.board[i]
  if s and s.id=="seed" and self:IsFresh(s.uid) then
   local chance=R.seedChance+R.fertilizerBoost*self:Count("fertilizer")
   local ci=COORDS[i]
   for j,t in ipairs(self.board) do if t and j~=i then
    local cj=COORDS[j]
    local nearby=ci and cj and math.abs(cj.x-ci.x)<=1 and math.abs(cj.y-ci.y)<=1
    if (nearby and (t.id=="rain" or t.id=="sun")) or (t.id=="sun" and self:Has("hat")) then chance=chance+R.seedBoost end
   end end
   if (ci and ci.y==4) or self:Random()<math.min(1,chance) then
    s.id=assert(self:Pick(D.seedPool,self:Weights(false),self:Has("fertilizer") and 3 or 1))
    s.age=0;s.countedTurn=nil;self:Emit("grow",i,i,"生长！")
   end
  end
 end
 self.visualBoard={};local dice={};local triggerDie
 for i=1,BOARD_SIZE do local s=self.board[i];self.visualBoard[i]=s
  if s then
   if self:IsFresh(s.uid) then self:Age(s) end;local sides=D.symbols[s.id].tags.die
   if sides then if self:IsFresh(s.uid) then s.roll=self:RollDie(sides) end;dice[i]=s.roll or 1;if dice[i]==1 then triggerDie=i end end
   if s.id=="gambler" and self:IsFresh(s.uid) then s.bank=s.bank+R.gamblerPerSpin end
  end
 end
 -- Fixed actor order is a prototype rule; see the source/audit document.
 if self:Has("guillotine") then
  for i=1,BOARD_SIZE do if self.board[i] and self.board[i].id=="billionaire" then self:Destroy(i,i,0,"断头台！") end end
 end
 for _,actor in ipairs({"necro","key","magic_key","cat","digger","miner","geo","fruitking","troll","dame","peel","gambler"}) do
  for i=1,BOARD_SIZE do local s=self.board[i]
   if s and s.id==actor then
    if actor=="gambler" and triggerDie then self:Destroy(i,i,s.bank,"赌命兑现！")
    else for _,j in ipairs(self:Neighbors(i)) do
     if not self.board[i] then break end
     local target=self.board[j]
     if target then local id=target.id;local def=D.symbols[id];local tags=def.tags
      if actor=="necro" and tags.human then self:Destroy(j,i,R.zaroff,"收割")
      elseif (actor=="key" or actor=="magic_key") and tags.chest then
       self:Destroy(j,i,0,"开箱暴富！",actor=="magic_key" and R.magicKey or 1);self:Destroy(i,i,0,"钥匙耗尽");break
      elseif actor=="cat" and id=="milk" then self:Destroy(j,i,R.milk,"喝掉牛奶")
      elseif actor=="digger" and (id=="urn" or id=="bigurn") then self:Destroy(j,i,R.hooligan,"掘墓")
      elseif actor=="miner" and tags.ore then self:Destroy(j,i,R.miner,"采矿")
      elseif (actor=="geo" and (tags.ore or tags.geo)) or (actor=="fruitking" and tags.fruit and def.rarity<=2) then
       self:Destroy(j,i,0,"吞噬成长！永久+1");s.growth=s.growth+R.growth
      elseif actor=="troll" and tags.monkey then self:Destroy(j,i,R.monkey*(def.value+target.growth),"吞噬水果")
      elseif actor=="dame" and id=="martini" then self:Destroy(j,i,R.martini,"饮尽马提尼")
      elseif actor=="peel" and id=="thief" then self:Destroy(j,i,0,"怪盗滑倒");self:Destroy(i,i,0,"香蕉皮销毁");break end
     end
    end end
   end
  end
 end
 local count={};local suits={};local extra={};local mult={};local emptyBonus={};local voidExpire={}
 for i=1,BOARD_SIZE do local s=self.board[i];extra[i]=0;mult[i]=1
  if s then
   count[s.id]=(count[s.id] or 0)+1;local suit=D.symbols[s.id].tags.suit;if suit then suits[suit]=(suits[suit] or 0)+1 end
  end
 end
 for i,s in ipairs(self.board) do if s then
  if D.symbols[s.id].tags.void then
   local n=0;for _,j in ipairs(self:Adjacent(i)) do if not self.board[j] then n=n+1;emptyBonus[j]=(emptyBonus[j] or 0)+1 end end
   if n==0 then voidExpire[#voidExpire+1]=i end
  end
  if self.board[i] then for _,j in ipairs(self:BuffTargets(i)) do local target=self.board[j];local tags=D.symbols[target.id].tags;local factor=1
   if s.id=="witch" and tags.witch then factor=R.witch
   elseif (s.id=="bee" or s.id=="rain") and target.id=="rose" then factor=s.id=="bee" and R.bee or R.rain
   elseif s.id=="sun" and target.id=="rose" then factor=R.sun
   elseif s.id=="midas" and target.id=="candy" then factor=R.midas
   elseif s.id=="joker" and tags.suit then factor=R.joker
   elseif s.id=="dame" and tags.gem then factor=R.dame end
   if s.id=="rose" and target.id=="bee" then extra[j]=extra[j]+1 end
   if D.symbols[s.id].tags.suit and tags.suit==D.symbols[s.id].tags.suit then extra[j]=extra[j]+1 end
   if factor>1 then
    local pair=s.uid..":"..target.uid;local seen=false;for _,key in ipairs(self.buffSeen) do if key==pair then seen=true;break end end
    if tags.growOnBuff and not seen then target.growth=target.growth+R.growth;self.buffSeen[#self.buffSeen+1]=pair end
    mult[j]=mult[j]*factor;if self:IsFresh(s.uid) or self:IsFresh(target.uid) then self:Emit("multiply",i,j,"×"..mult[j],mult[j]) end
   end
  end end
 end end
 local income=self.bonus;local expire={}
 for i=1,BOARD_SIZE do local s=self.board[i]
  if s then
   local def=D.symbols[s.id];local value=def.value+s.growth+extra[i]
   if dice[i] then value=dice[i]+s.growth+extra[i] end
   if s.id=="cat" then value=value+R.pizza*self:Count("pizza") end
   if s.id=="owl" and s.age%R.owlEvery==0 then value=value+R.owlBonus end
   if s.id=="crow" and s.age%R.crowEvery==0 then value=value-math.max(0,R.crowPenalty-R.taxEvasion*self:Count("candle")) end
   if s.id=="thief" then value=value+math.min(1,R.taxEvasion*self:Count("candle")) end
   if s.id=="cultist" then value=value+count.cultist-1+(count.cultist>=3 and 1 or 0) end
   if def.tags.ritual then value=value+R.ritualCandle*self:Count("bible") end
   if s.id=="diamond" or s.id=="pumpkin" then value=value+count[s.id]-1 end
   if (s.id=="emerald" or s.id=="ruby" or s.id=="strawberry") and count[s.id]>=2 then value=value+1 end
   if def.tags.suit and suits[def.tags.suit]>=3 then value=value+1 end
   if s.id=="midas" and self:IsFresh(s.uid) then self:Add("candy") end
   if s.id=="rabbit" then
    -- The first ten payouts use the old value; the upgrade starts next payout.
    if self:IsFresh(s.uid) and s.age==R.rabbitAfter then s.growth=s.growth+R.rabbitBonus end
    value=value*self:MagnetMultiplier()
   end
   if (COORDS[i] and COORDS[i].y==0) and (def.tags.volatile or s.id=="spirit") then mult[i]=mult[i]*2 end
   value=value*mult[i]*self:DesignMult(s.id);self.values[i]=value;income=income+value
   if self.actorBonus[i] then income=income+self.actorBonus[i]*mult[i];self.bonus=self.bonus+self.actorBonus[i]*mult[i] end
   if self:IsFresh(s.uid) and s.id=="spirit" and s.age>=R.spiritLife then expire[#expire+1]=i end
  elseif emptyBonus[i] then self.values[i]=emptyBonus[i];income=income+emptyBonus[i] end
 end
 local before=self.bonus
 for _,i in ipairs(expire) do self:Destroy(i,i,0,"怨灵消散") end
 for _,i in ipairs(voidExpire) do self:Destroy(i,i,0,"虚空消散",mult[i]) end
 income=income+self.bonus-before
 self.basePayout=math.max(0,math.floor(income));self.potValue=self.basePayout
 self:PrepareBrew();self.phase="review"
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
 if self:Random()*100<plan.risk then self.lostPayout=plan.payout;self.phase="exploded"
 else
  self.phase="brewed"
  self.discoveryActive=self.spinFuel==2 and self.spinRaised and self:Random()*100<self:DiscoveryChance()
 end
 return true
end
function G:Deadline()
 self.phase=self.left<=0 and (self.coins>=self:Rent() and "tax" or "lost") or "ready"
end
local function finishSpin(self,payout,outcome)
 self.coins=self.coins+payout;self.total=self.total+payout;self.last=payout
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
 self.choices={};self.phase=(self.paid>=#B.rents) and "victory" or "ready"
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
function G:Pay()
 if self.phase~="tax" or self.coins<self:Rent() then return false end
 self.coins=self.coins-self:Rent();self.paid=self.round;self.lastGrant={removal=0,reroll=0}
 self.blastChance=0;self.brewRewards=0;self.additiveUses={};self.temperature=0;self.selectedFuel=1;self.spinFuel=1;self.spinRaised=false;self.discoveryActive=false
 if self.paid>=B.emailFirstPayment and (self.paid-B.emailFirstPayment)%B.emailEvery==0 then
  self.lastGrant={removal=B.emailRemoval,reroll=B.emailReroll}
  self.removal=self.removal+B.emailRemoval;self.reroll=self.reroll+B.emailReroll
 end
 if self.paid>=(B.winRound or #B.rents) then self.won=true end
 self.round=self.round+1;self.left=self:Spins();self.draftContext="rent";self.phase="draft";self:DrawChoices(false)
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
