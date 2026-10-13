"""agmux black-box helpers. All tmux use goes through socket `agmux-test`; never the user's server.

Isolation: each Env has its own HOME / XDG_STATE_HOME / fake `ccmux` on PATH (logs argv, serves show.json).
Runs are serialised by a lock file because the tmux socket name is fixed.
"""
import atexit, datetime, fcntl, json, os, pathlib, re, shlex, shutil, subprocess, tempfile, time, unicodedata, uuid

BIN = os.environ.get("AGMUX_BIN") or os.path.expanduser("~/go/bin/agmux")
SOCK = os.environ.get("AGMUX_TEST_SOCK", "agmux-test")  # convention: agmux-test; override when another session shares the machine
HERE = pathlib.Path(__file__).resolve().parent
SRC = pathlib.Path(os.environ.get("AGMUX_SRC") or HERE.parent.parent)  # source tree of the binary under test (README, built-in avatar sets): set AGMUX_SRC to its worktree; the shared workspace may hold someone's half-done edits

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
    case " $* " in *session_attached*) if [ -e "$d/tmux.attached" ]; then cat "$d/tmux.attached"; else echo 0; fi; exit 0;; esac  # T45: `#{{session_attached}}` while a --stay board is hidden (0 = nobody has attached again; a test writes 1)
    if [ -e "$d/tmux.session" ]; then cat "$d/tmux.session"; else echo fl-own; fi; exit 0;;
  show|show-options) case " $* " in *allow-passthrough*) if [ -e "$d/tmux.passthrough" ]; then cat "$d/tmux.passthrough"; else echo on; fi; exit 0;; esac; exit 0;;  # T28
  list-clients) [ -e "$d/tmux.clients" ] || exit 0  # T38: five fields; a row with three gets the default termname (tmux.termname, xterm-256color) and a tty file of its own in $d/tty-<name>
    term=xterm-256color; [ -e "$d/tmux.termname" ] && term=$(cat "$d/tmux.termname")
    tab=$(printf '\\t'); while IFS="$tab" read -r a n s t y; do [ -z "$a$n$s" ] && continue; [ -z "$t" ] && t=$term; [ -z "$y" ] && {{ if [ -e "$d/tmux.tty" ]; then y=$(cat "$d/tmux.tty"); else y="$d/tty-$(printf %s "$n" | tr '/ ' '__')"; : >> "$y"; fi; }}; printf '%s\\t%s\\t%s\\t%s\\t%s\\n' "$a" "$n" "$s" "$t" "$y"; done < "$d/tmux.clients"; exit 0;;
  load-buffer) cat >> "$d/tmux.stdin"; exit 0;;
  show-environment) f="$d/tmux.env.plain"; case " $* " in *" -h "*) f="$d/tmux.env";; esac  # -h: the hidden variable a launcher sets; without it: the plain session one
    if [ -e "$f" ]; then cat "$f"; exit 0; else echo "unknown variable: AGMUX_CLIENT" >&2; exit 1; fi;;
