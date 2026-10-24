# Downloadable apps

Each `.lua` file here is an app that runs on the board: the games and tools
like Pomodoro. Boards on firmware 3.0 or later download an app when it's
installed and delete it (and what it saved) when it's uninstalled. Editing a
file ships a new version: its content hash changes, and boards fetch it the
next time they get their app list.

To add one, put the script here and add an entry to `Catalog` in
`internal/engine/engine.go`: its name, publisher, `Lua` set to the first
firmware with every function it uses (3.0, or 3.1 for `now`, `save` and
`saved`), and `Game: true` to list it under Games.

Check every app before shipping it:

```sh
make games-check   # runs each app for 100 simulated seconds with random presses
```

It uses the same API and limits as the board, so a script that passes won't
crash or freeze the board.

## A game

A script defines four functions. The board calls them; everything else is up
to the game.

```lua
function start() end          -- once, when the game is opened
function update(dt) end       -- every frame (about 40 a second); dt in seconds
function draw() end           -- every frame, after update; draw the whole screen
function button(b, down) end  -- a press (down = true) or release of TOP, MIDDLE or BOTTOM
```

Holding the middle button for a second always leaves the game; the game sees
the press first. Show controls on a start card, and use `best()` for a high
score.

## API

The screen is `W` × `H` (240 × 135). Coordinates are pixels from the top left;
fractions are cut off. Colours are numbers from `rgb()` or the names below.

| Function | |
|---|---|
| `rgb(0xRRGGBB)` | a colour |
| `cls(colour)` | fill the screen |
| `rect(x, y, w, h, colour)` / `rectb(...)` | filled / outlined rectangle |
| `rrect(x, y, w, h, r, colour)` | filled rectangle with rounded corners |
| `circ(x, y, r, colour)` / `circb(...)` | filled / outlined circle |
| `line(x0, y0, x1, y1, colour)` | |
| `tri(x0, y0, x1, y1, x2, y2, colour)` | filled triangle |
| `pix(x, y, colour)` | one pixel |
| `text(x, y, s, size, colour)` | `y` is the baseline; `size` is `SMALL`, `BOLD` or `LARGE` |
| `ctext(y, s, size, colour [, dx])` | centred, nudged right by `dx` |
| `shadow(y, s, size, colour)` | centred, with a drop shadow |
| `twidth(s, size)` | text width in pixels |
| `card(title, line1 [, line2 [, line3]])` | the start / game-over card; about 24 characters per line |
| `hog(x, y, r [, colour])` | the hedgehog, centred at `x, y` |
| `held(b)` | whether `TOP`, `MIDDLE` or `BOTTOM` is down |
| `millis()` | milliseconds since the board started |
| `now()` | the real time in seconds since 1970, or 0 while the board doesn't know it yet (3.1) |
| `best()` / `best(score)` | the saved high score; with a score, keeps it if it's higher and returns the best |
| `save(key, value)` | keeps a number, string or boolean for next time, also through restarts; `nil` forgets it (3.1) |
| `saved(key)` | what `save` kept, or `nil` (3.1) |
| `print(...)` | writes to the board's serial log |

Colours: `BLACK`, `WHITE`, `HOG`, `GRAY`, `DARK`, `RED`, `GREEN`, `BLUE`.

Lua's `table`, `string`, `math` and `coroutine` libraries are there;
`math.random` uses the board's hardware random source. There is no file,
network or OS access, and no `load`.

## Limits

- 160 KB of memory per game (the current ones use 26–53 KB).
- A single call into the game must finish within 250 ms or about four million
  Lua instructions, so an endless loop ends the game instead of the board.
- Scripts up to 32 KB. Storage on the board is 128 KB in all.
- `save`: 32 keys per app, keys up to 24 characters, text up to 200. The board
  writes saved values to flash at most every two seconds, so saving often is
  fine but the last two seconds can be lost if the power goes.
- Numbers are 32-bit: integers up to about two billion, floats with about 7
  digits.

When a game breaks a rule or hits an error, the board shows the error and its
line number on a card; holding the middle button leaves.
