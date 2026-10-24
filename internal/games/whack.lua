-- Whack-a-Hog: hogs pop out of burrows in three rows, one row per button. Hit
-- the button for the hog's row before it ducks back down. Wrong button costs a
-- point. 30 seconds a round, and the hogs get quicker as you score.

local TOP_BAR, ROUND = 15, 30
local LANE_H = (H - TOP_BAR) // 3
local HOLES_X = { 50, 120, 190 }
local GRASS, LINE, DIRT, MISS = rgb(0x1D3B1F), rgb(0x2C5530), rgb(0x3B2414), rgb(0x5A1A14)

local state, timeLeft, nextIn, upLeft, upFor, lane, hole, score, top, overAt
local hitLane, hitAt, missLane, missAt = nil, 0, nil, 0

local function laneY(l) return TOP_BAR + l * LANE_H end

local function reset()
  state, timeLeft, score, nextIn = "ready", ROUND, 0, 0.6
  lane, hitLane, missLane = nil, nil, nil
end

function start()
  top = best()
  reset()
end

function update(dt)
  if state ~= "playing" then return end
  timeLeft = timeLeft - dt
  if timeLeft <= 0 then
    timeLeft, state, overAt = 0, "over", millis()
    top = best(score)
    return
  end
  if lane then
    upLeft = upLeft - dt
    if upLeft <= 0 then -- got away
      lane, nextIn = nil, 0.2 + math.random() * 0.4
    end
  else
    nextIn = nextIn - dt
    if nextIn <= 0 then
      lane, hole = math.random(0, 2), math.random(1, 3)
      upFor = math.max(0.45, 1.1 - score * 0.025)
      upLeft = upFor
    end
  end
end

function draw()
  cls(GRASS)
  rect(0, 0, W, TOP_BAR, BLACK)
  text(6, 12, "Time " .. math.ceil(timeLeft), SMALL, WHITE)
  ctext(12, "Score " .. score, SMALL, WHITE, 64)
  local t = millis()
  for l = 0, 2 do
    local y = laneY(l)
    if l == missLane and t - missAt < 200 then rect(0, y, W, LANE_H, MISS) end
    if l > 0 then rect(0, y, W, 1, LINE) end
    for h, cx in ipairs(HOLES_X) do
      local cy = y + LANE_H - 8
      if l == lane and h == hole then
        local shown = math.min(1, (upFor - upLeft) / 0.08) -- pops up quickly
        hog(cx, cy - math.floor(12 * shown), 9)
      end
      rrect(cx - 15, cy, 30, 7, 3, DIRT)
      if l == hitLane and h == hole and t - hitAt < 250 then ctext(y + 14, "+1", BOLD, WHITE, cx - W // 2) end
    end
  end
  if state == "ready" then
    card("Whack-a-Hog", "Press the hog's row", "Middle: start", "Hold middle to leave")
  elseif state == "over" then
    card("Time's up", "Score " .. score .. "   Best " .. top, "Middle: play again")
  end
end

function button(b, down)
  if not down then return end
  if state == "ready" then
    if b == MIDDLE then state = "playing" end
    return
  end
  if state == "over" then
    if b == MIDDLE and millis() - overAt > 600 then reset() end
    return
  end
  if b == lane then
    score, hitLane, hitAt = score + 1, lane, millis()
    lane, nextIn = nil, 0.15 + math.random() * 0.3
  else
    score, missLane, missAt = math.max(0, score - 1), b, millis()
  end
end
