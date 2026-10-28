"""T78 fixtures for `agmux recall` / `agmux -r` (M6): transcripts in the real Claude Code / pi formats under a throw-away $HOME, a cache dir of its own, and
fake `claude` / `pi` / `pbcopy` / `$EDITOR` scripts that record what they were run with (argv, cwd, stdin) instead of doing anything.

Nothing here touches the user's ~/.claude, ~/.pi, ~/.cache or tmux: the Env's HOME is a temp dir, XDG_CACHE_HOME points into it, and PATH holds only the fakes (+ the Env's fake tmux on request).
Transcript lines are modelled on internal/recall/testdata (anonymised copies of the real formats): Claude `type` user / assistant / custom-title / ai-title / agent-name, pi `session` / `message` / `session_info`."""
import json, os, re, shlex, subprocess, time, uuid
from collections import namedtuple
from lib import Env, Term, BIN, tm

COLS = ("source", "id", "name", "project", "branch", "mtime", "hits", "snippet")  # `agmux recall --dump`: tab separated, one session per line (T75)
Row = namedtuple("Row", COLS)
NOW = int(time.time()) // 60 * 60


def dumps(d):
    """compact like Claude Code and pi write it (the readers look for `\"type\":\"user\"` without spaces)"""
    return json.dumps(d, ensure_ascii=False, separators=(",", ":"))


def when(minutes_ago):
    """a whole-second mtime `minutes_ago` minutes before the fixture's now: the dump prints mtime as RFC 3339 UTC, so seconds are all that is compared"""
    return NOW - minutes_ago * 60


def rfc3339(sec):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(sec))


def _ts(sec):
    return time.strftime("%Y-%m-%dT%H:%M:%S.000Z", time.gmtime(sec))


# ---------------------------------------------------------------- transcripts
def claude_lines(sid, cwd, branch, turns, t0=None, title=None, ai_title=None, agent=None):
    """Claude Code transcript lines (a list of JSON strings). turns: (role, text) with role 'u' / 'a' for what the user / the agent said, or ('raw', dict) for a hand-made line.
    Around the sentences: a hook attachment, a thinking block, a tool call and its result, like a real one."""
    t0 = t0 or when(600); n = [0]

    def base(typ, **kw):
        n[0] += 1
        d = {"parentUuid": "u0", "isSidechain": False, "type": typ, "uuid": f"x{n[0]}", "timestamp": _ts(t0 + n[0]), "userType": "external", "entrypoint": "cli", "cwd": cwd, "sessionId": sid, "version": "2.1.0", "gitBranch": branch}
        d.update(kw); return dumps(d)
    out = [dumps({"type": "agent-setting", "agentSetting": "dev", "sessionId": sid}), base("attachment", attachment={"type": "hook"}, rendered=[])]
    for role, text in turns:
        if role == "raw": out.append(dumps(text))
        elif role == "u": out.append(base("user", message={"role": "user", "content": text}, permissionMode="default"))
        else:
            out.append(base("assistant", message={"role": "assistant", "content": [{"type": "thinking", "thinking": "hmm", "signature": "s"}]}))
            out.append(base("assistant", message={"role": "assistant", "content": [{"type": "text", "text": text}]}))
            out.append(base("assistant", message={"role": "assistant", "content": [{"type": "tool_use", "id": "c1", "name": "Bash", "input": {"command": "ls"}}]}))
            out.append(base("user", message={"role": "user", "content": [{"type": "tool_result", "tool_use_id": "c1", "content": f"tool output that must never be found: {sid}"}]}))
    if title: out.append(dumps({"type": "custom-title", "customTitle": title, "sessionId": sid}))
    if ai_title: out.append(dumps({"type": "ai-title", "aiTitle": ai_title, "sessionId": sid}))
    if agent: out.append(dumps({"type": "agent-name", "agentName": agent, "sessionId": sid}))
    return out