esac
exit 0
"""
        (self.bin / "tmux").write_text(tmux)
        (self.bin / "tmux").chmod(0o755)
        # PATH for the REAL-tmux tests: the fake ccmux, but not the fake tmux
        self.bin_real = self.tmp / "bin_real"; self.bin_real.mkdir()
        os.symlink(self.bin / "ccmux", self.bin_real / "ccmux")

    def cleanup(self):
        if getattr(self, "pty_tty", None) is not None: self.pty_tty.close()
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

    def set_tmux(self, session="fl-own", clients=(), fail=None, session_env=None, session_env_plain=None):
        """clients: (activity, client_name, client_session[, termname[, tty]]) rows served by `tmux list-clients -F ...` (T38: termname defaults to set_term's, tty to a file
        $fake/tty-<name> that tty_bytes() reads); fail: subcommand that exits 1; session_env / session_env_plain: what `show-environment -h` / without -h print for AGMUX_CLIENT."""
        (self.fake / "tmux.session").write_text(session + "\n")  # NB: tmux keeps trailing spaces in names, so the file keeps them too
        (self.fake / "tmux.clients").write_text("".join("\t".join(str(x) for x in c) + "\n" for c in clients))
        if session_env_plain is not None: (self.fake / "tmux.env.plain").write_text(session_env_plain + "\n")
        if fail:
            (self.fake / "tmux.fail").write_text(fail)
        if session_env is not None:  # what `tmux show-environment -t <session> AGMUX_CLIENT` prints, e.g. "AGMUX_CLIENT=/dev/outer" or "-AGMUX_CLIENT"
            (self.fake / "tmux.env").write_text(session_env + "\n")

    def use_pty_tty(self):
        """T38: make the fake tmux's clients (those that give no tty of their own) have a real pty as their tty, /dev/ttysNNN: agmux writes pictures there; tty_bytes() returns what it wrote."""
        if getattr(self, "pty_tty", None) is None:
            self.pty_tty = PtyTty(); (self.fake / "tmux.tty").write_text(self.pty_tty.path + "\n")
        return self.pty_tty

    def tty_bytes(self, name="c-test"):
        """what agmux wrote to the tty of fake tmux client `name` (T38: pictures go straight to the client's tty); with use_pty_tty: what the pty received"""
        if getattr(self, "pty_tty", None) is not None:
            return self.pty_tty.bytes()
        p = self.fake / ("tty-" + name.replace("/", "_").replace(" ", "_"))
        return p.read_bytes() if p.exists() else b""

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
           gitBranch="feature/qa-acceptance-very-long-twig-name-for-truncation", logPath=str(jsonl),
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


MIN_COL, GAP, MAX_COLS = 40, 1, 4  # S1.1: column minimum 40; T8: at most 4 columns; T39: one cell between columns (was 2)
AV_X, PAD = 1, 0  # T39: the avatar starts right after the border (card cell 1); no padding between the border and the text (was 2 and 1)


