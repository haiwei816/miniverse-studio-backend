-- Full-board building followed by one upfront heat/additive decision.
local B={
 id="cauldron-transmutation-v1",
 floor=1,
 checkedAt="2026-10-30",
 -- Initial tuning for whole-board payouts; normal play can finish without brewing.
 rents={100, 380, 1350, 5200, 17000, 46000, 115000, 280000, 720000, 1850000, 4800000, 12800000,
        16000000, 35000000, 78000000, 172000000, 380000000, 840000000, 1850000000, 4100000000},
 spins={5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10},
 -- Draw odds: Common / Uncommon / Rare / Legendary based on completed rent payments
 symbolOdds={
  {100, 0, 0, 0},
  {85, 15, 0, 0},
  {70, 26, 4, 0},
  {52, 36, 10, 2},
  {40, 40, 15, 5},
  {30, 40, 20, 10},
  {20, 40, 25, 15}
 },
 itemOdds={
  {100, 0, 0, 0},
  {88, 12, 0, 0},
  {76, 22, 2, 0},
  {70, 26, 3.5, 0.5},
  {64, 30, 5, 1},
  {58, 33, 7, 2},
  {52, 36, 9, 3}
 },
 -- Main line: four slots fed by the existing three recipes; no new product, no new art.
 -- assist: 候选权重倍数，仅在未达成且已过 assistFrom 夜时生效（保底，避免抽不到卡导致主线无解）。
 mainline={
  assist=3,
  slots={
   {id="sleep",name="安眠露",shield="恐惧护盾",potion="dream_potion",maxTemp=1,
    materials={"rose","milk"},assistFrom=3,hint="曼陀罗花 + 幽灵牛奶，文火(0~1)熬成"},
   {id="acid",name="熔甲酸",shield="镰甲护盾",potion="poison_crystal",minTemp=3,
    materials={"toadstool","puffer"},assistFrom=6,hint="残次毒蘑菇 + 剧毒河豚，大火(≥3)熬成"},
   {id="mist",name="织梦雾",shield="幻影护盾",potion="dream_essence",
    materials={"peel","thief"},assistFrom=9,hint="封印符 + 潜行梦魇，任意炉温合成"},
   {id="ultimate",name="万灵灭世汤",shield="死神本体",
    needs={"dream_potion","poison_crystal","dream_essence"},
    hint="一锅同时熬出安眠露、熔甲酸、织梦雾三瓶秘药"}
  }
 },
 winRound=12,
 -- 房租奖励只给驱魔符；骨骰改成稀缺资源（只靠遗物/邮件）。
 rentReward={removal=1,reroll=0},
 -- Final night settlement. HP calibrated from tests/probe_night_income.py (see docs §8):
 -- round-12 income p25/p50/p75 = 1684/2080/2684, so 3 shields ~0.8-1.3x HP and 4 shields ~2x.
 boss={
  hp=2800000,
  shieldCount=4,
  damagePerEarned=0.6,
  shieldFactorBase=0.2,
  shieldFactorStep=0.2,
  ultimateMultiplier=2
 },
 endlessGrowth=1.18,
 endlessStart=900,
 endlessIncrement=150,
 endlessSpins=10,
 spinCost=0,
 boardSize=21,
 rowSizes={3,5,5,5,3},
 brew={
  maxRisk=100,
  fuelNames={"不加柴", "加1根", "加2根"},
  temperature={baseBoost=25,baseRisk=1,boostPerLevel=26,riskPerLevel=7.5,discoveryPerLevel=5,discoveryMax=25},
  additiveOrder={"frost_dew","dragon_dust","safety_seal"},
  -- Unlock as relics; each owned copy permits one upfront use per rent cycle.
  additives={
   frost_dew={name="寒霜露",boost=20,risk=-26,refund=0},
   dragon_dust={name="龙息粉",boost=150,risk=6,refund=0},
   safety_seal={name="保温符",boost=0,risk=0,refund=50}
  },
  materials={
   toadstool={risk=1,boost=2},unstable={risk=2,boost=4},
   puffer={risk=4,boost=10},poison_crystal={risk=3,boost=8},antidote={risk=-3,boost=0},rose={risk=0,boost=2}
  }
 },
 -- Symbol levels + family investment: the player's build-up axis.
 -- 成长靠“把卡养大”：每级 ×1.22，满级 18 级 ≈ ×29，再叠家族投资 +60% ≈ ×47
 upgrade={
  levels={1.0,1.10,1.21,1.33,1.46,1.61,1.77,1.95},
  maxLevel=8,
  tokenEvery=4,
  familyRate=0.06,
  familyCap=0.40,
  choiceCount=3,
  auto=false
 },
 rarityValueBonus={0,1,3,6},
 -- 流派共鸣：场上同流派张数超过 base 后，该流派全体 ×(1+step*(n-base))，cap 封顶
 set={base=1,step=0.52,cap=40,orphan=0.45},
 -- 连锁共振：一张卡每多一个倍率来源，额外 ×(1+step)，叠得越密越爆
 resonance={step=0.22,cap=20},
 -- 流派仪式：交租时若该流派在场张数≥min，永久累积增幅（复利，cap 封顶）
 rite={minBase=3,minStep=4,base=1.25,perGroup=0.25,perOmni=0.15,soloGrowth=1.15,cap=3000,familyGrowthPower={[1]=0.7}},
 -- 百花齐放：场上流派越杂，全场产出越高（奖励灵活变通，硬追单派吃不到）
 diversity={step=0.0,cap=1.0},
 -- 万能卡：不属任何流派，但给全场加成，并折算仪式门槛（“主流派 + 万能”能拿到满速）
 universal={mulStep=0.18,mulCap=4.0,riteCredit=1},
 -- 牌袋臃肿惩罚：超过 free 张后每张 -step（参考幸运房东的稀释感），逼玩家克制拿牌
 bloat={free=17,step=0.055,cap=0.70},
 -- 灵活变通：第一次拿到新流派的材料，永久提高全场产出（硬追单派吃不到）
 pivot={gain=0.06,cap=0.6},
 startCoins=1,
 -- 全局经济倍率：第 n 夜的收入 / 房租 / Boss 血量 / 存款全部 × coinBase*coinGrowth^(n-1)。
 -- 前期保持小数字，后期租金到千万级；无尽模式封顶在 coinCapRound 夜。
 economy={coinBase=1, coinGrowth=1, coinCapRound=12},
 emailFirstPayment=3,
 emailEvery=2,
 emailRemoval=1,
 emailReroll=1,
 rules={
  -- Witch & Familiar synergies (redesigned ratios)
  milk=30,          -- Black cat drinks ghost milk (+30 per bottle)
  owlEvery=2,       -- Owl triggers every 2 rounds (+2 instead of every 3)
  owlBonus=2,
  crowEvery=3,      -- Crow triggers every 3 rounds (-2 instead of every 4)
  crowPenalty=2,
  witch=2,          -- Witch buffs familiars x2
  -- Herbal & Fruit synergies
  bee=2,
  rain=2,
  sun=3.0,            -- Blood moon multiplier for adjacent roses
  seedChance=0.30,  -- Seed growth rate (30% instead of 25%)
  seedBoost=0.40,
  growth=1,
  -- Undead & Graveyard synergies
  spiritLife=3,     -- Wandering spirit lives for 3 cycles (higher burst)
  hooligan=8,       -- Gravedigger smashes urn for +8 (instead of 6)
  zaroff=18,        -- Lich / Necromancer harvests for +18 (instead of 25)
  -- Mineral & Alchemy synergies
  miner=120,        -- Fire salamander consumes ore for +120
  magicKey=2.5,     -- Magic key multiplier 2.5x (instead of 3x)
  -- Gambling & Halloween party synergies
  gamblerPerSpin=3, -- Gambler pot increases by +3 (instead of 2)
  thiefPerSpin=3,   -- Phantom thief payout 3x (instead of 4x)
  monkey=5,         -- Troll / Monkey feast 5x (instead of 6x)
  joker=2.2,
  dame=1.7,
  dame2=1.30,       -- 第 2 位血族女王（递减叠乘）
  dame3=1.15,       -- 第 3 位血族女王
  dameStack=3,      -- 同一颗宝石最多吃三位血族女王
  sunStack=3,       -- 同一朵花最多吃三张猩红血月
  sun2=2.0,         -- 第 2 张猩红血月（递减）
  sun3=1.4,         -- 第 3 张猩红血月
  roseCap=0,        -- 曼陀罗花从相邻倍率拿到的总倍率上限（0 = 不限）
  jokerStack=3,     -- 一颗魔晶最多吃三张炼金法阵（99 = 不限）
  joker2=1.6,       -- 第 2 张炼金法阵（递减）
  joker3=1.3,       -- 第 3 张炼金法阵
  sameSymbolCap=6,  -- 灵魂钻石 / 巨型南瓜的同种加成最多 +6
  geoEatsOre=false, -- 科学怪人只吃宝石类；矿石转化交给火蜥蜴与大火
  geoOreDrop=false,
  -- 永久成长封顶：只防无限刷，不再把成长捂死
  growCap={amethyst=40,pear=60,fruitking=40,geo=150,taotie=60},
  fruitGain={2,2,3},-- 果王吞普通/罕见/稀有果实的永久成长
  elementBonus=10,   -- 元素与炉温匹配的额外金币
  elementPair=3,    -- 相邻异属性魔晶互相加币
  gemPair=0,        -- 相邻两颗宝石互相加币（宝石流前期强度旋钮）
  essenceBonus=150,  -- 封印符 + 潜行梦魇合成织梦雾的超度奖金
  sealIngredientBonus=2, -- 梦网丝、封印符、潜行梦魇的基础产出提升
  dreamEssenceBoost=6, -- 每瓶织梦雾给予同场使魔与亡灵的基础产出
  hydraMin=3, hydraPerPoison=1, hydraMultiplier=1.2, -- 毒素流传奇扩散
  martini=35,       -- Dame drinks martini for +35 (instead of 40)
  midas=2.5,        -- Pumpkin Midas buffs candy 2.5x (instead of 3x)
  -- Relic & Witch Artifact powers
  luckyCat=1.25,    -- Black cat paw high-rarity boost x1.25 (instead of 1.3)
  shinyPebble=1.15, -- Shiny pebble boost x1.15 (instead of 1.1)
  hexTedium=1.25,   -- Totem penalty divisor /1.25 (instead of 1.3)
  goldenCarrot=3.0, -- Magnetic star boost 3x (instead of 5x, preventing inflation)
  miningPick=40,    -- Alchemical pickaxe +40 per ore
  pizza=8,
  ritualCandle=1,
  fertilizerBoost=0.40,
  taxEvasion=1,
  cardboardEvery=8, -- Witch broom grants removal every 8 spins (instead of 10)
  blackPepper=1,
  billionaire=32,   -- Greedy spirit death rattle +32 (instead of 39)
  void=6,           -- Void fruit collapse +6 (instead of 8)
  rabbitAfter=8,    -- Moon rabbit awakens after 8 cycles (instead of 10)
  rabbitBonus=2,
  -- Reduce this brew's chance only; the floor is 0.
  pressureBonus=2,
  outletThreshold=30,
  outletBoost=25
 }
}

function B.Odds(paid, relic)
 local rows = relic and B.itemOdds or B.symbolOdds
 return {table.unpack(rows[math.min(paid + 1, #rows)])}
end

function B.Guarantee(paid, slot)
 if paid <= 0 then return 1 end
 if paid <= 4 then return 2 end
 if paid <= 8 then return slot == 1 and 3 or 1 end
 if paid == 9 then return slot <= 2 and 3 or 1 end
 return 3
end

return B
