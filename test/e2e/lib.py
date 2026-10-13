"""agmux black-box helpers. All tmux use goes through socket `agmux-test`; never the user's server.

Isolation: each Env has its own HOME / XDG_STATE_HOME / fake `ccmux` on PATH (logs argv, serves show.json).
Runs are serialised by a lock file because the tmux socket name is fixed.
"""
import atexit, datetime, fcntl, json, os, pathlib, re, shlex, shutil, subprocess, tempfile, time, unicodedata, uuid

BIN = os.environ.get("AGMUX_BIN") or os.path.expanduser("~/go/bin/agmux")
SOCK = os.environ.get("AGMUX_TEST_SOCK", "agmux-test")  # convention: agmux-test; override when another session shares the machine
HERE = pathlib.Path(__file__).resolve().parent

_lock = open(HERE / f".lock-{os.environ.get('AGMUX_TEST_SOCK', 'agmux-test')}", "w")  # one runner per tmux socket name; other roles use other sockets
fcntl.flock(_lock, fcntl.LOCK_EX)


def tm(*args, check=False):
    return subprocess.run(["tmux", "-L", SOCK, "-f", "/dev/null", *map(str, args)], capture_output=True, text=True, check=check)


atexit.register(lambda: tm("kill-server"))


def wait(pred, timeout=8.0, step=0.1):
    """Poll until pred() is truthy (returns it) or timeout (returns the last falsy value)."""
    end, v = time.time() + timeout, None
    while time.time() < end:
        v = pred()
        if v:
            return v
        time.sleep(step)
    return v