def pi_lines(sid, cwd, turns, t0=None, name=None):
    """pi session lines: the header, model change, then user / assistant messages (assistant with a thinking block + a text block, a tool call and its result after it), `session_info` last"""
    t0 = t0 or when(600); n = [0]

    def line(typ, **kw):
        n[0] += 1
        d = {"type": typ, "id": f"p{n[0]}", "parentId": f"p{n[0] - 1}", "timestamp": _ts(t0 + n[0])}
        d.update(kw); return dumps(d)
    out = [dumps({"type": "session", "version": 3, "id": sid, "timestamp": _ts(t0), "cwd": cwd}), line("model_change", provider="p", modelId="m")]
    for role, text in turns:
        if role == "raw": out.append(dumps(text))
        elif role == "u": out.append(line("message", message={"role": "user", "content": [{"type": "text", "text": text}], "timestamp": 1}))
        else:
            out.append(line("message", message={"role": "assistant", "content": [{"type": "thinking", "thinking": "x"}, {"type": "text", "text": text}]}))
            out.append(line("message", message={"role": "assistant", "content": [{"type": "toolCall", "id": "c1", "name": "bash", "arguments": {"command": "ls"}}]}))
            out.append(line("message", message={"role": "toolResult", "toolCallId": "c1", "content": [{"type": "text", "text": f"tool output that must never be found: {sid}"}]}))
    if name: out.append(line("session_info", name=name))
    return out


