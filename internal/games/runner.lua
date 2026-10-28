-- Hog Runner: jump the cacti, duck under the birds, in the spirit of Chrome's
-- offline dinosaur game. Middle (or top) jumps, bottom ducks while held and
-- drops you faster mid-air. The pace keeps rising.

local GROUND, HOG_X = 118, 30
local STAND_W, STAND_H, DUCK_W, DUCK_H = 18, 18, 22, 10
local BIRD_BOTTOM = GROUND - 12 -- a standing hog hits it, a ducking one passes under
local GRAVITY, JUMP = 1100, 360 -- px/s², px/s
local START_SPEED, MAX_SPEED, ACCEL = 130, 300, 4 -- px/s, px/s, px/s per second
local BIRD_COLOR = rgb(0x8E7CC3)

local state, lift, vlift, speed, distance, untilNext, top, overAt
local obstacles = {}

local function score() return math.floor(distance / 10) end
local function ducking() return held(BOTTOM) and lift <= 0 end

local function reset()
  state, lift, vlift = "ready", 0, 0
  speed, distance, untilNext = START_SPEED, 0, 120
  obstacles = {}
end

function start()
  top = best()
  reset()
end

local function spawn()
  if #obstacles >= 4 then return end
  local bird = speed > 170 and math.random(4) == 1
  if bird then
    obstacles[#obstacles + 1] = { x = W + 4, w = 16, h = 8, bird = true }
  else
    obstacles[#obstacles + 1] = { x = W + 4, w = 8 + math.random(0, 2) * 4, h = 16 + math.random(0, 11), bird = false }
  end
end

local function jump()
  if lift <= 0 then vlift = JUMP end
end

function update(dt)
  if state ~= "running" then return end
  speed = math.min(MAX_SPEED, speed + ACCEL * dt)
  distance = distance + speed * dt

  vlift = vlift - GRAVITY * dt
  lift = lift + vlift * dt
  if lift <= 0 then lift, vlift = 0, 0 end

  untilNext = untilNext - speed * dt
  if untilNext <= 0 then
    spawn()
    untilNext = 150 + math.random(0, 169) + speed * 0.4 -- gaps grow with the pace, so they stay jumpable
  end

  local hw, hh = STAND_W, STAND_H
  if ducking() then hw, hh = DUCK_W, DUCK_H end
  local left, right = HOG_X + 2, HOG_X + hw - 2
  local bottom, topEdge = GROUND - lift - 1, GROUND - lift - hh + 2
  for i = #obstacles, 1, -1 do
    local o = obstacles[i]
    o.x = o.x - speed * dt
    if o.x + o.w < 0 then
      table.remove(obstacles, i)
    else
      local oBottom = o.bird and BIRD_BOTTOM or GROUND
      if right > o.x and left < o.x + o.w and bottom > oBottom - o.h and topEdge < oBottom then
        state, overAt = "over", millis()
        top = best(score())
        return
      end
    end
  end
end

local function drawCactus(o)
  local x, y = o.x, GROUND - o.h
  rrect(x, y, o.w, o.h + 1, 2, GREEN)
  if o.w >= 12 then -- arms
    rect(x - 3, y + 6, 3, 2, GREEN)
    rect(x - 3, y + 3, 2, 4, GREEN)
    rect(x + o.w, y + 9, 3, 2, GREEN)
    rect(x + o.w + 1, y + 5, 2, 5, GREEN)
  end
end

local function drawBird(o)
  local x, y = o.x, BIRD_BOTTOM - o.h
  rect(x + 2, y + 3, 12, 4, BIRD_COLOR)
  tri(x, y + 4, x + 3, y + 2, x + 3, y + 6, BIRD_COLOR)
  if (millis() // 150) % 2 == 0 then
    tri(x + 5, y + 3, x + 11, y + 3, x + 9, y - 3, BIRD_COLOR)
  else
    tri(x + 5, y + 6, x + 11, y + 6, x + 9, y + 11, BIRD_COLOR)
  end
end

local function drawRunner()
  local bottom = GROUND - math.floor(lift)
  local step = math.floor(distance / 14) % 2 == 1
  if ducking() then
    local y = bottom - DUCK_H
    rrect(HOG_X, y, DUCK_W, DUCK_H - 2, 4, HOG)
    for i = 0, 2 do tri(HOG_X + 2 + i * 5, y, HOG_X + 6 + i * 5, y, HOG_X + 2 + i * 5, y - 3, BLACK) end
    circ(HOG_X + DUCK_W - 4, y + 3, 1, BLACK)
    rect(HOG_X + 4 + (step and 0 or 2), bottom - 2, 3, 2, WHITE)
    rect(HOG_X + 14 + (step and 2 or 0), bottom - 2, 3, 2, WHITE)
    return
  end
  local y = bottom - STAND_H
  rrect(HOG_X, y + 2, STAND_W, STAND_H - 6, 6, HOG)
  for i = 0, 2 do tri(HOG_X + 1 + i * 4, y + 4, HOG_X + 5 + i * 4, y + 2, HOG_X + i * 4, y - 1, BLACK) end
  circ(HOG_X + STAND_W - 5, y + 7, 2, WHITE)
  circ(HOG_X + STAND_W - 4, y + 7, 1, BLACK)
  local legs = lift > 0 or step
  rect(HOG_X + 4, bottom - 4, 3, 4, legs and WHITE or GRAY)
  rect(HOG_X + 11, bottom - 4, 3, 4, legs and GRAY or WHITE)
end

function draw()
  cls(BLACK)
  rect(0, GROUND, W, 1, GRAY)
  for i = 0, 11 do -- pebbles scroll by with the ground
    local x = (i * 41 - math.floor(distance)) % W
    rect(x, GROUND + 5 + (i % 3) * 4, 2 + i % 3, 1, DARK)
  end
  for _, o in ipairs(obstacles) do
    if o.bird then drawBird(o) else drawCactus(o) end
  end
  drawRunner()
  text(W - 62, 18, string.format("%05d", score()), BOLD, WHITE)
  text(W - 136, 18, string.format("HI %05d", top), BOLD, GRAY)
  if state == "ready" then
    card("Hog Runner", "Middle: jump", "Bottom: duck", "Hold middle to leave")
  elseif state == "over" then
    card("Game over", "Score " .. score() .. "   Best " .. top, "Middle: run again", "Hold middle to leave")
  end
end

function button(b, down)
  if not down then return end
  if state == "ready" then
    state = "running"
    if b ~= BOTTOM then jump() end
  elseif state == "running" then
    if b == BOTTOM and lift > 0 then vlift = math.min(vlift, -250) end -- duck mid-air: drop faster
    if b ~= BOTTOM then jump() end
  elseif b == MIDDLE and millis() - overAt > 600 then
    reset()
  end
end