# ---------------------------------------------------------------- environment
class Env:
    def __init__(self):
        self.tmp = pathlib.Path(tempfile.mkdtemp(prefix="agmux-e2e-"))
        self.home, self.state, self.bin, self.fake = (self.tmp / n for n in ("home", "state", "bin", "fake"))
        for d in (self.home, self.state, self.bin, self.fake):
            d.mkdir()
        (self.fake / "show.json").write_text("[]")
        self.pane_ids = {}
        (self.fake / "tmux.clients").write_text("1\tc-test\tmain\n")  # one outer client: enter can switch; T10 tests override with set_tmux()
        d = shlex.quote(str(self.fake))
        script = f"""#!/bin/sh
d={d}
if [ "$1" = show ]; then
  date +%s >> "$d/show.calls"
  if [ -e "$d/showfail" ]; then printf 'ccmux: show exploded\\nsecond poll-error line\\nthird poll-error line\\n' >&2; exit 1; fi
  if [ -e "$d/orders" ]; then  # rotate through show.0.json .. show.N-1.json, one per poll
    n=$(cat "$d/orders"); c=$(wc -l < "$d/show.calls"); cat "$d/show.$(( (c - 1) % n )).json"; exit 0
  fi
  cat "$d/show.json"; exit 0
fi
printf '%s\\037' "$@" >> "$d/calls.log"; printf '\\n' >> "$d/calls.log"
if [ "$1" = review ] && [ -e "$d/review.fail" ]; then printf 'review: first stderr line\\nreview exploded: the last word\\n' >&2; exit 1; fi
if [ "$1" = review ]; then printf 'REVIEW-RUNNING\\n'; [ -e "$d/review.hold" ] && sleep 2; printf 'REVIEW-DONE\\n'; fi
if [ -e "$d/fail" ]; then printf 'boom from fake ccmux\\nsecond stderr line\\nthird stderr line\\n' >&2; exit 1; fi
exit 0
"""
        (self.bin / "ccmux").write_text(script)
        (self.bin / "ccmux").chmod(0o755)
        # fake `tmux` (T10: agmux calls tmux straight from $PATH): records argv, answers display-message / list-clients from files
        tmux = f"""#!/bin/sh
d={d}
printf '%s\\037' "$@" >> "$d/tmux.log"; printf '\\n' >> "$d/tmux.log"
perl -MTime::HiRes=time -e 'printf "%.3f\\n", time' >> "$d/tmux.times"
if [ -e "$d/tmux.fail" ] && [ "$(cat "$d/tmux.fail")" = "$1" ]; then printf 'tmux: boom from fake tmux\\nsecond tmux line\\n' >&2; exit 1; fi
case "$1" in
  display-message) case " $* " in *" ; "*) echo "0 0 0 default 50"; exit 0;; esac
    case " $* " in *client_termname*) if [ -e "$d/tmux.termname" ]; then cat "$d/tmux.termname"; else echo xterm-256color; fi; exit 0;; esac  # T28: renderer auto-detect  # T24: the preview's `display-message <cursor format> ; capture-pane`: cursor hidden, then an empty screen
    if [ -e "$d/tmux.session" ]; then cat "$d/tmux.session"; else echo fl-own; fi; exit 0;;
  show|show-options) case " $* " in *allow-passthrough*) if [ -e "$d/tmux.passthrough" ]; then cat "$d/tmux.passthrough"; else echo on; fi; exit 0;; esac; exit 0;;  # T28
  list-clients) [ -e "$d/tmux.clients" ] && cat "$d/tmux.clients"; exit 0;;
  load-buffer) cat >> "$d/tmux.stdin"; exit 0;;
  show-environment) if [ -e "$d/tmux.env" ]; then cat "$d/tmux.env"; exit 0; else echo "unknown variable: $4" >&2; exit 1; fi;;
esac
exit 0
"""
        (self.bin / "tmux").write_text(tmux)
        (self.bin / "tmux").chmod(0o755)
        # PATH for the REAL-tmux tests: the fake ccmux, but not the fake tmux
        self.bin_real = self.tmp / "bin_real"; self.bin_real.mkdir()
        os.symlink(self.bin / "ccmux", self.bin_real / "ccmux")

    def cleanup(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    @property
    def prefix_real(self):
        """like `prefix`, but PATH has the REAL tmux (and the fake ccmux) instead of the fake tmux, for tests whose panes live in the private tmux server"""
        q = shlex.quote
        return (f"env -i PATH={q(str(self.bin_real))}:/opt/homebrew/bin:/usr/bin:/bin HOME={q(str(self.home))} XDG_STATE_HOME={q(str(self.state))} "
                "TERM=xterm-256color COLORTERM=truecolor LANG=en_US.UTF-8")

    @property
    def prefix(self):
        q = shlex.quote
        return (f"env -i PATH={q(str(self.bin))}:/usr/bin:/bin HOME={q(str(self.home))} XDG_STATE_HOME={q(str(self.state))} "
                "TERM=xterm-256color COLORTERM=truecolor LANG=en_US.UTF-8")

    def write_show(self, sessions):
        (self.fake / "show.json").write_text(json.dumps(sessions))

    def write_show_orders(self, variants):
        """Each poll gets the next variant (a different array order of the same sessions)."""
        for i, v in enumerate(variants):
            (self.fake / f"show.{i}.json").write_text(json.dumps(v))
        (self.fake / "orders").write_text(str(len(variants)))

    def calls(self):
        p = self.fake / "calls.log"
        if not p.exists():
            return []
        return [[a for a in line.split("\x1f") if a != ""] for line in p.read_text().split("\n") if line]

    def actions(self):
        """What the user asked for, whichever channel carries it: ccmux calls plus the tmux ones that replaced `ccmux switch` / `ccmux send` in T10
        (switch-client -t <pane> -> ["switch", id]; send-keys -t <pane> -l -- <text> -> ["send", id, "--", text]; the Enter key press is not a separate action)."""
        out = list(self.calls())
        for c in self.tmux_calls():
            if c[0] == "switch-client" and "-t" in c:
                out.append(["switch", self.pane_ids.get(c[c.index("-t") + 1], "?")])
            elif c[0] == "send-keys" and "-l" in c and "--" in c:
                out.append(["send", self.pane_ids.get(c[c.index("-t") + 1], "?"), "--", " ".join(c[c.index("--") + 1:])])
        return out

    def tmux_calls(self):
        p = self.fake / "tmux.log"
        if not p.exists():
            return []
        return [[a for a in line.split("\x1f") if a != ""] for line in p.read_text().split("\n") if line]

    def tmux_stdin(self):
        p = self.fake / "tmux.stdin"
        return p.read_text() if p.exists() else ""

    def tmux_times(self):
        p = self.fake / "tmux.times"
        return [float(x) for x in p.read_text().split()] if p.exists() else []

    def set_term(self, termname=None, passthrough=None):
        """T28: what the fake tmux answers to `display-message -p -c <client> '#{client_termname}'` (default xterm-256color) and `show -gv allow-passthrough` (default on)."""
        for name, v in (("tmux.termname", termname), ("tmux.passthrough", passthrough)):
            p = self.fake / name
            p.unlink(missing_ok=True) if v is None else p.write_text(v + "\n")

    def set_tmux(self, session="fl-own", clients=(), fail=None, session_env=None):
        """clients: (activity, client_name, client_session) rows served by `tmux list-clients -F ...`; fail: subcommand that exits 1."""
        (self.fake / "tmux.session").write_text(session + "\n")  # NB: tmux keeps trailing spaces in names, so the file keeps them too
        (self.fake / "tmux.clients").write_text("".join(f"{a}\t{n}\t{s}\n" for a, n, s in clients))
        if fail:
            (self.fake / "tmux.fail").write_text(fail)
        if session_env is not None:  # what `tmux show-environment -t <session> AGMUX_CLIENT` prints, e.g. "AGMUX_CLIENT=/dev/outer" or "-AGMUX_CLIENT"
            (self.fake / "tmux.env").write_text(session_env + "\n")

    def show_calls(self):
        p = self.fake / "show.calls"
        return len(p.read_text().split()) if p.exists() else 0

    def flag(self, name, on=True):
        p = self.fake / name
        p.touch() if on else p.unlink(missing_ok=True)

    def hud(self, native_id, text):
        d = self.state / "agmux"
        d.mkdir(exist_ok=True)
        (d / f"{native_id}.json").write_text(text)

    def avatars_json(self, mapping):
        d = self.state / "agmux"
        d.mkdir(exist_ok=True)
        (d / "avatars.json").write_text(json.dumps(mapping))


# ---------------------------------------------------------------- screen parsing
_PAL = [(0, 0, 0), (205, 49, 49), (13, 188, 121), (229, 229, 16), (36, 114, 200), (188, 63, 188), (17, 168, 205), (229, 229, 229),
        (102, 102, 102), (241, 76, 76), (35, 209, 139), (245, 245, 67), (59, 142, 234), (214, 112, 214), (41, 184, 219), (255, 255, 255)]


def rgb(c):
    """Colour spec -> (r,g,b) or None (default colour)."""
    if c is None:
        return None
    if c[0] == "rgb":
        return c[1:]
    n = c[1]
    if n < 16:
        return _PAL[n]
    if n >= 232:
        v = 8 + 10 * (n - 232)
        return (v, v, v)
    n -= 16
    lv = [0, 95, 135, 175, 215, 255]
    return (lv[n // 36], lv[n // 6 % 6], lv[n % 6])


def lum(c):
    r = rgb(c)
    return None if r is None else 0.2126 * r[0] + 0.7152 * r[1] + 0.0722 * r[2]


def is_red(c):
    r = rgb(c)
    return bool(r) and r[0] >= 200 and r[1] <= 130 and r[2] <= 150


def is_green(c):
    r = rgb(c)
    return bool(r) and r[1] >= 150 and r[1] > r[0] and r[1] > r[2]


def is_orange(c):
    r = rgb(c)
    return bool(r) and r[0] >= 200 and 90 <= r[1] <= 190 and r[2] <= 110


def is_gray(c):
    r = rgb(c)
    return bool(r) and max(r) - min(r) <= 40


def is_magenta(c):
    r = rgb(c)
    return bool(r) and r[0] >= 150 and r[2] >= 150 and r[1] <= 120


C_GREEN, C_YELLOW, C_RED, C_DIM = ("rgb", 166, 227, 161), ("rgb", 249, 226, 175), ("rgb", 255, 97, 136), ("rgb", 88, 91, 112)


def cw(ch):
    if unicodedata.combining(ch):
        return 0
    return 2 if unicodedata.east_asian_width(ch) in "WF" else 1


_TOK = re.compile(r"(\x1b\[[0-9;:]*m)")


def _sgr(params, st):
    p = [int(x) if x else 0 for x in params.replace(":", ";").split(";")] or [0]
    i = 0
    while i < len(p):
        v = p[i]
        if v == 0:
            st.update(fg=None, bg=None, dim=False, bold=False, rev=False)
        elif v == 1: st["bold"] = True
        elif v == 2: st["dim"] = True
        elif v == 22: st["bold"] = st["dim"] = False
        elif v == 7: st["rev"] = True
        elif v == 27: st["rev"] = False
        elif 30 <= v <= 37: st["fg"] = ("idx", v - 30)
        elif 90 <= v <= 97: st["fg"] = ("idx", v - 90 + 8)
        elif 40 <= v <= 47: st["bg"] = ("idx", v - 40)
        elif 100 <= v <= 107: st["bg"] = ("idx", v - 100 + 8)
        elif v == 39: st["fg"] = None
        elif v == 49: st["bg"] = None
        elif v in (38, 48):
            k = "fg" if v == 38 else "bg"
            if i + 1 < len(p) and p[i + 1] == 5 and i + 2 < len(p):
                st[k] = ("idx", p[i + 2]); i += 2
            elif i + 1 < len(p) and p[i + 1] == 2 and i + 4 < len(p):
                st[k] = ("rgb", p[i + 2], p[i + 3], p[i + 4]); i += 4
        i += 1


class Row:
    """One screen row: cells[i] = (ch, fg, bg, dim); wide chars occupy two cells (second has ch='')."""

    def __init__(self, cells):
        self.cells = cells
        idx, txt = [], []
        for col, c in enumerate(cells):
            if c[0]:
                txt.append(c[0]); idx.append(col)
        self.text, self._idx = "".join(txt), idx

    def find(self, s, start=0):
        i = self.text.find(s, start)
        return -1 if i < 0 else self._idx[i]

    def sub(self, c0, c1):
        return self.cells[c0:c1]

    def width_text(self, c0, c1):
        return "".join(c[0] for c in self.cells[c0:c1])


def parse_screen(raw):
    rows = []
    for line in raw.split("\n"):
        st = dict(fg=None, bg=None, dim=False, bold=False, rev=False)
        cells = []
        for tok in _TOK.split(line):
            m = _TOK.fullmatch(tok)
            if m:
                _sgr(m.group(1)[2:-1], st)
                continue
            for ch in tok:
                w = cw(ch)
                fg, bg = (st["bg"], st["fg"]) if st["rev"] else (st["fg"], st["bg"])
                if w == 0:
                    continue
                cells.append((ch, fg, bg, st["dim"]))
                if w == 2:
                    cells.append(("", fg, bg, st["dim"]))
        rows.append(Row(cells))
    return rows


def strip_ansi(s):
    return _TOK.sub("", s)


def disp_width(s):
    return sum(cw(c) for c in s)


# ---------------------------------------------------------------- cards
TL, TR, BL, BR = "╭┏╔", "╮┓╗", "╰┗╚", "╯┛╝"


def isin(ch, s):
    return bool(ch) and ch in s  # "" (wide-char filler) is `in` every string


class Card:
    def __init__(self, r, c, w, rows, ok, why=""):
        self.r, self.c, self.w, self.rows, self.ok, self.why = r, c, w, rows, ok, why

    def line(self, i):
        return self.rows[i].text if self.rows[i] else ""

    @property
    def text(self):
        return "\n".join(r.text for r in self.rows)

    def border_cells(self):
        out = []
        for i, row in enumerate(self.rows):
            if i in (0, len(self.rows) - 1):
                out += row.cells[:]
            else:
                out += [row.cells[0], row.cells[-1]]
        return out


def find_cards(snap, height=5):
    cards = []
    for r, row in enumerate(snap):
        for c, cell in enumerate(row.cells):
            if not isin(cell[0], TL):
                continue
            j = c + 1
            while j < len(row.cells) and not isin(row.cells[j][0], TR):
                j += 1
            if j >= len(row.cells):
                cards.append(Card(r, c, None, [], False, "top border has no right corner")); continue
            w = j - c + 1
            if r + height > len(snap):
                cards.append(Card(r, c, w, [], False, "card runs off the bottom")); continue
            rows = [Row(snap[r + i].cells[c:c + w]) for i in range(height)]
            ok, why = True, ""
            if not rows[-1].cells or not isin(rows[-1].cells[0][0], BL) or not isin(rows[-1].cells[-1][0], BR):  # empty row = capture caught a half-drawn frame
                ok, why = False, "bottom corners missing / misaligned"
            for i in range(1, height - 1):
                if len(rows[i].cells) < w or not isin(rows[i].cells[0][0], "│┃") or not isin(rows[i].cells[-1][0], "│┃"):
                    ok, why = False, f"row {i} side borders misaligned: {rows[i].text!r}"
            cards.append(Card(r, c, w, rows, ok, why))
    return cards


# ---------------------------------------------------------------- terminal under test
class Term:
    def __init__(self, env, args="board", w=200, h=50, binary=None, extra_env="", real_tmux=False):
        self.env, self.w, self.h = env, w, h
        self.name = "t" + uuid.uuid4().hex[:8]
        prefix = env.prefix
        if real_tmux:  # agmux reaches the private server like a pane inside it would: through $TMUX
            sock = tm("display-message", "-p", "#{socket_path},#{pid},0").stdout.strip()
            assert sock.count(",") == 2, f"no running private tmux server yet: {sock!r}"
            prefix, extra_env = env.prefix_real, f"TMUX={sock} {extra_env}"
        cmd = f"{prefix} {extra_env} {shlex.quote(binary or BIN)} {args}"
        r = tm("start-server", ";", "set", "-g", "remain-on-exit", "on", ";", "new-session", "-d", "-s", self.name,
               "-x", w, "-y", h, cmd)
        if r.returncode:
            raise RuntimeError(r.stderr)

    def raw(self):
        return tm("capture-pane", "-e", "-p", "-t", self.name).stdout

    def snap(self):
        return parse_screen(self.raw())

    def text(self):
        return tm("capture-pane", "-p", "-t", self.name).stdout

    def keys(self, *k):
        tm("send-keys", "-t", self.name, *k)

    def lit(self, s):
        # "--": literal text may itself start with a dash. tmux parses an argument ending in `;` as a command
        # separator and eats it, so each trailing `;` is passed as its own `\;` argument (same as agmux's literal()).
        body = s.rstrip(";")
        tm("send-keys", "-t", self.name, "-l", "--", *([body] if body else []), *["\\;"] * (len(s) - len(body)))

    def esc(self):
        self.keys("Escape"); time.sleep(0.4)  # ESC + next key would otherwise merge into Alt-<key>

    def click(self, col, row, n=1):
        """Inject an SGR mouse click at 0-based cell (col,row) straight into the pane's pty."""
        x, y = col + 1, row + 1
        self.lit((f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m") * n)

    def wheel(self, col, row, up=True, n=1):
        x, y = col + 1, row + 1
        self.lit((f"\x1b[<{64 if up else 65};{x};{y}M") * n)

    def resize(self, w, h):
        tm("resize-window", "-t", self.name, "-x", w, "-y", h)
        self.w, self.h = w, h

    def dead(self):
        return tm("display", "-p", "-t", self.name, "#{pane_dead}").stdout.strip() == "1"

    def exit_status(self, timeout=5):
        if not wait(self.dead, timeout):
            return None
        return int(tm("display", "-p", "-t", self.name, "#{pane_dead_status}").stdout.strip())

    def alive(self):
        return not self.dead()

    def close(self):
        tm("kill-session", "-t", self.name)


# ---------------------------------------------------------------- fixture
def iso(dt):
    return dt.strftime("%Y-%m-%dT%H:%M:%S.000Z")


def build_fixture(env, with_hud=True):
    """9 sessions / 5 groups. Returns dict name->meta used by assertions. Column order G1..G5."""
    now = datetime.datetime.now(datetime.timezone.utc)
    h = env.home
    long_cwd = "/zzz/agmux-e2e/aaaaaaaaaaaa/bbbbbbbbbbbb/cccccccccccc/dddddddddddd/eeeeeeeeeeee/ffffffffffff/gggggggggggg/final-dir"
    jsonl = env.tmp / "a1.jsonl"
    jsonl.write_text('{"type":"assistant","message":{"model":"claude-sonnet-4-5"}}\n{"type":"user"}\n'
                     '{"type":"assistant","message":{"model":"claude-opus-5-5"}}\n{"type":"user"}\n')

    def mk(id, name, cwd, project, status="idle", agent="claude", pane="%1", **kw):
        s = dict(id=id, summary=name, agentType=agent, cwd=cwd, project=project, mainRepoRoot=cwd, gitBranch="main", status=status,
                 attentionType=None, pendingTool=None, tmuxPane=pane, trackingMode="native" if pane else "background", subagents=[],
                 nativeSessionId="n-" + id, logPath=None, lastActivityAt=iso(now - datetime.timedelta(seconds=90)),
                 updatedAt=iso(now), backgroundDetail=None, lastPrompt=None, attentionState=None)
        s.update(kw)
        return s

    S = [
        mk("a1", "alpha-main", f"{h}/work/alpha", "alpha", "working", pendingTool="Bash", pane="%101",
           gitBranch="feature/qa-acceptance-very-long-branch-name-for-truncation", logPath=str(jsonl),
           subagents=[{"agentId": "x1", "status": "working", "pendingTool": None}, {"agentId": "x2", "status": "idle", "pendingTool": None}],
           lastPrompt='<cross-session-message from="uds:/tmp/cc-socks/1.sock" from-name="orch" from-mode="bypass">\n先修折行\n第二行不该出现\n</cross-session-message>'),
        mk("a2", None, f"{h}/work/alpha", "pj-fallback", pane="%102",
           lastPrompt='<cross-session-message from="uds:/tmp/cc-socks/9.sock" from-name="peer" from-mode="bypass">\n</cross-session-message>\nThis came from another Claude session — not typed by your user'),
        mk("a3", "告警会话-超长中文名字-用来测试宽字符截断-再多写一些字让它超过列宽", f"{h}/work/alpha", "alpha", "working",
           pane="%103", attentionType="permission", pendingTool="Bash"),
        mk("b1", "bravo-29", f"{h}/work/bravo", "bravo", "idle", agent="codex", pane="%104",
           subagents=[{"agentId": "y1", "status": "working", "pendingTool": None}]),  # raw idle, but a subagent is working -> `⏵ working · agents`
        mk("b2", "bravo-bg", f"{h}/work/bravo", "bravo", pane=None, lastPrompt="should-not-show-for-background",
           backgroundDetail="后台任务：交接文档已写好"),
        mk("c1", "charlie-30", f"{h}/work/charlie", "charlie", agent="gemini", pane="%105"),
        mk("c2", "charlie-59", f"{h}/work/charlie", "charlie", pane="%106", attentionState="unread",
           lastPrompt="<task-notification>\n<task-id>x1</task-id>\n<summary>构建完成 (exit 0)</summary>\n</task-notification>"),
        mk("d1", "delta-60", f"{h}/work/delta", "delta", "waiting", pane="%107", lastPrompt="\n\n  修 <b>折行</b> 问题\n第二行不该出现"),
        mk("e1", "echo-long", long_cwd, "echo", pane="%108", attentionState="read", lastPrompt="按 <leader>w 触发 Vec<T> 而 a < b"),
    ]
    env.write_show(S)
    env.pane_ids = {s["tmuxPane"]: s["id"] for s in S if s["tmuxPane"]}
    ctx = lambda p: '{"context_window":{"used_percentage":%s,"remaining_percentage":%s},"rate_limits":{"used_percentage":99}}' % (p, 100 - p)
    if with_hud:
        env.hud("n-a1", '{"rate_limits":{"used_percentage":99},"context_window":{"used_percentage":41,"remaining_percentage":59}}')
        for sid, p in (("b1", 29), ("c1", 30), ("c2", 59), ("d1", 60)):
            env.hud("n-" + sid, ctx(p))
    return dict(
        sessions=S, home=str(h), groups=["~/work/alpha", "~/work/bravo", "~/work/charlie", "~/work/delta", long_cwd],
        sizes=[3, 2, 2, 1, 1], pct={"alpha-main": 41, "bravo-29": 29, "charlie-30": 30, "charlie-59": 59, "delta-60": 60},
    )


MIN_COL, GAP, MAX_COLS = 40, 2, 4  # S1.1: column minimum 40; T8: at most 4 columns


def layout(W):
    n = max(1, min(MAX_COLS, (W + GAP) // (MIN_COL + GAP)))
    return n, (W - 2 * (n - 1)) // n


def parse_attr(raw):
    """Like parse_screen but keeps the attributes parse_screen folds away: rows of cells (char, reversed, underlined, fg). A wide character is followed by a filler cell ('', same attributes)."""
    rows = []
    for line in raw.split("\n"):
        st = dict(rev=False, ul=False, fg=None); cells = []
        for tok in _TOK.split(line):
            m = _TOK.fullmatch(tok)
            if m:
                p = [int(x) if x else 0 for x in m.group(1)[2:-1].replace(":", ";").split(";")] or [0]
                i = 0
                while i < len(p):
                    v = p[i]
                    if v == 0: st.update(rev=False, ul=False, fg=None)
                    elif v == 7: st["rev"] = True
                    elif v == 27: st["rev"] = False
                    elif v == 4: st["ul"] = True
                    elif v == 24: st["ul"] = False
                    elif v == 39: st["fg"] = None
                    elif v == 38 and i + 4 < len(p) and p[i + 1] == 2: st["fg"] = ("rgb", p[i + 2], p[i + 3], p[i + 4]); i += 4
                    elif v == 38 and i + 2 < len(p) and p[i + 1] == 5: st["fg"] = ("idx", p[i + 2]); i += 2
                    i += 1
                continue
            for ch in tok:
                w = cw(ch)
                if w == 0:
                    continue
                cells.append((ch, st["rev"], st["ul"], st["fg"]))
                if w == 2:
                    cells.append(("", st["rev"], st["ul"], st["fg"]))
        rows.append(cells)
    return rows


# ---------------------------------------------------------------- T28: image avatars (kitty graphics + Unicode placeholders)
PLACEHOLDER = "\U0010EEEE"
DIACRITICS = [0x305, 0x30D, 0x30E, 0x310, 0x312, 0x33D, 0x33E, 0x33F]  # kitty's rowcolumn-diacritics, first eight: index = row / column number
ID_BASE = 0x00C0DE00
KITTY_ENV = "TMUX=/fake/tmux,1,0 "  # extra_env for Term / ptyrun: agmux believes it runs inside tmux (the fake tmux answers); TERM stays xterm-256color


def png(rgb=(200, 60, 60), size=8, noise=False):
    """A tiny valid PNG (solid colour); different colours make different bytes, so frames are tellable apart."""
    import struct, zlib
    def chunk(t, d):
        c = struct.pack(">I", len(d)) + t + d
        return c + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    import random
    rnd = random.Random(sum(rgb))  # noise=True: incompressible pixels, to get an upload that needs several 4096-byte chunks
    raw = b"".join(b"\x00" + (bytes(rnd.randrange(256) for _ in range(size * 3)) if noise else bytes(rgb) * size) for _ in range(size))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")


def write_avatar_set(cfg, name, states, **files):
    """$XDG_CONFIG_HOME/agmux/avatars/<name>/<state>/NN.png: states = {"idle": 3, "working": 2, ...} (frame counts; frame k of state s has a colour of its own).
    files = extra raw files {"working/readme.txt": b"..."}. cfg = the XDG_CONFIG_HOME directory. Returns the set directory."""
    base = pathlib.Path(cfg) / "agmux" / "avatars" / name
    for si, (st, n) in enumerate(states.items()):
        (base / st).mkdir(parents=True, exist_ok=True)
        for k in range(n):
            (base / st / f"{k + 1:02d}.png").write_bytes(png((40 + 50 * si, 30 + 40 * k, 200 - 30 * k)))
    for rel, data in files.items():
        (base / rel).parent.mkdir(parents=True, exist_ok=True); (base / rel).write_bytes(data if isinstance(data, bytes) else data.encode())
    return base


def ph_cells(raw):
    """Placeholder cells of a `capture-pane -p -e` dump: [(row, x, id, row_diacritic_index, col_diacritic_index)], x = display column, id = the 24-bit value of the
    38;2;r;g;b foreground. A cell that lacks its diacritics (the short form) has index None."""
    out = []
    for r, line in enumerate(raw.split("\n")):
        st, x = dict(fg=None, bg=None, dim=False, bold=False, rev=False), 0
        toks = _TOK.split(line)
        for tok in toks:
            m = _TOK.fullmatch(tok)
            if m:
                _sgr(m.group(1)[2:-1], st); continue
            i = 0
            while i < len(tok):
                ch = tok[i]; i += 1
                if ch == PLACEHOLDER:
                    dia = []
                    while i < len(tok) and unicodedata.combining(tok[i]):
                        dia.append(ord(tok[i])); i += 1
                    f = st["fg"]; idv = (f[1] << 16 | f[2] << 8 | f[3]) if f and f[0] == "rgb" else None
                    ix = lambda k: DIACRITICS.index(dia[k]) if len(dia) > k and dia[k] in DIACRITICS else None
                    out.append((r, x, idv, ix(0), ix(1))); x += 1
                else:
                    x += cw(ch)
    return out


def kitty_commands(data):
    """Kitty graphics APCs found in a raw byte stream written by agmux, tmux passthrough wrappers (\\ePtmux;<ESC doubled>\\e\\\\) undone:
    [{"ctl": {"a": "T", "i": "12632576", ...}, "payload": bytes, "wrapped": bool, "chunk_len": n}] in stream order, plus nothing for non-graphics bytes.
    Chunks of one image (m=1 ... m=0) stay separate entries: callers join them."""
    b = data if isinstance(data, bytes) else data.encode("latin-1")
    out, i = [], 0
    def apc(body, wrapped):
        m = re.fullmatch(rb"\x1b_G([^;]*)(?:;(.*))?\x1b\\", body, re.S)
        if m:
            ctl = dict(kv.split("=", 1) for kv in m.group(1).decode().split(",") if "=" in kv)
            out.append(dict(ctl=ctl, payload=m.group(2) or b"", wrapped=wrapped, raw=body))
    while i < len(b):
        if b.startswith(b"\x1bPtmux;", i):
            j, body = i + 7, bytearray()
            while j < len(b):
                if b[j] == 0x1B and j + 1 < len(b) and b[j + 1] == 0x1B:
                    body.append(0x1B); j += 2
                elif b[j] == 0x1B and j + 1 < len(b) and b[j + 1] == 0x5C:  # ESC \ undoubled = end of the wrapper
                    j += 2; break
                else:
                    body.append(b[j]); j += 1
            apc(bytes(body), True); i = j
        elif b.startswith(b"\x1b_G", i):
            j = b.find(b"\x1b\\", i)
            if j < 0: break
            apc(b[i:j + 2], False); i = j + 2
        else:
            i += 1
    return out


def kitty_images(cmds):
    """Join chunked uploads: {id: dict(ctl=first chunk's keys, data=decoded bytes, chunks=[payload lengths])}; deletions: kitty_deletes(cmds)."""
    import base64
    imgs, cur = {}, None
    for c in cmds:
        ctl = c["ctl"]
        if ctl.get("a") == "d": continue
        if cur is None:
            cur = dict(ctl=ctl, b64=b"", chunks=[])
        cur["b64"] += c["payload"]; cur["chunks"].append(len(c["payload"]))
        if ctl.get("m", "0") == "0":
            cur["data"] = base64.b64decode(cur["b64"]); imgs[int(cur["ctl"]["i"])] = cur; cur = None
    return imgs


def kitty_deletes(cmds):
    return [int(c["ctl"]["i"]) for c in cmds if c["ctl"].get("a") == "d" and "i" in c["ctl"]]


class PtyRun:
    """agmux on a bare pty (no tmux, no terminal emulator): records every byte it writes, so upload / delete sequences can be checked.
    Environment = Env.prefix-style (fake ccmux and fake tmux first on PATH); extra = dict overriding it. Always close()."""

    def __init__(self, env, args="board", w=200, h=50, extra=None, binary=None):
        import fcntl, pty, struct, termios
        e = {"PATH": f"{env.bin}:/usr/bin:/bin", "HOME": str(env.home), "XDG_STATE_HOME": str(env.state), "TERM": "xterm-256color",
             "COLORTERM": "truecolor", "LANG": "en_US.UTF-8", **(extra or {})}
        e = {k: v for k, v in e.items() if v is not None}
        self.data, self._os = bytearray(), os
        pid, fd = pty.fork()
        if pid == 0:
            os.execve(binary or BIN, [binary or BIN, *args.split()], e)
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", h, w, 0, 0))
        self.pid, self.fd, self.status = pid, fd, None
        self._th = __import__("threading").Thread(target=self._pump, daemon=True); self._th.start()

    def _pump(self):
        while True:
            try:
                d = os.read(self.fd, 65536)
            except OSError:
                break
            if not d: break
            self.data += d
            try:  # a terminal answers what termenv / bubbletea ask at start-up; unanswered they wait seconds before going on
                if b"\x1b]11;?" in d: os.write(self.fd, b"\x1b]11;rgb:0000/0000/0000\x1b\\")
                if b"\x1b[6n" in d: os.write(self.fd, b"\x1b[1;1R")
            except OSError:
                break

    def send(self, s):
        os.write(self.fd, s if isinstance(s, bytes) else s.encode())

    def exited(self, timeout=5):
        end = time.time() + timeout
        while time.time() < end:
            r, st = os.waitpid(self.pid, os.WNOHANG)
            if r: self.status = os.waitstatus_to_exitcode(st); self._th.join(1); return True
            time.sleep(0.05)
        return False

    def close(self):
        if self.status is None:
            try: os.kill(self.pid, 9); os.waitpid(self.pid, 0)
            except OSError: pass
        try: os.close(self.fd)
        except OSError: pass


def upload_ids(env, cfg, extra=None):
    """Start agmux on a bare pty as a kitty terminal would see it, let it upload, quit: {png bytes: image id}. Independent of how ids are numbered, so a test can
    say 'this card shows the frame of such-and-such file' (needs the fixture sessions in env: it waits for the first card)."""
    e = {"XDG_CONFIG_HOME": str(cfg), "TERM": "xterm-kitty", **(extra or {})}
    r = PtyRun(env, extra=e)
    try:
        assert wait(lambda: b"alpha-main" in bytes(r.data), 8), bytes(r.data)[-300:]
        r.send("q"); r.exited(5)
        return {v["data"]: i for i, v in kitty_images(kitty_commands(bytes(r.data))).items()}
    finally:
        r.close()