class RecallEnv(Env):
    """Env + a Claude / pi tree to put transcripts in, a cache dir, a PATH of fakes and `run()` for the agmux under test."""

    def __init__(self):
        super().__init__()
        self.cache, self.rec, self.rbin, self.cfg = (self.tmp / n for n in ("cache", "rec", "rbin", "cfg"))
        for d in (self.cache, self.rec, self.rbin, self.cfg / "agmux"): d.mkdir(parents=True)
        self.pi = self.home / ".pi" / "agent" / "sessions"; self.pi.mkdir(parents=True)
        self.recall_dir = self.cache / "agmux" / "recall"
        self.editor = self.rbin / "fakeedit"; self._fakes()

    # ---- the fakes: each run writes <rec>/<name>.<n> = cwd, then argv one per line, then (stdin of the clipboard ones)
    def _fakes(self):
        d = shlex.quote(str(self.rec))
        for name in ("claude", "pi", "fakeedit", "pbcopy", "xclip", "wl-copy"):
            stdin = 'cat > "$f.stdin"' if name in ("pbcopy", "xclip", "wl-copy") else ""
            marker = "" if stdin else f"printf 'FAKE-{'EDITOR' if name == 'fakeedit' else name.upper()} %s\\n' \"$*\""
            hold = 'while [ -e ' + d + '/hold ]; do sleep 0.1; done' if name == "fakeedit" else ""
            body = f"""#!/bin/sh
f={d}/{name}.$(perl -MTime::HiRes=time -e 'printf "%.6f", time').$$
{{ pwd; for a in "$@"; do printf '%s\\n' "$a"; done; }} > "$f"
{stdin}
{marker}
{hold}
exit 0
"""
            (self.rbin / name).write_text(body); (self.rbin / name).chmod(0o755)

    def ran(self, name):
        """what the fake `name` was run with so far, oldest first: [{"cwd": ..., "argv": [...], "stdin": ... (clipboards)}]"""
        out = []
        for f in sorted(self.rec.glob(f"{name}.*")):
            if f.name.endswith(".stdin"): continue
            lines = f.read_text().split("\n"); ent = {"cwd": lines[0], "argv": lines[1:-1]}
            if (f.parent / (f.name + ".stdin")).exists(): ent["stdin"] = (f.parent / (f.name + ".stdin")).read_text()
            out.append(ent)
        return out

    # ---- transcripts
    def claude_file(self, cwd, sid, lines, age=60):
        """~/.claude/projects/<cwd as Claude names it>/<sid>.jsonl with the mtime `age` minutes ago; returns the path"""
        p = self.claude / "projects" / re.sub(r"[^A-Za-z0-9]", "-", cwd) / f"{sid}.jsonl"; return self._put(p, lines, age)

    def pi_file(self, cwd, sid, lines, age=60, stamp="2026-10-01T08-00-00-000Z"):
        """~/.pi/agent/sessions/<cwd as pi names it>/<stamp>_<sid>.jsonl"""
        p = self.pi / ("--" + cwd.strip("/").replace("/", "-") + "--") / f"{stamp}_{sid}.jsonl"; return self._put(p, lines, age)

    def _put(self, p, lines, age):
        p.parent.mkdir(parents=True, exist_ok=True); p.write_text("\n".join(lines) + "\n"); os.utime(p, (when(age), when(age))); return p

    def touch(self, p, age):
        os.utime(p, (when(age), when(age)))

    def append(self, p, lines, age=None, newline=True):
        """append lines to a transcript the way a running agent does (and give it a newer mtime: `age` minutes ago, default 1)"""
        with open(p, "a") as f: f.write("\n".join(lines) + ("\n" if newline else ""))
        t = when(1 if age is None else age); os.utime(p, (t, t))

    def repo(self, name):
        """a git repository ~/work/<name> (project = its name) and a function that adds a worktree of it: wt('feature/x') -> ~/work/<name>.worktrees/feature/x (the project is still <name>)"""
        root = str(self.home / "work" / name); self.git_repo(root)

        def wt(branch):
            p = str(self.home / "work" / f"{name}.worktrees" / branch)
            if not os.path.exists(p):
                os.makedirs(os.path.dirname(p), exist_ok=True); self._git("worktree", "add", "-q", "-b", branch, p, cwd=root)
            return p
        return root, wt

    # ---- the agmux under test
    def environ(self, tmux=False, **extra):
        path = f"{self.rbin}:{self.bin}:/usr/bin:/bin" if tmux else f"{self.rbin}:/usr/bin:/bin"
        e = {"PATH": path, "HOME": str(self.home), "XDG_CACHE_HOME": str(self.cache), "XDG_STATE_HOME": str(self.state), "XDG_CONFIG_HOME": str(self.cfg),
             "TERM": "xterm-256color", "COLORTERM": "truecolor", "LANG": "en_US.UTF-8", "EDITOR": str(self.editor)}
        e.update(extra); return {k: v for k, v in e.items() if v is not None}

    def run(self, *args, timeout=30, **extra):
        return subprocess.run([BIN, *args], env=self.environ(**extra), capture_output=True, text=True, timeout=timeout, stdin=subprocess.DEVNULL)

    def dump(self, *query, flags=("--dump",), **extra):
        """`agmux recall --dump [flags] [--] query...`: (rows as Row, CompletedProcess); the output must be one Row of eight tab separated fields per line"""
        r = self.run("recall", *flags, *(("--",) if query and query[0].startswith("-") else ()), *query, **extra)
        rows = []
        for l in r.stdout.splitlines():
            f = l.split("\t"); assert len(f) == len(COLS), f"not {len(COLS)} fields: {l!r}"; rows.append(Row(*f))
        return rows, r

    def cache_files(self):
        return sorted(p.name for p in self.recall_dir.glob("*")) if self.recall_dir.exists() else []

    def cache_json(self, name):
        return json.loads((self.recall_dir / name).read_text())

    def put_cache(self, name, d):
        (self.recall_dir / name).write_text(dumps(d))


