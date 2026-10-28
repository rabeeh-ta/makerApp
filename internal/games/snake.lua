-- Snake with two-button steering: top turns left, bottom turns right (from the
-- snake's point of view), middle pauses. Eat to grow; walls and your own tail
-- end the game. It speeds up as it grows.

local CELL, ROWS = 8, 15
local COLS, FIELD_TOP = W // CELL, H - ROWS * CELL -- a score bar above the field
local DX, DY = { 1, 0, -1, 0 }, { 0, 1, 0, -1 } -- right, down, left, up
local BODY = rgb(0xFF8A3D)

local state, body, dir, eaten, turns, food, sinceStep, top, overAt

local function stepSeconds() return math.max(0.07, 0.15 - eaten * 0.004) end

local function occupied(x, y)
  for _, c in ipairs(body) do
    if c.x == x and c.y == y then return true end
  end
  return false
end

local function placeFood()
  repeat
    food = { x = math.random(0, COLS - 1), y = math.random(0, ROWS - 1) }
  until not occupied(food.x, food.y)
end

local function reset()
  state, dir, eaten, turns, sinceStep = "ready", 1, 0, {}, 0
  body = {}
  for i = 0, 3 do body[#body + 1] = { x = 8 - i, y = ROWS // 2 } end
  placeFood()
end

function start()
  top = best()
  reset()
end

local function step()
  if #turns > 0 then dir = (dir - 1 + table.remove(turns, 1)) % 4 + 1 end
  local nx, ny = body[1].x + DX[dir], body[1].y + DY[dir]
  local grow = nx == food.x and ny == food.y
  -- The tail moves out of the way this step unless the snake is growing.
  local hit = nx < 0 or ny < 0 or nx >= COLS or ny >= ROWS
  for i = 1, #body - (grow and 0 or 1) do
    if body[i].x == nx and body[i].y == ny then hit = true end
  end
  if hit then
    state, overAt = "over", millis()
    top = best(eaten)
    return
  end
  table.insert(body, 1, { x = nx, y = ny })
  if grow then
    eaten = eaten + 1
    placeFood()
  else
    table.remove(body)
  end
end

function update(dt)
  if state ~= "playing" then return end
  sinceStep = sinceStep + dt
  while state == "playing" and sinceStep >= stepSeconds() do
    sinceStep = sinceStep - stepSeconds()
    step()
  end
end

function draw()
  cls(BLACK)
  text(6, 12, "Score " .. eaten, SMALL, WHITE)
  ctext(12, "Best " .. top, SMALL, GRAY, 70)
  rect(0, FIELD_TOP - 1, W, 1, DARK)
  circ(food.x * CELL + CELL // 2, FIELD_TOP + food.y * CELL + CELL // 2, 3, RED)
  for i = #body, 1, -1 do
    local c = body[i]
    rrect(c.x * CELL + 1, FIELD_TOP + c.y * CELL + 1, CELL - 2, CELL - 2, 2, i == 1 and HOG or BODY)
  end
  if state == "ready" then
    card("Snake", "Top: turn left", "Bottom: turn right", "Middle: start or pause")
  elseif state == "paused" then
    card("Paused", "Middle: resume", "Hold middle to leave")
  elseif state == "over" then
    card("Game over", "Score " .. eaten .. "   Best " .. top, "Middle: play again", "Hold middle to leave")
  end
end

function button(b, down)
  if not down then return end
  if b == MIDDLE then
    if state == "ready" or state == "paused" then
      state = "playing"
    elseif state == "playing" then
      state = "paused"
    elseif millis() - overAt > 600 then
      reset()
    end
    return
  end
  -- Presses between steps queue up (two at most), one turn per step.
  if state == "playing" and #turns < 2 then turns[#turns + 1] = b == TOP and -1 or 1 end
end
