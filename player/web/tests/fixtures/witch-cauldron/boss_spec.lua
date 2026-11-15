local G=require "Game"
local D=require "Data"
local B=D.balance
-- Economy multiplier at the showdown night: HP and night earnings are scaled by it.
local M=G.New(1):CoinMul(B.winRound or 12)
local n=0
local function expectDamage(shields,earned,ultimate)
 local factor=B.boss.shieldFactorBase+B.boss.shieldFactorStep*shields
 return math.floor(earned*B.boss.damagePerEarned*factor*(ultimate and B.boss.ultimateMultiplier or 1))
end
local function check(condition,message) n=n+1;assert(condition,message) end

-- Deterministic table: shields x night earnings. HP=900 from B.boss (x economy multiplier).
local function plan(broken,earned,ultimate)
 local g=G.New(31)
 g.mainline={}
 for i,slot in ipairs(B.mainline.slots) do
  if i<=broken then g.mainline[slot.id]=1 end
 end
 if broken==4 or ultimate then g.mainline.ultimate=1 end
 g.roundEarned=earned*M
 return g:BossPlan()
end
local cases={
 -- 第 12 夜中位收益约 10,000 币：需要 4 盾（或 3 盾+万灵汤）才砍得动 5,500 血的死神。
 {0,7000000,false,"lost"},{1,7000000,false,"lost"},{2,7000000,false,"lost"},
 {3,7000000,false,"victory"},{4,7000000,false,"victory"},
 {4,1000000,false,"lost"},{0,30000000,false,"victory"},{2,10000000,false,"victory"},
}
for _,c in ipairs(cases) do
 local p=plan(c[1],c[2],c[3])
 check(p.damage==expectDamage(c[1],c[2]*M,c[3] or c[1]==4),"damage for "..c[1].." shields at "..(c[2]*M).." earned: got "..p.damage)
 check(p.result==c[4],"result for "..c[1].." shields at "..(c[2]*M).." earned")
end

-- Deterministic: same state twice, same numbers (no dice in the settlement).
local a,b=plan(3,2080,false),plan(3,2080,false)
check(a.damage==b.damage and a.result==b.result,"boss settlement never rolls dice")

-- Earnings only come from this night: bottle adds, pay resets.
local g=G.New(41);g.bag={};g.uid=0;g:ClearPot()
local s=g:Add("candy");g.board[11]=s;g.fresh={s.uid};g.phase="arrange";g.turn=1;g.temperature=0
g:Resolve();g:StartBrew("")
if g.phase=="exploded" then g:FinishExplosion() else g:Bottle() end
check(g.roundEarned==g.last,"night earnings equal the collected payout")

-- Final rent goes to the showdown, not the byproduct picker.
local final=G.New(43);final.mainline={sleep=1,acid=2,mist=3,ultimate=4};final.round=12;final.paid=11
final.coins=20000000*M;final.phase="tax";final.roundEarned=7000000*M
check(final:Pay(),"final rent can be paid")
check(final.phase=="victory","four shields win the showdown")
check(final.boss and final.boss.damage==expectDamage(4,7000000*M,true),"boss numbers are stored for the result screen")
check(final.won==true,"win flag kept for the ladder")

-- A thin run loses on the final night.
local thin=G.New(44);thin.mainline={};thin.round=12;thin.paid=11;thin.coins=20000000*M;thin.phase="tax";thin.roundEarned=1400*M
check(thin:Pay(),"thin run can still pay rent")
check(thin.phase=="lost" and thin.boss.damage==expectDamage(0,1400*M,false),"no shields means a token damage")
check(thin.won==false,"losing the showdown does not set the win flag")

-- Earlier rents open the reward step, then the relic pick, never a material draft.
local mid=G.New(45);mid.round=3;mid.paid=2;mid.coins=5000*M;mid.phase="tax";mid.roundEarned=400*M
check(mid:Pay(),"mid rent")
check(mid.phase=="reward" and mid.boss==nil,"only the final rent is a showdown")
check(mid.removal==0 and mid.reroll==0,"rent waits for the player to claim its reward")
check(mid:ClaimRentReward(),"rent reward can be claimed")
check(mid.removal==1 and mid.reroll==0,"rent hands out the removal token")
check(mid.phase=="relic" and #mid.choices>0,"rent reward flows into the relic pick")
check(mid.roundEarned==0,"nightly earnings reset after a normal rent")

-- Rents after the showdown stay plain; the boss never triggers again.
local after=G.New(47);after.mainline={sleep=1,acid=2,mist=3,ultimate=4};after.round=13;after.paid=12
after.coins=20000000*M;after.phase="tax";after.roundEarned=900*M
check(after:Pay(),"rent after the showdown can be paid")
check(after.boss==nil and after.phase=="reward","later nights skip the showdown and open the reward step")
check(after.round==14 and after.roundEarned==0,"later nights advance normally")

-- Old saves have no boss or night counter and must not break.
local legacy=G.New(46);legacy.mainline={sleep=1,acid=2,mist=3,ultimate=4};legacy.roundEarned=nil;legacy.boss=nil
legacy:Restore()
check(legacy.roundEarned==0,"old saves start with zero nightly earnings")
local p=legacy:BossPlan()
check(p and p.damage==0 and p.result=="lost","old saves fall back to zero damage")

-- Legacy rules have no boss table at all.
local legacyData=require "legacy_heat_v3.Data"
check(legacyData.balance.boss==nil and legacyData.balance.mainline==nil,"legacy rules keep no showdown")

print("PASS: "..n.." boss assertions")