def layout(W):
    n = max(1, min(MAX_COLS, (W + GAP) // (MIN_COL + GAP)))
    return n, (W - GAP * (n - 1)) // n


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


def png(rgb=(200, 60, 60), size=8, noise=False, height=None):
    """A tiny valid PNG (solid colour); different colours make different bytes, so frames are tellable apart."""
    import struct, zlib
    def chunk(t, d):
        c = struct.pack(">I", len(d)) + t + d
        return c + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    import random
    rnd = random.Random(sum(rgb))  # noise=True: incompressible pixels, to get an upload that needs several 4096-byte chunks
    height = height or size  # size = width in pixels
    raw = b"".join(b"\x00" + (bytes(rnd.randrange(256) for _ in range(size * 3)) if noise else bytes(rgb) * size) for _ in range(height))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", size, height, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")


def write_avatar_set(cfg, name, states, **files):
    """$XDG_CONFIG_HOME/agmux/avatars/<name>/<state>/NN.png: states = {"idle": 3, "working": 2, ...} (frame counts; frame k of state s has a colour of its own).
    files = extra raw files {"working/readme.txt": b"..."}. cfg = the XDG_CONFIG_HOME directory. Returns the set directory."""
    base = pathlib.Path(cfg) / "agmux" / "avatars" / name
    for si, (st, n) in enumerate(states.items()):
        (base / st).mkdir(parents=True, exist_ok=True)
        for k in range(n):
            (base / st / f"{k + 1:02d}.png").write_bytes(png(((40 + 31 * si) % 256, (30 + 40 * k) % 256, (200 - 30 * k) % 256)))
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
    """Start agmux on a bare pty as a kitty terminal would see it, let it upload, quit: {source file bytes: image id}. Looks frames up by what is on disk (since T31 the
    uploaded bytes are a crop of the file: see frame_table), so a test can say 'this card shows the frame of such-and-such file'. Needs the fixture sessions in env."""
    table, _ = frame_table(env, cfg, extra)
    roots = {s: (src, d) for s, src, d in [(n, "builtin", None) for n in ()]}
    out = {}
    for (sid, dirname, fname), i in table.items():
        user = pathlib.Path(cfg) / "agmux" / "avatars" / sid / dirname / fname
        p = user if user.exists() else SRC / "internal" / "avatar" / "sets" / sid / dirname / fname
        out[p.read_bytes()] = i
    return out


# ---------------------------------------------------------------- T31 / T33: which uploaded image is which frame
import re as _re
_TAKE = _re.compile(r"_v\d+\.png$")  # alternate takes (idle_v2.png): not frames. idle_2.png is one.
OLD_DIRS = {"think": "working", "hi": "attention", "success": "done"}


def scan_set(d):
    """Frames agmux finds in one set directory: [(dir, file)] in upload order (states idle, think, read, write, tool, hi, success; a state's old-name directory only when
    the new one has no frame), or None when the set has no idle frame (skipped)."""
    d = pathlib.Path(d); out = []
    for st in AVATAR_STATES:
        for dirname in [st] + ([OLD_DIRS[st]] if st in OLD_DIRS else []):
            p = d / dirname
            files = sorted(f.name for f in p.iterdir() if f.is_file() and f.name.endswith(".png") and not f.name.startswith(".") and not _TAKE.search(f.name)) if p.is_dir() else []
            if files:
                out += [(dirname, f) for f in files]; break
    return out if any(x[0] == "idle" for x in out) else None


def set_list(cfg=None, src=None):
    """[(set id, source, [(dir, file)])] in load order: the built-in sets (SRC tree, sorted), then the user's in `cfg`/agmux/avatars (name order); a user set replaces a built-in
    one of the same name in place; names starting with . or named hash are not sets; sets without idle frames are skipped."""
    out = []
    for root, source in ((pathlib.Path(src or SRC) / "internal" / "avatar" / "sets", "builtin"), (pathlib.Path(cfg or "/nonexistent") / "agmux" / "avatars", "user")):
        if not root.is_dir(): continue
        for d in sorted(root.iterdir()):
            if d.name.startswith(".") or d.name == "hash" or not d.is_dir(): continue
            fr = scan_set(d)
            if fr is None: continue
            i = next((k for k, s in enumerate(out) if s[0] == d.name), None)
            if i is None: out.append((d.name, source, fr))
            else: out[i] = (d.name, source, fr)
    return out


def frame_table(env, cfg, extra=None):
    """{(set id, directory, file name): image id}: what agmux uploaded, matched to files by upload order (sets in load order, frames in state / file-name order). Asserts the number
    of uploaded images equals the number of frames found on disk (so a skipped bad PNG makes it fail loudly: handle those sets in the test)."""
    e = {"XDG_CONFIG_HOME": str(cfg), "TERM": "xterm-kitty", **(extra or {})}
    r = PtyRun(env, extra=e)
    try:
        assert wait(lambda: b" sessions" in bytes(r.data), 8), bytes(r.data)[-300:]
        r.send("q"); r.exited(5); imgs = kitty_images(kitty_commands(bytes(r.data)))
    finally:
        r.close()
    keys = [(s, d, f) for s, _, fr in set_list(cfg) for d, f in fr]
    assert len(keys) == len(imgs), f"{len(imgs)} images uploaded, {len(keys)} frames on disk"
    return dict(zip(keys, imgs)), imgs  # imgs: {id: dict(ctl, data, chunks)} in upload order


def decode_png(data):
    """(width, height, rows) of an 8-bit RGB / RGBA PNG; rows = list of bytes (channels interleaved). Enough for agmux's own re-encoded crops (Go writes NRGBA / RGBA)."""
    import struct, zlib
    assert data[:8] == b"\x89PNG\r\n\x1a\n", "not a PNG"
    i, idat, w = 8, b"", None
    while i < len(data):
        n = struct.unpack(">I", data[i:i + 4])[0]; typ = data[i + 4:i + 8]; body = data[i + 8:i + 8 + n]
        if typ == b"IHDR": w, h, depth, ctype, _, _, inter = struct.unpack(">IIBBBBB", body); assert depth == 8 and ctype in (2, 6) and inter == 0, (depth, ctype, inter)
        elif typ == b"IDAT": idat += body
        i += 12 + n
    ch = 3 if ctype == 2 else 4; raw = zlib.decompress(idat); stride = w * ch; rows, prev = [], bytearray(stride)
    for y in range(h):
        f = raw[y * (stride + 1)]; line = bytearray(raw[y * (stride + 1) + 1:(y + 1) * (stride + 1)])
        for x in range(stride):
            a = line[x - ch] if x >= ch else 0; b = prev[x]; c = prev[x - ch] if x >= ch else 0
            if f == 1: line[x] = (line[x] + a) & 255
            elif f == 2: line[x] = (line[x] + b) & 255
            elif f == 3: line[x] = (line[x] + (a + b) // 2) & 255
            elif f == 4:
                p = a + b - c; pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                line[x] = (line[x] + (a if pa <= pb and pa <= pc else b if pb <= pc else c)) & 255
        rows.append(bytes(line)); prev = line
    return w, h, rows, ch


def expected_upload_size(w, h):
    """T31: the size of the uploaded frame for a w x h file: its centred 3:2 part (sizes round down), shrunk to at most 128 high; (w, h) itself when that changes nothing."""
    if w * 2 > h * 3: cw, ch = max(h * 3 // 2, 1), h
    else: cw, ch = w, max(w * 2 // 3, 1)
    if ch > 128: return max(cw * 128 // ch, 1), 128
    return cw, ch


# ---------------------------------------------------------------- T30-T32 fixtures
def png_pixels(w, h, fn):
    """An 8-bit RGB PNG whose pixel (x, y) is fn(x, y) = (r, g, b): lets a test tell which part of the picture ended up in a crop."""
    import struct, zlib
    def chunk(t, d):
        c = struct.pack(">I", len(d)) + t + d
        return c + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    raw = b"".join(b"\x00" + b"".join(bytes(fn(x, y)) for x in range(w)) for y in range(h))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(raw, 1)) + chunk(b"IEND", b"")


BAD_PNG = b"\x89PNG\r\n\x1a\nnot really"  # a .png whose header cannot be read: its size is unknown
AVATAR_STATES = ("idle", "think", "read", "write", "tool", "hi", "success")  # pi-emote's state directories (T30); old names working / attention / done are aliases


def state_set(cfg, name, frames=None, **files):
    """A user image set with every T30 state directory: idle has 3 frames (the first is the calm face, the others blink), the others one each; frames overrides
    ({"idle": 2, "read": 3, ...}; a state with 0 gets no directory)."""
    n = {"idle": 3, **{s: 1 for s in AVATAR_STATES[1:]}, **(frames or {})}
    return write_avatar_set(cfg, name, {s: k for s, k in n.items() if k}, **files)


# every session of build_states_fixture: name -> (avatar state it must show, status, pendingTool, attentionType, attentionState, subagents)
STATE_CASES = {
    "s-idle": ("idle", "idle", None, None, None, []),
    "s-think": ("think", "working", None, None, None, []),
    "s-agents": ("think", "idle", None, None, None, [{"agentId": "y1", "status": "working", "pendingTool": None}]),  # raw idle, a subagent works -> `⏵ working · agents`
    "s-read": ("read", "working", "Read", None, None, []), "s-grep": ("read", "working", "Grep", None, None, []), "s-glob": ("read", "working", "Glob", None, None, []),
    "s-webfetch": ("read", "working", "WebFetch", None, None, []), "s-websearch": ("read", "working", "WebSearch", None, None, []),
    "s-write": ("write", "working", "Write", None, None, []), "s-edit": ("write", "working", "Edit", None, None, []),
    "s-multiedit": ("write", "working", "MultiEdit", None, None, []), "s-notebook": ("write", "working", "NotebookEdit", None, None, []),
    "s-bash": ("tool", "working", "Bash", None, None, []), "s-mcp": ("tool", "working", "mcp__srv__do", None, None, []),
    "s-attention": ("hi", "working", "Bash", "permission", None, []),  # needs you even though a tool is pending
    "s-waiting": ("hi", "waiting", None, None, None, []),
    "s-done": ("success", "idle", None, None, "unread", []),
}


def build_states_fixture(env, cases=None):
    """One session per STATE_CASES entry (17), five per project group (so 4 columns at 200 wide, ~25 rows: use h >= 30), fake panes %201.. .
    Returns {name: expected avatar state}. Replaces whatever show.json held; with_hud sessions are not needed here."""
    cases = cases or STATE_CASES
    now = datetime.datetime.now(datetime.timezone.utc); h = env.home; out, S = {}, []
    for i, (name, (state, status, tool, att, attstate, subs)) in enumerate(cases.items()):
        g = f"g{i // 5}"
        S.append(dict(id=f"s{i:02d}", summary=name, agentType="claude", cwd=f"{h}/work/{g}", project=g, mainRepoRoot=f"{h}/work/{g}", gitBranch="main", status=status,
                      attentionType=att, pendingTool=tool, tmuxPane=f"%{201 + i}", trackingMode="native", subagents=subs, nativeSessionId=f"n-s{i:02d}", logPath=None,
                      lastActivityAt=iso(now - datetime.timedelta(seconds=90)), updatedAt=iso(now), backgroundDetail=None, lastPrompt=None, attentionState=attstate))
        out[name] = state
    env.write_show(S); env.pane_ids = {s["tmuxPane"]: s["id"] for s in S}
    return out


GROUP_ROWS = [  # T43: name, project, cwd (relative to ~/work), branch, tmuxTarget (None = background, no pane), (status, attentionState)
    ("shop-main", "shopcart", "shopcart", "main", "alpha # main:1.0", ("working", None)),
    ("shop-hf", "shopcart", "shopcart.worktrees/hotfix/alert-group", "hotfix/alert-group", "alpha # hotfix/alert-group:2.0", ("waiting", None)),
    ("shop-hf2", "shopcart", "shopcart.worktrees/hotfix/alert-group", "hotfix/alert-group", "beta # hotfix/alert-group:1.0", ("idle", "unread")),
    ("shop-feat", "shopcart", "shopcart.worktrees/feature/permission-dashboard", "feature/permission-dashboard", "beta # feature/permission-dashboard:1.1", ("idle", None)),
    ("sql-a", "sqlmux", "sqlmux", "main", "gamma sess # main:1.0", ("working", None)),
    ("sql-b", "sqlmux", "sqlmux", "main", "alpha # main:1.1", ("working", None)),
    ("sql-c", "sqlmux", "sqlmux", "main", "gamma sess # w:0.0", ("waiting", None)),
    ("solo-bg", "solo", "solo", "main", None, ("idle", None)),
]


def build_group_fixture(env):
    """T43 fixture: 8 sessions in 3 projects (shopcart = main repo + two worktrees = 3 cwds; 5 cwds in all), tmux sessions alpha / beta / `gamma sess` (a space in the name; window names with `/` and `:`),
    one background session without a pane, and all four states (done = idle + unread). Panes %401.. . Returns the expected membership per group_by:
    {mode: {title: [names sorted]}} for cwd (titles `~/work/...`), project, session, state; plus `order` = {mode: [titles in the expected on-screen order]} (alphabetical; `(no pane)` last; state fixed) and `sessions`."""
    now = datetime.datetime.now(datetime.timezone.utc); h = env.home; S = []
    for i, (name, proj, cwd, br, target, (status, att)) in enumerate(GROUP_ROWS):
        wt = "worktrees" in cwd
        S.append(dict(id=f"g{i:02d}", summary=name, agentType="claude", cwd=f"{h}/work/{cwd}", project=proj, mainRepoRoot=f"{h}/work/{proj}", isWorktree=wt, worktreeRoot=f"{h}/work/{cwd}" if wt else None,
                      gitBranch=br, status=status, attentionType=None, pendingTool=None, tmuxPane=f"%{401 + i}" if target else None, tmuxTarget=target, trackingMode="native" if target else "background",
                      subagents=[], nativeSessionId=f"n-g{i:02d}", logPath=None, lastActivityAt=iso(now - datetime.timedelta(seconds=90)), updatedAt=iso(now), backgroundDetail=None,
                      lastPrompt=None, attentionState=att))
    env.write_show(S); env.pane_ids = {s["tmuxPane"]: s["id"] for s in S if s["tmuxPane"]}
    state_of = lambda r: "done" if r[5] == ("idle", "unread") else r[5][0]
    by = {"cwd": {}, "project": {}, "session": {}, "state": {}}
    for r in GROUP_ROWS:
        for mode, key in (("cwd", f"~/work/{r[2]}"), ("project", r[1]), ("session", r[4].split(" # ")[0] if r[4] else "(no pane)"), ("state", state_of(r))):
            by[mode].setdefault(key, []).append(r[0])
    order = {m: sorted(d) for m, d in by.items()}
    order["session"] = sorted(k for k in by["session"] if k != "(no pane)") + ["(no pane)"]  # orchestrator: `(no pane)` is always last
    order["state"] = ["working", "waiting", "done", "idle"]  # fixed order, not by name
    return dict(sessions=S, groups={m: {k: sorted(v) for k, v in d.items()} for m, d in by.items()}, order=order)


def sample_series(fn, secs, step=0.05):
    """[(t, fn())] for secs seconds: fn reads the screen and returns what to track (an image id, a status word...)."""
    t0, out = time.time(), []
    while time.time() - t0 < secs:
        out.append((time.time() - t0, fn())); time.sleep(step)
    return out


FALLBACK = {"read": "think", "write": "think", "tool": "think", "think": "idle", "hi": "idle", "success": "idle"}  # T30: a state without frames shows the next one on its chain
OLD_NAMES = {"think": "working", "hi": "attention", "success": "done"}  # old directory names, still accepted (the new name wins when both exist)


def frames_for(frames, state):
    """frames = {state: [ids or anything]} of a set; the frames a card in `state` shows: its own, else along FALLBACK, ending at idle."""
    while not frames.get(state) and state != "idle":
        state = FALLBACK[state]
    return frames[state], state


def set_lines(cfg=None):
    """The `agmux avatars` lines expected for the image sets (built-in ones, then the user's in cfg), computed from the files: `id  source  idle 2 · think 2 · ...` (an old directory name counts under its new one)."""
    inv = {v: k for k, v in OLD_DIRS.items()}; out = []
    for sid, source, fr in set_list(cfg):
        n = {}
        for d, f in fr: n[inv.get(d, d)] = n.get(inv.get(d, d), 0) + 1
        out.append(f"{sid}  {source}  " + " · ".join(f"{s} {n.get(s, 0)}" for s in AVATAR_STATES))
    return out


# ---------------------------------------------------------------- T34 / T35 fixtures
def build_many_fixture(env, n=22, per_group=6, sizes=None):
    """n sessions named m00.. in groups of per_group (project p0, p1, ...): more cards than a 120x30 screen shows, to scroll. Panes %301.. . Returns [names] in order.
    sizes=[6, 2, 3, 5]: group sizes of their own (n is then their sum)."""
    now = datetime.datetime.now(datetime.timezone.utc); h = env.home; S = []
    if sizes: n = sum(sizes); grp = [k for k, s in enumerate(sizes) for _ in range(s)]
    for i in range(n):
        g = f"p{grp[i] if sizes else i // per_group}"
        S.append(dict(id=f"m{i:02d}", summary=f"m{i:02d}", agentType="claude", cwd=f"{h}/work/{g}", project=g, mainRepoRoot=f"{h}/work/{g}", gitBranch="main", status="idle",
                      attentionType=None, pendingTool=None, tmuxPane=f"%{301 + i}", trackingMode="native", subagents=[], nativeSessionId=f"n-m{i:02d}", logPath=None,
                      lastActivityAt=iso(now - datetime.timedelta(seconds=90)), updatedAt=iso(now), backgroundDetail=None, lastPrompt=None, attentionState=None))
    env.write_show(S); env.pane_ids = {s["tmuxPane"]: s["id"] for s in S}
    return [s["summary"] for s in S]


SHEET_OUT = (255, 255, 255)  # outside the dark grid a sheet is light (pi's cut() finds the grid where the dark background ends)


def sheet_colour(r, c):
    """A colour of its own for grid cell (row r, column c): dark by pi's test (mean <= 200), all different for up to 6 x 5 cells."""
    return (80 + 30 * c, 90 + 25 * r, 200 - 20 * c - 10 * r)


def sheet_png(cols=6, rows=5, cw=180, ch=150, pad=40, edge=12, blank_from=None):
    """A synthetic portrait sheet like draw-portrait's cut() expects: the dark grid (cols x rows cells of cw x ch) in the top left corner, `pad` light pixels to its right and below.
    Cell (r, c) is solid sheet_colour(r, c) except `edge` pixels at its left and right sides, one step brighter (outside the centred 97% square when cw > ch: a correct cut never
    contains them). Cells with index >= blank_from (row-major) are light, like the empty cells at the end of a 19-cell sheet. Returns (png bytes, width, height)."""
    w, h = cols * cw + pad, rows * ch + pad
    def px(x, y):
        if x >= cols * cw or y >= rows * ch: return SHEET_OUT
        c, r = x // cw, y // ch
        if blank_from is not None and r * cols + c >= blank_from: return SHEET_OUT
        col = sheet_colour(r, c); ix = x % cw
        return tuple(v + 1 for v in col) if ix < edge or ix >= cw - edge else col
    return png_pixels(w, h, px), w, h


# the 30 cells of a pi-agent-portrait sheet, left to right, top to bottom (scripts/draw-portrait FRAMES + NEW_FRAMES); `--layout 19` = the first 19 on a 5 x 4 grid
PI_FRAMES = ["idle/idle.png", "idle/idle_blink.png", "hi/hi1.png", "hi/hi2.png", "talk/talk_close.png", "talk/talk_small.png", "talk/talk_mid.png", "talk/talk_wide.png",
             "think/think.png", "think/think_hard.png", "read/read1.png", "read/read2.png", "write/write1.png", "write/write2.png", "tool/tool1.png", "tool/tool2.png",
             "failure/failure1.png", "failure/failure2.png", "compact/compact1.png", "success/success1.png", "success/success2.png", "sleep/sleep1.png", "sleep/sleep2.png",
             "wait/wait1.png", "wait/wait2.png", "interrupted/interrupted1.png", "search/search1.png", "bash/bash1.png", "error/error1.png", "heard/heard1.png"]


class PtyTty:
    """A pty standing in for a terminal's tty: `path` (/dev/ttysNNN, what a tmux client's #{client_tty} says) can be opened and written by agmux; everything written is read here."""

    def __init__(self, read=True, chunk=65536, delay=0.0):
        """read=False: nobody reads (a terminal that stopped); chunk / delay: read `chunk` bytes then wait `delay` seconds (a slow terminal)."""
        import pty, threading, tty as _tty
        self.read, self.chunk, self.delay = read, chunk, delay
        self.master, slave = pty.openpty(); _tty.setraw(slave)  # raw: no echo / newline translation: the bytes come out as written
        self.path = os.ttyname(slave); self._slave = slave; self._data = bytearray(); self._lock = threading.Lock(); self._stop = False
        self._th = threading.Thread(target=self._pump, daemon=True); self._th.start()

    def _pump(self):
        import select
        while not self._stop:
            if not self.read: time.sleep(0.1); continue
            try:
                if not select.select([self.master], [], [], 0.2)[0]: continue
                d = os.read(self.master, self.chunk)
            except OSError: break
            if not d: break
            with self._lock: self._data += d
            if self.delay: time.sleep(self.delay)

    def bytes(self):
        time.sleep(0.05)
        with self._lock: return bytes(self._data)

    def close(self):
        self._stop = True; self._th.join(2)
        for fd in (self.master, self._slave):
            try: os.close(fd)
            except OSError: pass
