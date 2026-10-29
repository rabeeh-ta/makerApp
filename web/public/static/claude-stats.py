#!/usr/bin/env python3
# Claude Stats for makerApp: a Claude Code status line that also sends your
# Claude usage to your makerDevice. Prints "Opus 5.5 · session 37% · week 12%"
# and posts the numbers to your push link when they change (or every 2
# minutes). Every 15 minutes it also starts itself with --stats in the
# background, which totals your Claude Code transcripts (sessions, tokens,
# streaks) for the stats screens; the next post carries them.
#
# Set it as Claude Code's status line in ~/.claude/settings.json, with your
# push link from the app's App Store page:
#   "statusLine": {"type": "command", "command": "python3 ~/.claude/claude-stats.py YOUR_PUSH_LINK"}
import glob, json, os, subprocess, sys, time
from collections import defaultdict
from datetime import date, datetime, timedelta

PUSH_URL = next((a for a in sys.argv[1:] if a != "--stats"), "")  # without one it is only a status line
HOME = os.path.expanduser("~/.claude")
LAST = os.path.join(HOME, "makerapp-usage.last")
STATS = os.path.join(HOME, "makerapp-stats.json")
CACHE = os.path.join(HOME, "makerapp-stats-cache.json")


def model_name(m):
    # claude-opus-5-5 -> Opus 5.5, claude-haiku-4-5-20251001 -> Haiku 4.5
    parts = [p for p in m.removeprefix("claude-").split("-") if not (p.isdigit() and len(p) == 8)]
    return parts[0].title() + " " + ".".join(parts[1:]) if parts else m


def compact(n):
    # 6397126249 -> 6.4b, 18956206 -> 19m, as /usage shows them
    for size, unit in ((1e9, "b"), (1e6, "m"), (1e3, "k")):
        if n >= size:
            return f"{n / size:.1f}".rstrip("0").rstrip(".") + unit
    return str(n)


def scan(path):
    # One transcript's totals, mergeable with the others'.
    seen, days, models, sessions = set(), defaultdict(lambda: [0, 0]), defaultdict(lambda: [0, 0, 0, 0]), {}
    for line in open(path, errors="ignore"):
        if '"assistant"' not in line or '"usage"' not in line:
            continue
        try:
            d = json.loads(line)
        except ValueError:
            continue
        msg = d.get("message") or {}
        u, key = msg.get("usage"), (msg.get("id"), d.get("requestId"))
        if d.get("type") != "assistant" or not u or key in seen or msg.get("model", "").startswith("<"):
            continue
        seen.add(key)
        ts = datetime.fromisoformat(d["timestamp"].replace("Z", "+00:00")).astimezone()
        tok = [u.get("input_tokens", 0), u.get("output_tokens", 0), u.get("cache_read_input_tokens", 0), u.get("cache_creation_input_tokens", 0)]
        day = days[ts.date().isoformat()]
        day[0], day[1] = day[0] + 1, day[1] + sum(tok)
        models[msg["model"]] = [a + b for a, b in zip(models[msg["model"]], tok)]
        if not d.get("isSidechain"):
            s = sessions.setdefault(d["sessionId"], [ts.timestamp(), ts.timestamp()])
            s[0], s[1] = min(s[0], ts.timestamp()), max(s[1], ts.timestamp())
    return {"days": days, "models": models, "sessions": sessions}


