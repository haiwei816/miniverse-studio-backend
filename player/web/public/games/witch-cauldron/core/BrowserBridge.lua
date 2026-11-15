-- Browser adapter only. Game/Data/AlchemyBalance remain byte-for-byte copies.
local Game = require "Game"
local Data = require "Data"
local B = Data.balance
local M = {}
local game

local function check(ok, message)
 if not ok then error("CAULDRON:" .. (message or "当前阶段无法执行此操作"), 0) end
 return ok
end
local function clone(value, visiting)
 if type(value) ~= "table" then
  if type(value) == "function" then return nil end
  return value
 end
 visiting = visiting or {}
 check(not visiting[value], "存档包含循环引用")
 visiting[value] = true
 local out = {}
 for k,v in pairs(value) do if type(v) ~= "function" then out[k] = clone(v, visiting) end end
 visiting[value] = nil
 return out
end
local function normalizeNumericMap(value)
 local out = {}
 for key,item in pairs(value or {}) do
  local n = tonumber(key)
  if n and n % 1 == 0 then out[n] = item else out[key] = item end
 end
 return out
end
local function requireGame() check(game ~= nil, "请先创建或读取游戏") end

function M.catalog()
 return clone({symbols=Data.symbols,order=Data.order,relics=Data.relics,
  relicOrder=Data.relicOrder,balance=B,rarity=Data.rarity})
end
function M.snapshot()
 requireGame()
 local out = clone(game)
 out.rent = game:Rent()
 out.estimate = game:Estimate()
 out.nextTemperature = game:NextTemperature()
 out.brewPreview = game:PreviewBrew(game.selectedAdditive, game.phase == "ready" and out.nextTemperature or game.temperature)
 out.bossPlan = game:BossPlan()
 out.additiveRemaining = {}
 for _,id in ipairs(B.brew.additiveOrder) do out.additiveRemaining[id] = game:AdditiveRemaining(id) end
 return out
end
function M.newGame(seed)
 game = Game.New(seed)
 return M.snapshot()
end
function M.load(value)
 -- JS validates a detached plain-data envelope before crossing the VM boundary.
 -- Build a candidate and finish restoration before replacing the live game.
 local candidate = clone(value.game)
 for _,name in ipairs({"rites","familyBoost","seenGroups","roundScores"}) do candidate[name] = normalizeNumericMap(candidate[name]) end
 local rng,phase,upgradeChoices = candidate.rng,candidate.phase,candidate.upgradeChoices
 candidate.events = {};candidate.values = {};candidate.incomeDetails = nil
 for _,instance in ipairs(candidate.bag) do instance.lastIncome = nil end
 setmetatable(candidate, Game)
 candidate:Restore()
 -- Current-format choices already exist. Restore supports legacy upgrades by
 -- drawing replacements; loading a browser save must not redraw or advance RNG.
 candidate.rng = rng
 if phase == "upgrade" then candidate.upgradeChoices = upgradeChoices;candidate.phase = phase end
 if candidate.phase == "review" then candidate:DetectMainline() end
 candidate.visualBoard = {}
 for i=1,B.boardSize do candidate.visualBoard[i] = candidate.board[i] end
 game = candidate
 return M.snapshot()
end
function M.exportSave()
 requireGame()
 local state = clone(game)
 -- Settlement totals are authoritative; these are presentation-only caches.
 state.events = {};state.values = {};state.incomeDetails = nil;state.visualBoard = {}
 for _,instance in ipairs(state.bag) do instance.lastIncome = nil end
 for _,instance in ipairs(state.board) do if instance then instance.lastIncome = nil end end
 return {version=1,profile=B.id,game=state}
end

local methods = {
 choose="Choose",skip="Skip",reroll="Reroll",pay="Pay",reward="ClaimRentReward",
 upgrade="PickUpgrade",skipUpgrade="SkipUpgrade",remove="Remove",continue="Continue",
 fuel="SelectFuel",finishExplosion="FinishExplosion",bottle="Bottle",
 spin="Spin",resolve="Resolve",openUpgrade="OpenUpgrade"
}
function M.dispatch(action, arg)
 requireGame()
 if action == "additive" then
  check(game.phase == "ready")
  check(arg == "" or (B.brew.additives[arg] and game:AdditiveRemaining(arg) > 0), "没有可用的该添加剂")
  game.selectedAdditive = arg
 elseif action == "brew" or action == "collect" then
  local additive = game.selectedAdditive or ""
  check(additive == "" or game:AdditiveRemaining(additive) > 0, "没有可用的该添加剂")
  check(game.phase == "ready" or game.phase == "arrange" or game.phase == "review")
  if game.phase == "ready" then check(game:Spin()) end
  if game.phase == "arrange" then check(game:Resolve()) end
  if action == "collect" or game.basePayout <= 0 then check(game:Bottle())
  else
   check(game:StartBrew(additive))
   if game.phase == "brewed" then check(game:Bottle())
   elseif arg and arg.finishExplosion then check(game:FinishExplosion()) end
  end
 else
  local method = methods[action]
  check(method ~= nil, "不支持此游戏操作")
  check(game[method](game, arg))
 end
 return M.snapshot()
end

return M
