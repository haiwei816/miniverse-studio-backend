-- Full-board building followed by one upfront heat/additive decision.
local B={
 id="cauldron-heat-brew-v3",
 floor=1,
 checkedAt="2026-10-30",
 -- Initial tuning for whole-board payouts; normal play can finish without brewing.
 rents={26, 140, 270, 350, 520, 550, 770, 910, 1170, 1350, 1800, 1950,
        2300, 2700, 3200, 3750, 4450, 5250, 6200, 7300},
 spins={5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10},
 -- Draw odds: Common / Uncommon / Rare / Legendary based on completed rent payments
 symbolOdds={
  {100, 0, 0, 0},
  {88, 12, 0, 0},
  {76, 22, 2, 0},
  {70, 26, 3.5, 0.5},
  {64, 30, 5, 1},
  {60, 32, 6.5, 1.5}
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
 winRound=12,
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
  temperature={baseBoost=10,baseRisk=2,boostPerLevel=18,riskPerLevel=4,discoveryPerLevel=5,discoveryMax=25},
  additiveOrder={"frost_dew","dragon_dust","safety_seal"},
  -- Unlock as relics; each owned copy permits one upfront use per rent cycle.
  additives={
   frost_dew={name="寒霜露",boost=-5,risk=-6,refund=0},
   dragon_dust={name="龙息粉",boost=35,risk=8,refund=0},
   safety_seal={name="保温符",boost=0,risk=3,refund=50}
  },
  materials={
   toadstool={risk=1,boost=2},unstable={risk=2,boost=4},
   puffer={risk=4,boost=10},antidote={risk=-3,boost=0},rose={risk=0,boost=2}
  }
 },
 -- Symbol levels + family investment: the player's build-up axis.
 upgrade={
  levels={1.0,1.25,1.6,2.1,2.8},
  maxLevel=12,
  tokenEvery=3,
  familyRate=0.08,
  familyCap=0.6,
  choiceCount=3,
  auto=true
 },
 rarityValueBonus={0,1,3,6},
 startCoins=1,
 emailFirstPayment=3,
 emailEvery=2,
 emailRemoval=2,
 emailReroll=2,
 rules={
  -- Witch & Familiar synergies (redesigned ratios)
  milk=7,           -- Black cat drinks ghost milk (+7 instead of 9)
  owlEvery=2,       -- Owl triggers every 2 rounds (+2 instead of every 3)
  owlBonus=2,
  crowEvery=3,      -- Crow triggers every 3 rounds (-2 instead of every 4)
  crowPenalty=2,
  witch=2,          -- Witch buffs familiars x2
  -- Herbal & Fruit synergies
  bee=2,
  rain=2,
  sun=4,            -- Blood moon multiplier (4x instead of 5x)
  seedChance=0.30,  -- Seed growth rate (30% instead of 25%)
  seedBoost=0.40,
  growth=1,
  -- Undead & Graveyard synergies
  spiritLife=3,     -- Wandering spirit lives for 3 cycles (higher burst)
  hooligan=8,       -- Gravedigger smashes urn for +8 (instead of 6)
  zaroff=18,        -- Lich / Necromancer harvests for +18 (instead of 25)
  -- Mineral & Alchemy synergies
  miner=15,         -- Fire salamander consumes ore for +15 (instead of 20)
  magicKey=2.5,     -- Magic key multiplier 2.5x (instead of 3x)
  -- Gambling & Halloween party synergies
  gamblerPerSpin=3, -- Gambler pot increases by +3 (instead of 2)
  thiefPerSpin=3,   -- Phantom thief payout 3x (instead of 4x)
  monkey=5,         -- Troll / Monkey feast 5x (instead of 6x)
  joker=2,
  dame=2,
  martini=35,       -- Dame drinks martini for +35 (instead of 40)
  midas=2.5,        -- Pumpkin Midas buffs candy 2.5x (instead of 3x)
  -- Relic & Witch Artifact powers
  luckyCat=1.25,    -- Black cat paw high-rarity boost x1.25 (instead of 1.3)
  shinyPebble=1.15, -- Shiny pebble boost x1.15 (instead of 1.1)
  hexTedium=1.25,   -- Totem penalty divisor /1.25 (instead of 1.3)
  goldenCarrot=3.0, -- Magnetic star boost 3x (instead of 5x, preventing inflation)
  miningPick=8,     -- Alchemical pickaxe +8 (instead of 10)
  pizza=1,
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
