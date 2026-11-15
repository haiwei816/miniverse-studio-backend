-- Pure presentation queue. Reading the ledger never changes settlement or RNG.
local M={}
local function uidKey(uid)
 local n=tonumber(uid)
 return n and n%1==0 and string.format("%.0f",n) or tostring(uid)
end
local function copy(source)
 local out={};for key,value in pairs(source) do out[key]=value end;return out
end
function M.Build(game)
 local result={events={},initial={},production={},extra={},usesLedger=false}
 local details=game.incomeDetails
 if not details or details.turn~=game.turn then
  for _,event in ipairs(game.events or {}) do result.events[#result.events+1]=copy(event) end
  return result
 end
 result.usesLedger=true
 local entries={}
 for _,entry in pairs(details.byUid) do entries[#entries+1]=entry end
 table.sort(entries,function(a,b) return a.slot==b.slot and a.uid<b.uid or a.slot<b.slot end)
 local drinking={}
 local accounted=0
 for slot=1,21 do result.initial[slot]=0;result.production[slot]=0;result.extra[slot]=0 end
 -- Structural reactions retain their art, while money comes from ledger amounts.
 for _,original in ipairs(game.events or {}) do
  local event=copy(original)
  if event.effect=="drink" then
   local entry=details.byUid[uidKey(event.actorUid)]
   local gain=entry and entry.reactions[event.reactionIndex or 0]
   if gain then
    event.incomeDelta=gain.amount;event.value=gain.amount
    drinking[uidKey(event.actorUid)..":"..uidKey(event.reactionIndex)]=true
    accounted=accounted+gain.amount
   end
   result.events[#result.events+1]=event
  elseif event.kind=="ritual" then
   event.incomeOnlyVisual=true;result.events[#result.events+1]=event
  elseif event.kind~="multiply" and (event.kind~="bonus" or event.value==0) then
   if event.kind=="devour" then event.text=event.text:gsub("%s*%+[%d%.]+","") end
   result.events[#result.events+1]=event
  end
 end
 -- Growth is a separate, non-monetary cue. Only show it on the surviving
 -- instance: an ingredient may have grown and then been consumed this turn.
 local growthRows={};local maxGrowth=0
 for _,entry in ipairs(entries) do
  local survivor=game.board[entry.slot]
  if survivor and uidKey(survivor.uid)==uidKey(entry.uid) then
   growthRows[entry.uid]=entry.permanentChanges or {}
   maxGrowth=math.max(maxGrowth,#growthRows[entry.uid])
  end
 end
 for depth=1,maxGrowth do
  local batch
  for _,entry in ipairs(entries) do
   local change=growthRows[entry.uid] and growthRows[entry.uid][depth]
   if change then
    if not batch or #batch.items>=3 then
     batch={kind="growth",items={},duration=0.55,text="材料永久成长"}
     result.events[#result.events+1]=batch
    end
    batch.items[#batch.items+1]={slot=entry.slot,uid=entry.uid,amount=change.amount,
     label=change.label,nextTurn=change.nextTurn}
   end
  end
 end
 local rows={}
 local maxSteps=0
 for _,entry in ipairs(entries) do
  local steps={}
  for _,original in ipairs(entry.steps or {}) do
   if math.abs(original.amount)>1e-9 or (original.factor and original.factor~=1) then
    local step=copy(original);local previous=steps[#steps]
    -- Repeated identical additive sources can share one readable number.
    if previous and not previous.factor and not step.factor and previous.label==step.label
      and previous.sourceUid==step.sourceUid and previous.after==step.before and previous.amount*step.amount>0 then
     previous.amount=previous.amount+step.amount;previous.after=step.after
    else steps[#steps+1]=step end
   end
  end
  rows[entry.uid]=steps;maxSteps=math.max(maxSteps,#steps)
 end
 local money={}
 local function append(entry,step,section)
  local source=step.sourceUid and details.byUid[uidKey(step.sourceUid)]
  money[#money+1]={slot=entry.slot,uid=entry.uid,source=source and source.slot,
   before=step.before,after=step.after,delta=step.amount,factor=step.factor,label=step.label,section=section}
  accounted=accounted+step.amount
 end
 -- One step per material per pass preserves every material's calculation order.
 for depth=1,maxSteps do
  for _,entry in ipairs(entries) do
   local step=rows[entry.uid][depth]
   if step then append(entry,step,"production") end
  end
 end
 for _,entry in ipairs(entries) do
  for index,gain in ipairs(entry.reactions or {}) do
   if not drinking[uidKey(entry.uid)..":"..uidKey(index)] and gain.amount~=0 then
    append(entry,{amount=gain.amount,label=gain.label,sourceUid=gain.sourceUid},"extra")
   end
  end
  if (entry.directReward or 0)~=0 then append(entry,{amount=entry.directReward,label=entry.reason or "反应收益"},"extra") end
 end
 for slot,value in pairs(game.values or {}) do
  if not game.board[slot] and not (details.bySlot[slot] or details.bySlot[tostring(slot)]) and value~=0 then
   append({slot=slot},{amount=value,label="空位收益"},"extra")
  end
 end
 -- Global recipe rewards and the final integer rounding have no material owner.
 local residual=(game.basePayout or 0)-accounted
 if math.abs(residual)>1e-7 then
  append({slot=0},{amount=residual,label=math.abs(residual)<1 and "整锅收益取整" or "整锅反应收益"},"global")
 end
 local pace=0.42
 local batch=nil;local occupied={}
 for _,item in ipairs(money) do
  -- Never put successive updates for the same cell into one parallel batch.
  if not batch or #batch.items>=3 or occupied[item.slot] then
   batch={kind="income",items={},duration=pace,text="金币累加"};occupied={}
   result.events[#result.events+1]=batch
  end
  batch.items[#batch.items+1]=item;occupied[item.slot]=true
 end
 result.accounted=accounted
 return result
end
return M