def compute_stats():
    try:
        cache = json.load(open(CACHE))
    except (OSError, ValueError):
        cache = {}
    files = {}
    for path in glob.glob(os.path.join(HOME, "projects", "**", "*.jsonl"), recursive=True):
        st = os.stat(path)
        got = cache.get(path)
        files[path] = got if got and got["sig"] == [st.st_size, st.st_mtime] else {"sig": [st.st_size, st.st_mtime], **scan(path)}
    json.dump(files, open(CACHE, "w"))

    days, models, sessions = defaultdict(lambda: [0, 0]), defaultdict(lambda: [0, 0, 0, 0]), {}
    for f in files.values():
        for k, v in f["days"].items():
            days[k] = [a + b for a, b in zip(days[k], v)]
        for k, v in f["models"].items():
            models[k] = [a + b for a, b in zip(models[k], v)]
        for k, (a, b) in f["sessions"].items():
            s = sessions.setdefault(k, [a, b])
            s[0], s[1] = min(s[0], a), max(s[1], b)
    if not days:
        return None

    today, active = date.today(), sorted(date.fromisoformat(d) for d in days)
    streak, best, run, prev = 0, 0, 0, None
    for d in active:
        run = run + 1 if prev and d - prev == timedelta(days=1) else 1
        best, prev = max(best, run), d
    d = today if today in active else today - timedelta(days=1)
    while d in active:
        streak, d = streak + 1, d - timedelta(days=1)
    longest = max((b - a for a, b in sessions.values()), default=0)
    dd, rem = divmod(int(longest), 86400)
    hh, mm = rem // 3600, rem % 3600 // 60
    tok = [sum(m[i] for m in models.values()) for i in range(4)]
    last30 = [days.get((today - timedelta(days=i)).isoformat(), [0, 0])[1] for i in range(29, -1, -1)]
    top = max(days, key=lambda k: days[k][0])
    items = [  # in /usage's order
        ("Favorite model", model_name(max(models, key=lambda k: sum(models[k])))),
        ("Total tokens", compact(sum(tok))),
        ("Sessions", str(len(sessions))),
        ("Longest session", f"{dd}d {hh}h" if dd else f"{hh}h {mm}m"),
        ("Active days", f"{len(active)}/{(today - active[0]).days + 1}"),
        ("Longest streak", f"{best} days"),
        ("Most active day", date.fromisoformat(top).strftime("%b %d").replace(" 0", " ")),
        ("Current streak", f"{streak} days"),
        ("Input tokens", compact(tok[0])),
        ("Output tokens", compact(tok[1])),
        ("Cache read", compact(tok[2])),
        ("Cache write", compact(tok[3])),
    ]
    return {"items": [{"label": a, "value": b} for a, b in items], "days": last30, "tokens_30d": sum(last30)}


if "--stats" in sys.argv[1:]:
    stats = compute_stats()
    if stats:
        json.dump(stats, open(STATS + ".tmp", "w"))
        os.replace(STATS + ".tmp", STATS)
    sys.exit()

data = json.load(sys.stdin)
limits = data.get("rate_limits") or {}


def window(key, fmt):
    w = limits.get(key) or {}
    if w.get("used_percentage") is None:
        return None
    resets = datetime.fromtimestamp(w["resets_at"]).strftime(fmt).replace(" 0", " ").lstrip("0") if w.get("resets_at") else ""
    return {"used": round(w["used_percentage"]), "resets": resets}


session, week = window("five_hour", "%I:%M %p"), window("seven_day", "%a %I:%M %p")
parts = [(data.get("model") or {}).get("display_name", "Claude")]
parts += [f"{name} {w['used']}%" for name, w in (("session", session), ("week", week)) if w]
print(" · ".join(parts))

quiet = {"stdout": subprocess.DEVNULL, "stderr": subprocess.DEVNULL, "start_new_session": True}
try:
    stats_age = time.time() - os.path.getmtime(STATS)
except OSError:
    stats_age = float("inf")
if stats_age > 900:
    open(STATS, "a").close()
    os.utime(STATS)  # so the next refresh doesn't start another while this one runs
    subprocess.Popen([sys.executable, os.path.abspath(__file__), "--stats"], **quiet)

try:
    stats = json.load(open(STATS))
except (OSError, ValueError):
    stats = None
if PUSH_URL and (session or week or stats):  # plans without session limits still send their stats
    payload = json.dumps({"session": session, "week": week, "stats": stats})
    try:
        stamp, _, sent = open(LAST).read().partition("\n")
    except OSError:
        stamp, sent = "0", ""
    if payload != sent or time.time() - float(stamp or 0) > 120:
        with open(LAST, "w") as f:
            f.write(f"{time.time()}\n{payload}")
        subprocess.Popen(["curl", "-s", "-m", "10", "-H", "Content-Type: application/json", "-d", payload, PUSH_URL], **quiet)
