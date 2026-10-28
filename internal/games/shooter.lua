-- Hog Invaders: Space Invaders on its side. Your ship sits on the left; top
-- and bottom move it, middle fires. The invaders march up and down, stepping
-- closer each time they reach an edge, and shoot back. Three lives.

local TOP_BAR, SHIP_X, SHIP_W, SHIP_H = 14, 6, 12, 10
local COLS, ROWS, INV_W, INV_H = 4, 5, 10, 8
local STEP_X, STEP_Y = INV_W + 8, INV_H + 8
local MAX_SHOTS, MAX_BOMBS = 2, 3
local SHIP_SPEED, SHOT_SPEED, BOMB_SPEED = 120, 260, 110 -- px/s
local ROW_COLORS = {}
for i, hex in ipairs({ 0x8E7CC3, 0x22CCEE, 0x2BB673, 0xFAC515, 0xF79009 }) do ROW_COLORS[i] = rgb(hex) end

local state, shipY, fx, fy, fdir, marchIn, bombIn, alive, lives, wave, score, legs
local invaders, shots, bombs, stars = {}, {}, {}, {}
local lastShot, hurtAt, top, overAt = 0, -10000, 0, 0

local function marchSeconds()
  return math.max(0.08, (0.12 + 0.5 * alive / (COLS * ROWS)) * 0.85 ^ (wave - 1))
end

local function newWave()
  for c = 1, COLS do
    invaders[c] = {}
    for r = 1, ROWS do invaders[c][r] = true end
  end
  alive = COLS * ROWS
  fx, fy, fdir = W - COLS * STEP_X - 4, TOP_BAR + 2, 1
  marchIn, bombs = marchSeconds(), {}
end

local function reset()
  state, shipY, lives, wave, score = "ready", H // 2 - SHIP_H // 2, 3, 1, 0
  hurtAt, shots = -10000, {}
  newWave()
end

function start()
  for i = 1, 24 do stars[i] = { x = math.random(0, W - 1), y = math.random(TOP_BAR, H - 1) } end
  top = best()
  reset()
end

local function gameOver()
  state, overAt = "over", millis()
  top = best(score)
end

local function invX(c) return fx + (c - 1) * STEP_X end
local function invY(r) return fy + (r - 1) * STEP_Y end

local function march()
  -- The edges of the formation, counting only invaders still alive.
  local minC, minR, maxR = COLS + 1, ROWS + 1, 0
  for c = 1, COLS do
    for r = 1, ROWS do
      if invaders[c][r] then
        minC, minR, maxR = math.min(minC, c), math.min(minR, r), math.max(maxR, r)
      end
    end
  end
  if (fdir > 0 and invY(maxR) + INV_H + 4 > H) or (fdir < 0 and invY(minR) - 4 < TOP_BAR) then
    fdir, fx = -fdir, fx - 6 -- reached an edge: step towards the ship
  else
    fy = fy + fdir * 4
  end
  legs = not legs
  if invX(minC) <= SHIP_X + SHIP_W + 2 then gameOver() end -- they've reached you
end

