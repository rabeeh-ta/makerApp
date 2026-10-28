-- Breakout on its side: your paddle is on the left, the bricks on the right.
-- Top and bottom move the paddle, middle launches the ball (and pauses).
-- Where the ball meets the paddle sets its angle. Three lives; clearing the
-- wall brings a faster one.

local PADDLE_X, PW, PH, BALL = 6, 5, 28, 4
local COLS, ROWS, BRICK_W, BRICK_H, GAP, BRICKS_X, BRICKS_Y = 5, 7, 10, 17, 2, 168, 2
local PADDLE_SPEED, START_SPEED, PER_BRICK, PER_LEVEL = 170, 115, 1.5, 12 -- px/s
local ROW_COLORS = {}
for i, hex in ipairs({ 0xF04438, 0xF79009, 0xFAC515, 0x2BB673, 0x22CCEE, 0x2D7FF9, 0x8E7CC3 }) do
  ROW_COLORS[i] = rgb(hex)
end

local state, bricks, left, lives, level, score, py, bx, by, vx, vy, speed, top, overAt

local function buildWall()
  bricks, left = {}, COLS * ROWS
  for c = 1, COLS do
    bricks[c] = {}
    for r = 1, ROWS do bricks[c][r] = true end
  end
end

local function newGame()
  lives, level, score, speed = 3, 1, 0, START_SPEED
  py = H // 2 - PH // 2
  buildWall()
  state = "aim"
end

function start()
  top = best()
  newGame()
  state = "title"
end

local function launch()
  local a = math.rad(math.random(-35, 35))
  vx, vy, state = speed * math.cos(a), speed * math.sin(a), "playing"
end

local function brickX(c) return BRICKS_X + (c - 1) * (BRICK_W + GAP) end
local function brickY(r) return BRICKS_Y + (r - 1) * (BRICK_H + GAP) end

-- Breaks the first brick the ball overlaps, if any.
local function hitBrick()
  for c = 1, COLS do
    for r = 1, ROWS do
      if bricks[c][r] then
        local x, y = brickX(c), brickY(r)
        if bx + BALL > x and bx < x + BRICK_W and by + BALL > y and by < y + BRICK_H then
          bricks[c][r], left = false, left - 1
          score = score + 10 + (c - 1) * 5 -- deeper bricks are worth more
          speed = speed + PER_BRICK
          return true
        end
      end
    end
  end
  return false
end

local function clamp(v, lo, hi) return math.max(lo, math.min(hi, v)) end

function update(dt)
  if state == "title" or state == "paused" or state == "over" then return end
  local dir = (held(BOTTOM) and 1 or 0) - (held(TOP) and 1 or 0)
  py = clamp(py + dir * PADDLE_SPEED * dt, 0, H - PH)
  if state == "aim" then -- the ball rides on the paddle until launched
    bx, by = PADDLE_X + PW + 1, py + PH / 2 - BALL / 2
    return
  end

  -- Move one axis at a time, so a brick hit knows which way to bounce.
  bx = bx + vx * dt
  if hitBrick() then bx, vx = bx - vx * dt, -vx end
  by = by + vy * dt
  if hitBrick() then by, vy = by - vy * dt, -vy end
  if by <= 0 then
    by, vy = 0, math.abs(vy)
  elseif by >= H - BALL then
    by, vy = H - BALL, -math.abs(vy)
  end
  if bx >= W - BALL then bx, vx = W - BALL, -math.abs(vx) end
  if vx < 0 and bx <= PADDLE_X + PW and bx + BALL >= PADDLE_X and by + BALL > py and by < py + PH then
    local off = ((by + BALL / 2) - (py + PH / 2)) / (PH / 2) -- -1 top edge .. 1 bottom edge
    local a = math.rad(60) * clamp(off, -1, 1)
    vx, vy, bx = speed * math.cos(a), speed * math.sin(a), PADDLE_X + PW + 1
  end
  if bx < 0 then
    lives = lives - 1
    if lives <= 0 then
      state, overAt = "over", millis()
      top = best(score)
    else
      state = "aim"
    end
    return
  end
  if left == 0 then
    level = level + 1
    speed = START_SPEED + (level - 1) * PER_LEVEL
    buildWall()
    state = "aim"
  end
end

function draw()
  cls(BLACK)
  for c = 1, COLS do
    for r = 1, ROWS do
      if bricks[c][r] then rect(brickX(c), brickY(r), BRICK_W, BRICK_H, ROW_COLORS[r]) end
    end
  end
  rect(PADDLE_X, py, PW, PH, WHITE)
  if state ~= "title" and state ~= "over" then rect(bx, by, BALL, BALL, WHITE) end
  text(40, 14, score, SMALL, GRAY)
  for i = 0, lives - 1 do circ(110 + i * 8, 9, 2, HOG) end
  if state == "aim" then text(40, 76, "Middle: launch", SMALL, GRAY) end
  if state == "title" then
    card("Breakout", "Top / bottom: move", "Middle: launch", "Hold middle to leave")
  elseif state == "paused" then
    card("Paused", "Middle: resume", "Hold middle to leave")
  elseif state == "over" then
    card("Game over", "Score " .. score .. "   Best " .. top, "Middle: play again", "Hold middle to leave")
  end
end

function button(b, down)
  if not down or b ~= MIDDLE then return end
  if state == "title" then
    state = "aim"
  elseif state == "aim" then
    launch()
  elseif state == "playing" then
    state = "paused"
  elseif state == "paused" then
    state = "playing"
  elseif millis() - overAt > 600 then
    newGame()
  end
end
