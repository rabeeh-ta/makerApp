-- Flappy Hog, ported from PostHog's DeskHog src/flappy_bird.cpp (MIT, see
-- THIRD_PARTY_NOTICES.md). Any button flaps; the sky and the ground both end it.

local BIRD_X, BIRD_R = 40, 5
local PIPE_W, GAP, MIN_PIPE, SPACING = 20, 64, 15, 130
local GRAVITY, FLAP, SPEED = 520, -155, 62 -- px/s², px/s, px/s
local PIPE_GREEN = rgb(0x008000)

local state, y, vy, score, top, overAt
local pipes, sky = {}, {}

local function randomGap() return MIN_PIPE + math.random(0, H - 2 * MIN_PIPE - GAP) end

local function reset()
  state, y, vy, score = "ready", H / 2, 0, 0
  for i = 1, 2 do pipes[i] = { x = W + 40 + (i - 1) * SPACING, gap = randomGap(), scored = false } end
end

function start()
  -- DeskHog's sunset: orange at the top fading to indigo at the bottom.
  for row = 0, H - 1 do
    local t = row / (H - 1)
    local r = math.floor(0xFF + (0x4B - 0xFF) * t)
    local g = math.floor(0xA5 * (1 - t))
    local b = math.floor(0x82 * t)
    sky[row] = rgb(r * 65536 + g * 256 + b)
  end
  top = best()
  reset()
end

local function crash()
  state, overAt = "over", millis()
  top = best(score)
end

function update(dt)
  if state == "ready" then
    y = H / 2 + 4 * math.sin(millis() / 200) -- hover until the first flap
    return
  end
  if state ~= "playing" then return end
  vy = vy + GRAVITY * dt
  y = y + vy * dt
  if y - BIRD_R <= 0 or y + BIRD_R >= H then return crash() end
  for _, p in ipairs(pipes) do
    p.x = p.x - SPEED * dt
    local overlapX = BIRD_X + BIRD_R > p.x and BIRD_X - BIRD_R < p.x + PIPE_W
    if overlapX and (y - BIRD_R < p.gap or y + BIRD_R > p.gap + GAP) then return crash() end
    if not p.scored and BIRD_X - BIRD_R > p.x + PIPE_W then
      p.scored, score = true, score + 1
    end
    if p.x + PIPE_W < 0 then
      p.x = math.max(pipes[1].x, pipes[2].x) + SPACING
      p.gap, p.scored = randomGap(), false
    end
  end
end

local function pipe(x, y0, h)
  if h <= 0 then return end
  rect(x, y0, PIPE_W, h, PIPE_GREEN)
  rectb(x, y0, PIPE_W, h, BLACK)
end

function draw()
  for row = 0, H - 1 do rect(0, row, W, 1, sky[row]) end
  for _, p in ipairs(pipes) do
    pipe(p.x, 0, p.gap)
    pipe(p.x, p.gap + GAP, H - p.gap - GAP)
  end
  hog(BIRD_X, y, BIRD_R)
  text(7, 19, "Score: " .. score, BOLD, BLACK)
  text(6, 18, "Score: " .. score, BOLD, WHITE)
  if state == "ready" then
    shadow(64, "Flappy Hog", LARGE, WHITE)
    shadow(88, "Press any button to flap", SMALL, WHITE)
    shadow(108, "Hold middle to leave", SMALL, WHITE)
  elseif state == "over" then
    shadow(60, "Game over", LARGE, WHITE)
    shadow(84, "Score " .. score .. "   Best " .. top, BOLD, WHITE)
    shadow(106, "Press to play again", SMALL, WHITE)
  end
end

function button(b, down)
  if not down then return end
  if state == "over" then
    if millis() - overAt > 600 then reset() end -- ignore frantic taps right after a crash
    return
  end
  state, vy = "playing", FLAP
end