local function dropBomb()
  if #bombs >= MAX_BOMBS then return end
  -- The front-most invader of a random row that still has one.
  for _ = 1, 8 do
    local r = math.random(ROWS)
    for c = 1, COLS do
      if invaders[c][r] then
        bombs[#bombs + 1] = { x = invX(c) - 3, y = invY(r) + INV_H // 2 - 1 }
        return
      end
    end
  end
end

function update(dt)
  if state ~= "playing" then return end
  local dir = (held(BOTTOM) and 1 or 0) - (held(TOP) and 1 or 0)
  shipY = math.max(TOP_BAR, math.min(H - SHIP_H, shipY + dir * SHIP_SPEED * dt))

  marchIn = marchIn - dt
  if marchIn <= 0 then
    march()
    marchIn = marchSeconds()
    if state ~= "playing" then return end
  end
  bombIn = bombIn - dt
  if bombIn <= 0 then
    dropBomb()
    bombIn = 0.7 + math.random() * 0.8
  end

  for i = #shots, 1, -1 do
    local s = shots[i]
    s.x = s.x + SHOT_SPEED * dt
    local gone = s.x > W
    for c = 1, COLS do
      for r = 1, ROWS do
        if not gone and invaders[c][r] then
          local ix, iy = invX(c), invY(r)
          if s.x + 4 > ix and s.x < ix + INV_W and s.y + 2 > iy and s.y < iy + INV_H then
            invaders[c][r], gone = false, true
            alive = alive - 1
            score = score + 10 * (ROWS + 1 - r) -- rows nearer the top are worth more
          end
        end
      end
    end
    if gone then table.remove(shots, i) end
  end
  if alive == 0 then
    wave = wave + 1
    newWave()
  end

  local shielded = millis() - hurtAt < 1000 -- a moment to recover after a hit
  for i = #bombs, 1, -1 do
    local b = bombs[i]
    b.x = b.x - (BOMB_SPEED + wave * 10) * dt
    if b.x < -4 then
      table.remove(bombs, i)
    elseif not shielded and b.x < SHIP_X + SHIP_W and b.x + 3 > SHIP_X and b.y + 2 > shipY and b.y < shipY + SHIP_H then
      table.remove(bombs, i)
      hurtAt, lives = millis(), lives - 1
      if lives <= 0 then return gameOver() end
    end
  end
end

local function drawInvader(x, y, color)
  rect(x + 2, y, INV_W - 4, INV_H - 2, color)
  rect(x, y + 2, INV_W, 3, color)
  rect(x + 3, y + 2, 1, 1, BLACK) -- eyes
  rect(x + INV_W - 4, y + 2, 1, 1, BLACK)
  local spread = legs and 0 or 2
  rect(x + 1 + spread, y + INV_H - 2, 2, 2, color)
  rect(x + INV_W - 3 - spread, y + INV_H - 2, 2, 2, color)
end

function draw()
  cls(BLACK)
  for _, s in ipairs(stars) do pix(s.x, s.y, DARK) end
  text(6, 11, "Score " .. score, SMALL, GRAY)
  for i = 0, lives - 1 do tri(W - 34 + i * 10, 3, W - 34 + i * 10, 11, W - 27 + i * 10, 7, HOG) end
  for c = 1, COLS do
    for r = 1, ROWS do
      if invaders[c][r] then drawInvader(invX(c), invY(r), ROW_COLORS[r]) end
    end
  end
  local blink = millis() - hurtAt < 1000 and (millis() // 100) % 2 == 1
  if not blink then
    tri(SHIP_X, shipY, SHIP_X, shipY + SHIP_H, SHIP_X + SHIP_W, shipY + SHIP_H / 2, HOG)
    rect(SHIP_X - 2, shipY + 2, 2, SHIP_H - 4, WHITE)
  end
  for _, s in ipairs(shots) do rect(s.x, s.y, 4, 2, WHITE) end
  for _, b in ipairs(bombs) do rect(b.x, b.y, 3, 2, RED) end
  if state == "ready" then
    card("Hog Invaders", "Top / bottom: move", "Middle: fire", "Hold middle to leave")
  elseif state == "over" then
    card("Game over", "Score " .. score .. "   Best " .. top, "Middle: play again", "Hold middle to leave")
  end
end

function button(b, down)
  if not down or b ~= MIDDLE then return end
  if state == "ready" then
    state, bombIn = "playing", 1.5
  elseif state == "over" then
    if millis() - overAt > 600 then reset() end
  elseif millis() - lastShot >= 250 and #shots < MAX_SHOTS then
    shots[#shots + 1] = { x = SHIP_X + SHIP_W, y = shipY + SHIP_H // 2 - 1 }
    lastShot = millis()
  end
end
