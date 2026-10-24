-- Simon: the board lights a pattern on three pads, one per button row; repeat
-- it with the buttons. Each round adds one step and plays a little faster.

local TOP_BAR = 15
local PAD_H = (H - TOP_BAR) // 3
local BRIGHT = { [0] = RED, GREEN, BLUE }
local DIM = { [0] = rgb(0x4A1714), rgb(0x0F3A24), rgb(0x10284F) }

local state, seq, shown, typed, timer, lit, top, overAt
local pressed, pressedAt = nil, 0

local function onTime() return math.max(0.22, 0.48 - #seq * 0.015) end

local function addStep()
  seq[#seq + 1] = math.random(0, 2)
  state, shown, lit, timer = "showing", 1, false, 0.5 -- a short pause before the first step
end

local function reset()
  state, seq, pressed = "ready", {}, nil
end

function start()
  top = best()
  reset()
end

function update(dt)
  if state == "correct" then
    timer = timer - dt
    if timer <= 0 then addStep() end
    return
  end
  if state ~= "showing" then return end
  timer = timer - dt
  if timer > 0 then return end
  if lit then -- step done: a gap, then the next one
    lit, shown, timer = false, shown + 1, 0.16
  elseif shown <= #seq then
    lit, timer = true, onTime()
  else
    state, typed = "input", 0
  end
end

function draw()
  cls(BLACK)
  text(6, 12, "Round " .. math.max(#seq, 1), SMALL, WHITE)
  ctext(12, "Best " .. top, SMALL, GRAY, 70)
  ctext(12, state == "showing" and "Watch" or state == "input" and "Your turn" or "", BOLD, HOG)
  local fresh = pressed ~= nil and millis() - pressedAt < 220
  for p = 0, 2 do
    local on = (state == "showing" and lit and seq[shown] == p) or (fresh and pressed == p)
    rrect(6, TOP_BAR + p * PAD_H + 3, W - 12, PAD_H - 6, 8, on and BRIGHT[p] or DIM[p])
  end
  if state == "ready" then
    card("Simon", "Watch, then repeat", "Middle: start", "Hold middle to leave")
  elseif state == "over" then
    card("Wrong pad", "Rounds " .. (#seq - 1) .. "   Best " .. top, "Middle: play again")
  end
end

function button(b, down)
  if not down then return end
  if state == "ready" then
    if b == MIDDLE then addStep() end
    return
  end
  if state == "over" then
    if b == MIDDLE and millis() - overAt > 600 then reset() end
    return
  end
  if state ~= "input" then return end
  pressed, pressedAt = b, millis()
  if b ~= seq[typed + 1] then
    state, overAt = "over", millis()
    top = best(#seq - 1)
    return
  end
  typed = typed + 1
  if typed == #seq then
    top = best(#seq)
    state, timer = "correct", 0.7
  end
end
