-- Stacker: a row of blocks slides back and forth; press any button to drop it
-- on the stack. Whatever hangs over the edge falls off, so the row narrows.
-- It speeds up as the tower grows. Reach the top to win.

local CELL_W, CELL_H, ROWS, START_W = 16, 9, 13, 5
local COLS, TOP_BAR = W // CELL_W, H - ROWS * CELL_H

local state, rows, pos, dir, speed, width, won, falling, top, overAt

local function moverLeft() return math.floor(pos + 0.5) end
local function rowY(level) return TOP_BAR + (ROWS - 1 - level) * CELL_H end

local function newRow()
  dir = math.random(2) == 1 and 1 or -1
  pos = dir > 0 and 0 or COLS - width
  speed = 6 + #rows * 0.9 -- cells per second
end

local function reset()
  state, rows, width, won, falling = "ready", {}, START_W, false, nil
  newRow()
end

function start()
  top = best()
  reset()
end

local function finish(didWin)
  state, won, overAt = "over", didWin, millis()
  top = best(#rows)
end

local function drop()
  local l, r = moverLeft(), moverLeft() + width
  local below = rows[#rows]
  if below then
    local nl, nr = math.max(l, below.left), math.min(r, below.left + below.width)
    if nr <= nl then return finish(false) end
    if nr - nl < width then -- show the overhang tumbling down
      falling = { y = rowY(#rows), vy = 0, x = l < below.left and l or nr, w = width - (nr - nl) }
    end
    l, width = nl, nr - nl
  end
  rows[#rows + 1] = { left = l, width = width }
  if #rows == ROWS then return finish(true) end
  if #rows >= 8 then -- like the arcade game, the top rows are narrower
    width = math.min(width, 2)
  elseif #rows >= 4 then
    width = math.min(width, 3)
  end
  newRow()
end

function update(dt)
  if falling then
    falling.vy = falling.vy + 400 * dt
    falling.y = falling.y + falling.vy * dt
    if falling.y > H then falling = nil end
  end
  if state ~= "playing" then return end
  pos = pos + dir * speed * dt
  if pos <= 0 then
    pos, dir = 0, 1
  elseif pos >= COLS - width then
    pos, dir = COLS - width, -1
  end
end

local function rowColor(level) -- orange at the base turning yellow at the top
  local g = 0x4E + (level - 1) * (0xC0 - 0x4E) // (ROWS - 1)
  return rgb(0xF50000 + g * 256)
end

local function blocks(left, count, y, color)
  for c = 0, count - 1 do rect((left + c) * CELL_W + 1, y + 1, CELL_W - 2, CELL_H - 2, color) end
end

function draw()
  cls(BLACK)
  text(6, 12, "Rows " .. #rows, SMALL, WHITE)
  ctext(12, "Best " .. top, SMALL, GRAY, 70)
  rect(0, TOP_BAR - 1, W, 1, DARK)
  for i, row in ipairs(rows) do blocks(row.left, row.width, rowY(i - 1), rowColor(i)) end
  if state == "playing" then blocks(moverLeft(), width, rowY(#rows), WHITE) end
  if falling then blocks(falling.x, falling.w, falling.y, GRAY) end
  if state == "ready" then
    card("Stacker", "Press to drop the row", "Hold middle to leave")
  elseif state == "over" then
    card(won and "Top reached!" or "Missed", "Rows " .. #rows .. "   Best " .. top, "Middle: play again")
  end
end

function button(b, down)
  if not down then return end
  if state == "ready" then
    state = "playing"
  elseif state == "playing" then
    drop()
  elseif b == MIDDLE and millis() - overAt > 600 then
    reset()
  end
end
