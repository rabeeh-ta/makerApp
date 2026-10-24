-- Paddle, ported from PostHog's DeskHog src/game/PaddleGame.cpp and
-- src/ui/PaddleCard.cpp (MIT, see THIRD_PARTY_NOTICES.md). Pong against the
-- board: top and bottom move, middle starts or pauses, first to 5 wins.

local PW, PH, BALL, WIN = 5, 30, 5, 5
local PLAYER_X, AI_X = 5, W - 2 * PW
local PLAYER_SPEED, AI_SPEED, BALL_VX, BALL_VY = 150, 55, 85, 55 -- px/s
local AI_LAG = 20 -- the board only chases the ball once it's this far off-centre
local SERVE_MS = 1500
local PHRASES = { "Hog-tastic!", "Hawkins would be proud", "Spiked-em!", "Prickly perfect!" }
local NET = rgb(0x333333)

local state, py, ay, bx, by, vx, vy, you, them, serveAt, youServe, verdict

local function centreBall()
  bx, by, vx, vy = W // 2 - BALL // 2, H // 2 - BALL // 2, 0, 0
end

local function serve(player)
  centreBall()
  youServe, serveAt, state = player, millis(), "serve"
end

function start()
  state, you, them = "start", 0, 0
  py, ay = H // 2 - PH // 2, H // 2 - PH // 2
  centreBall()
end

local function point(forYou)
  if forYou then you = you + 1 else them = them + 1 end
  if you >= WIN or them >= WIN then
    state = "over"
    verdict = you >= WIN and PHRASES[math.random(#PHRASES)] or "The board wins"
  else
    serve(false) -- DeskHog has the board serve after every point
  end
end

local function clamp(v, lo, hi) return math.max(lo, math.min(hi, v)) end

function update(dt)
  if state == "serve" and millis() - serveAt >= SERVE_MS then
    vx = youServe and BALL_VX or -BALL_VX
    vy = math.random(2) == 1 and BALL_VY or -BALL_VY
    state = "playing"
  end
  if state ~= "playing" then return end

  local dir = (held(BOTTOM) and 1 or 0) - (held(TOP) and 1 or 0)
  py = clamp(py + dir * PLAYER_SPEED * dt, 0, H - PH)

  bx, by = bx + vx * dt, by + vy * dt
  if by <= 0 then
    by, vy = 0, math.abs(vy)
  elseif by >= H - BALL then
    by, vy = H - BALL, -math.abs(vy)
  end

  local aiCentre, ballCentre = ay + PH / 2, by + BALL / 2
  if ballCentre > aiCentre + AI_LAG then
    ay = ay + AI_SPEED * dt
  elseif ballCentre < aiCentre - AI_LAG then
    ay = ay - AI_SPEED * dt
  end
  ay = clamp(ay, 0, H - PH)

  if vx < 0 and bx < PLAYER_X + PW and bx + BALL > PLAYER_X and by < py + PH and by + BALL > py then
    bx, vx = PLAYER_X + PW, -vx
  end
  if vx > 0 and bx + BALL > AI_X and bx < AI_X + PW and by < ay + PH and by + BALL > ay then
    bx, vx = AI_X - BALL, -vx
  end
  if bx >= W - BALL then
    point(true)
  elseif bx <= 0 then
    point(false)
  end
end

function draw()
  cls(BLACK)
  for y = 0, H - 1, 10 do rect(W // 2 - 1, y, 2, 5, NET) end
  text(W // 2 - 30, 24, you, LARGE, WHITE)
  text(W // 2 + 18, 24, them, LARGE, WHITE)
  rect(PLAYER_X, py, PW, PH, WHITE)
  rect(AI_X, ay, PW, PH, WHITE)
  if state ~= "start" and state ~= "over" then rrect(bx, by, BALL, BALL, 2, WHITE) end
  if state == "start" then
    shadow(62, "PADDLE!", LARGE, HOG)
    shadow(84, "Middle: start", SMALL, WHITE)
    shadow(102, "Top / bottom: move", SMALL, WHITE)
    shadow(120, "Hold middle to leave", SMALL, WHITE)
  elseif state == "serve" then
    shadow(76, "READY?", LARGE, HOG)
  elseif state == "paused" then
    shadow(70, "PAUSED", LARGE, HOG)
    shadow(92, "Middle: resume", SMALL, WHITE)
  elseif state == "over" then
    shadow(64, verdict, BOLD, HOG)
    shadow(88, "Middle: play again", SMALL, WHITE)
  end
end

function button(b, down)
  if not down or b ~= MIDDLE then return end
  if state == "start" or state == "over" then
    you, them = 0, 0
    py, ay = H // 2 - PH // 2, H // 2 - PH // 2
    serve(false) -- as in DeskHog, the board serves first
  elseif state == "playing" then
    state = "paused"
  elseif state == "paused" then
    state = "playing"
  end
end
