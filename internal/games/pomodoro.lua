-- Pomodoro: 25 minutes of focus, then a 5 minute break; every fourth break is
-- 15 minutes. Middle starts and pauses, bottom skips to the next phase, top
-- restarts the current one. The timer is saved with the real time, so it keeps
-- running while you're in other apps and through a restart.

local PHASES = {
  focus = { name = "Focus", minutes = 25, color = HOG, dim = rgb(0x3A1A08) },
  short = { name = "Short break", minutes = 5, color = GREEN, dim = rgb(0x0F3A24) },
  long = { name = "Long break", minutes = 15, color = BLUE, dim = rgb(0x10284F) },
}
local ROUNDS = 4 -- focus sessions before a long break

-- Big seven-segment digits: segment rectangles relative to a digit's corner,
-- and which segments each number lights.
local DW, DH, T = 30, 52, 6
local HALF = (DH - 3 * T) // 2
local SEGMENTS = {
  a = { T, 0, DW - 2 * T, T },
  b = { DW - T, T, T, HALF },
  c = { DW - T, (DH + T) // 2, T, HALF },
  d = { T, DH - T, DW - 2 * T, T },
  e = { 0, (DH + T) // 2, T, HALF },
  f = { 0, T, T, HALF },
  g = { T, (DH - T) // 2, DW - 2 * T, T },
}
local LIT = { [0] = "abcdef", "bc", "abdeg", "abcdg", "bcfg", "acdfg", "acdefg", "abc", "abcdefg", "abcdfg" }

local phase   -- "focus", "short" or "long"
local done    -- focus sessions finished in this round
local endsAt  -- while running: when the phase ends, in seconds; nil while paused
local left    -- while paused: seconds left

-- The real time in seconds; uptime until the board has synced its clock.
local function clock()
  local t = now()
  if t == 0 then return millis() // 1000 end
  return t
end

local function persist()
  save("phase", phase)
  save("done", done)
  save("ends", endsAt)
  save("left", left)
end

local function remaining()
  if endsAt then return math.max(0, endsAt - clock()) end
  return left
end

local function finished() return endsAt ~= nil and remaining() == 0 end

local function setPhase(p, running)
  phase, left, endsAt = p, PHASES[p].minutes * 60, nil
  if running then endsAt = clock() + left end
  persist()
end

local function nextPhase()
  if phase == "focus" then
    done = done + 1
    return done >= ROUNDS and "long" or "short"
  end
  if phase == "long" then done = 0 end
  return "focus"
end

function start()
  phase = saved("phase")
  if not PHASES[phase] then phase = "focus" end
  done = saved("done") or 0
  endsAt = saved("ends")
  left = saved("left") or PHASES[phase].minutes * 60
end

function update(dt) end -- the clock does the counting

local function digit(x, y, n, color)
  local lit = LIT[n]
  for name, r in pairs(SEGMENTS) do
    rrect(x + r[1], y + r[2], r[3], r[4], 2, lit:find(name, 1, true) and color or DARK)
  end
end

function draw()
  local p = PHASES[phase]
  local over = finished()
  local blink = (millis() // 500) % 2 == 0
  cls(over and blink and p.dim or BLACK)

  text(8, 19, over and p.name .. " done" or p.name, BOLD, p.color)
  for i = 1, ROUNDS do
    local x = W - 10 - (ROUNDS - i) * 12
    if i <= done then circ(x, 13, 4, HOG) else circb(x, 13, 4, GRAY) end
  end

  local secs = remaining()
  local mm, ss = secs // 60, secs % 60
  local x, y = 45, 34
  digit(x, y, mm // 10, p.color)
  digit(x + 36, y, mm % 10, p.color)
  if not endsAt or over or blink then -- the colon ticks while running
    rrect(x + 72, y + 14, 6, 6, 1, p.color)
    rrect(x + 72, y + 32, 6, 6, 1, p.color)
  end
  digit(x + 84, y, ss // 10, p.color)
  digit(x + 120, y, ss % 10, p.color)

  local total = p.minutes * 60
  rrect(16, 98, W - 32, 6, 3, DARK)
  local fill = math.floor((W - 32) * (total - secs) / total)
  if fill > 0 then rrect(16, 98, math.max(fill, 6), 6, 3, p.color) end

  local hint
  if over then
    hint = phase == "focus" and "Middle: start the break" or "Middle: back to focus"
  elseif endsAt then
    hint = "Middle: pause   Bottom: skip"
  elseif left == total then
    hint = "Middle: start   Bottom: skip"
  else
    hint = "Middle: resume   Top: reset"
  end
  ctext(124, hint, SMALL, GRAY)
end

function button(b, down)
  if not down then return end
  if b == MIDDLE then
    if finished() then
      setPhase(nextPhase(), true)
    elseif endsAt then -- pause
      left, endsAt = remaining(), nil
      persist()
    else -- start or resume
      endsAt = clock() + left
      persist()
    end
  elseif b == BOTTOM then
    setPhase(nextPhase(), false)
  elseif b == TOP then
    setPhase(phase, false)
  end
end