def corpus(env):
    """The standard fixture: 3 Claude + 3 pi sessions in different directories (a plain repo, a worktree, a directory that is gone), with and without names, with the noise a real
    transcript has. Returns dict key -> (path, session id). Ages (minutes): cart 10 < audit 20 < pi audit 30 < gone 40 < pi plain 50 < hush 60 (noise only: no sentence)."""
    shop, _ = env.repo("shopcart"); ledger, wt = env.repo("ledger"); api = wt("feature/audit/api"); gone = str(env.home / "work" / "vanished")
    out = {}
    sid = "3f6c1a52-0b7e-4c1d-9a55-6d2e8b1f7c01"
    out["cart"] = (env.claude_file(shop, sid, claude_lines(sid, shop, "feat/cart", [
        ("u", "cart total is wrong when a coupon is applied"), ("a", "The coupon is subtracted before tax. Let me look at\n  pricing.go."),
        ("u", "[Image #1] see the screenshot, the total is 12.50 instead of 11.00"), ("a", "Thanks, the rounding happens per line."),
        ("u", "now add a regression test for the coupon order")], title="cart-fix", ai_title="Fix cart total with coupons"), age=10), sid)
    sid = "8d2b7e90-5a41-4f0e-b6c3-1a9d0e4c2f12"
    out["audit"] = (env.claude_file(api, sid, claude_lines(sid, api, "feature/audit/api", [
        ("u", "export the audit log as csv"), ("a", "I will add an exporter next to the json one.")], ai_title="Audit log export"), age=20), sid)
    sid = "c0ffee00-1111-4222-8333-444455556666"
    out["gone"] = (env.claude_file(gone, sid, claude_lines(sid, gone, "main", [
        ("u", "rename the vanished helper before the directory goes"), ("a", "Renamed it to helper_v2 everywhere.")]), age=40), sid)
    sid = "feed0000-aaaa-4bbb-8ccc-ddddeeeeffff"
    out["hush"] = (env.claude_file(shop, sid, claude_lines(sid, shop, "main", [
        ("u", "<command-name>/clear</command-name>\n<command-message>clear</command-message>"), ("u", "ok"), ("u", "<system-reminder>noise-token</system-reminder>")]), age=60), sid)
    sid = "019e0000-aaaa-7bbb-8ccc-000000000001"
    out["pi_audit"] = (env.pi_file(api, sid, pi_lines(sid, api, [
        ("u", "why does the audit export skip rows with a null owner"), ("a", "The export joins owners with an inner join, so null owners drop out."),
        ("u", "switch to a left join and add a test")], name="audit-owner-join"), age=30), sid)
    sid = "019e0000-bbbb-7ccc-8ddd-000000000002"
    out["pi_plain"] = (env.pi_file(shop, sid, pi_lines(sid, shop, [("u", "summarise the checkout flow for the new hire"), ("a", "Checkout runs cart, address, payment, confirmation.")]), age=50), sid)
    return out


def ui_ready():
    """T76 delivered? (before it `agmux recall` without --dump says it is not there yet and exits 2.) Cheap: one process, no tty."""
    try: r = subprocess.run([BIN, "recall"], capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=10, env={"PATH": "/usr/bin:/bin", "HOME": "/nonexistent"})
    except subprocess.TimeoutExpired: return True
    return "only --dump" not in r.stderr


class RTerm(Term):
    """`Term` for the recall UI: the program runs in `cwd` (the scope "current project" follows it) with the recall environment (fakes first on PATH, cache dir, EDITOR, no TMUX), inside a real tmux pane that
    stands for the terminal: capture-pane is the screen. After an `exec` of a fake claude / pi the pane shows its marker line and is dead (remain-on-exit): `exit_status()` is the fake's."""

    def __init__(self, env, args="-r", w=140, h=40, cwd=None, tmux=False, lang="en", binary=None, **extra):
        self.env, self.w, self.h, self.name = env, w, h, "r" + uuid.uuid4().hex[:8]
        e = env.environ(tmux=tmux, AGMUX_LANG=lang, **extra)
        cmd = "env -i " + " ".join(f"{k}={shlex.quote(v)}" for k, v in e.items()) + f" {shlex.quote(binary or BIN)} {args}"
        r = tm("start-server", ";", "set", "-g", "remain-on-exit", "on", ";", "new-session", "-d", "-s", self.name, "-x", w, "-y", h, "-c", str(cwd or env.home), cmd)
        if r.returncode: raise RuntimeError(r.stderr)

    def visible(self, names):
        """which of {key: name} are on the screen: the layout of the list is the UI's, the session names are the fixture's"""
        t = self.text(); return {k for k, n in names.items() if n in t}
