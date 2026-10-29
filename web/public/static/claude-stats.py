#!/usr/bin/env python3
# Claude Stats for makerApp: shows your Claude Code stats on your makerDevice.
#
# It reads the history Claude Code keeps in ~/.claude/projects, takes only
# token counts, models and times from it (never what you or Claude wrote), and
# sends totals to your push link: favorite model, tokens, sessions, streaks and
# tokens per day. It only reads: Claude Code and its settings stay as they are.
# The one file it writes is a cache, ~/.cache/makerapp-claude-stats.json, so
# it doesn't reread old history every time.
#
#   python3 claude-stats.py YOUR_PUSH_LINK           send once
#   python3 claude-stats.py YOUR_PUSH_LINK --watch   send every 5 minutes until you stop it (Ctrl-C)
import glob, json, os, sys, time, urllib.request
from collections import defaultdict
from datetime import date, datetime, timedelta

HOME = os.path.expanduser("~/.claude")
CACHE = os.path.expanduser("~/.cache/makerapp-claude-stats.json")
EVERY = 300  # seconds between sends with --watch


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


def send(url, stats):
    body = json.dumps({"stats": stats}).encode()
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
    urllib.request.urlopen(req, timeout=10).close()


def main():
    args = sys.argv[1:]
    url = next((a for a in args if not a.startswith("--")), "")
    if not url.startswith(("https://", "http://")):
        sys.exit("Usage: python3 claude-stats.py YOUR_PUSH_LINK [--watch]\nYour push link is on the app's App Store page.")
    os.makedirs(os.path.dirname(CACHE), exist_ok=True)
    while True:
        stats = compute_stats()
        if not stats:
            print("No Claude Code history in ~/.claude/projects yet.", flush=True)
        else:
            try:
                send(url, stats)
                print(time.strftime("%H:%M"), "sent:", ", ".join(f"{i['label']} {i['value']}" for i in stats["items"][:3]), flush=True)
            except OSError as e:
                print(time.strftime("%H:%M"), "couldn't send:", e, flush=True)
        if "--watch" not in args:
            return
        time.sleep(EVERY)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        pass
