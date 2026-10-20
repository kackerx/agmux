"""T62 `agmux --version` / `agmux version`: one line `agmux <version> (<commit>, <date>)`, exit 0, nothing on stderr; `--version x` is a usage error (exit 2). The values come from the build (`-ldflags -X main.version= main.commit= main.date=`,
printed as they are), else from what Go recorded (a go install: the module's version -- a pseudo-version, or the tag -- and the 7 characters of the vcs revision and the vcs time), else `agmux dev (none, unknown)`. The usage's first line is
`usage: agmux [--version] <command>`. The command line moved into run() (T63's review): every other subcommand must exit and print what it did (the table below is what the binary of the batch before printed, apart from that first usage line).
Spec: specs/m3-config/tasks.md「第十二批」T62. No tmux: the binary run as a plain process, HOME / XDG_* in a temp dir."""
import os, re, shutil, subprocess, tempfile, unittest
from lib import *

USAGE_FIRST = "usage: agmux [--version] <command>"
LINE = re.compile(r"^agmux (\S+) \((\S+), (\S+)\)$")


class Base(unittest.TestCase):
    def setUp(self):
        self.tmp = pathlib.Path(tempfile.mkdtemp(prefix="agmux-t62-")); self.addCleanup(lambda: subprocess.run(["chmod", "-R", "u+w", str(self.tmp)]) and shutil.rmtree(self.tmp, ignore_errors=True))
        self.env = Env(); self.addCleanup(self.env.cleanup); self.home = self.env.home; build_fixture(self.env, with_hud=False)

    def run_bin(self, args, binary=None, stdin="", extra=None):
        e = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.tmp / "state"), "XDG_CONFIG_HOME": str(self.tmp / "cfg"), "LANG": "en_US.UTF-8", **(extra or {})}
        return subprocess.run([binary or BIN, *args], env=e, input=stdin, capture_output=True, text=True, timeout=30)


class Version(Base):
    def test_both_spellings_print_one_line_exit_0_and_say_nothing_on_stderr(self):
        for a in ("--version", "version"):
            with self.subTest(a=a):
                r = self.run_bin([a]); self.assertEqual((r.returncode, r.stderr), (0, ""), r); self.assertEqual(len(r.stdout.splitlines()), 1, repr(r.stdout)); self.assertTrue(r.stdout.endswith("\n")); self.assertRegex(r.stdout.rstrip("\n"), LINE)
                self.assertTrue(r.stdout.startswith("agmux "))
        self.assertEqual(self.run_bin(["--version"]).stdout, self.run_bin(["version"]).stdout)

    def test_a_version_with_anything_after_it_is_a_usage_error(self):
        for a in (["--version", "x"], ["version", "x"], ["--version", "--version"], ["-v"], ["--versions"], ["-version"]):
            with self.subTest(a=a):
                r = self.run_bin(a); self.assertEqual(r.returncode, 2, r); self.assertEqual(r.stdout, "", "usage goes to stderr"); self.assertIn(USAGE_FIRST, r.stderr)

    def test_the_usage_first_line_says_version(self):
        for a in (["help"], ["-h"], ["--help"]):
            r = self.run_bin(a); self.assertEqual((r.returncode, r.stderr), (0, ""), r); self.assertEqual(r.stdout.splitlines()[0], USAGE_FIRST)
        r = self.run_bin([]); self.assertEqual(r.returncode, 2); self.assertEqual(r.stderr.splitlines()[0], USAGE_FIRST)

    def test_the_readme_and_usage_know_it(self):
        self.assertIn("--version", (SRC / "README.md").read_text()); self.assertIn("--version", self.run_bin(["help"]).stdout)

    def test_the_binary_under_test_prints_something_a_build_can_have(self):
        m = LINE.match(self.run_bin(["--version"]).stdout.strip()); self.assertTrue(m); v, c, d = m.groups()
        self.assertTrue(v == "dev" or v.startswith("v"), v); self.assertTrue(c == "none" or re.fullmatch(r"[0-9a-f]{7}", c), c); self.assertTrue(d == "unknown" or re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ", d), d)


@unittest.skipUnless(shutil.which("go"), "needs the go toolchain")
class Builds(Base):
    """binaries built here from SRC: what the release builds do, and what a go install does"""
    def go(self, args, cwd, extra=None):
        e = {**os.environ, "GOFLAGS": "-modcacherw", **(extra or {})}; return subprocess.run(["go", *args], cwd=cwd, env=e, capture_output=True, text=True, timeout=300)

    def test_ldflags_values_are_printed_as_they_are(self):
        out = self.tmp / "x"; r = self.go(["build", "-trimpath", "-ldflags", "-X main.version=v0.0.0-test -X main.commit=abc1234 -X main.date=2026-10-17T00:00:00Z", "-o", str(out), "."], SRC); self.assertEqual(r.returncode, 0, r.stderr)
        r = self.run_bin(["--version"], binary=str(out)); self.assertEqual((r.stdout, r.stderr, r.returncode), ("agmux v0.0.0-test (abc1234, 2026-10-17T00:00:00Z)\n", "", 0))
        r = self.run_bin(["version"], binary=str(out)); self.assertEqual(r.stdout, "agmux v0.0.0-test (abc1234, 2026-10-17T00:00:00Z)\n")

    def test_the_ldflags_of_the_release_config_work_with_s_w(self):
        out = self.tmp / "y"; r = self.go(["build", "-trimpath", "-ldflags", "-s -w -X main.version=v1.2.3 -X main.commit=0123456 -X main.date=2026-01-02T03:04:05Z", "-o", str(out), "."], SRC); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.run_bin(["--version"], binary=str(out)).stdout, "agmux v1.2.3 (0123456, 2026-01-02T03:04:05Z)\n")

    def test_one_value_alone_leaves_the_others_to_go(self):
        out = self.tmp / "z"; r = self.go(["build", "-ldflags", "-X main.commit=feedbee", "-o", str(out), "."], SRC); self.assertEqual(r.returncode, 0, r.stderr); line = self.run_bin(["--version"], binary=str(out)).stdout.strip(); self.assertRegex(line, r"^agmux \S+ \(feedbee, \S+\)$")

    def test_go_install_from_a_clone_has_a_pseudo_version_and_seven_characters_of_the_commit(self):
        clone = self.tmp / "clone"; head = subprocess.run(["git", "-C", str(SRC), "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip(); self.assertTrue(head)
        r = subprocess.run(["git", "clone", "-q", str(SRC), str(clone)], capture_output=True, text=True); self.assertEqual(r.returncode, 0, r.stderr); subprocess.run(["git", "-C", str(clone), "checkout", "-q", head], check=True, capture_output=True)
        gobin = self.tmp / "gobin"; r = self.go(["install", "."], clone, {"GOBIN": str(gobin), "GOMODCACHE": subprocess.run(["go", "env", "GOMODCACHE"], capture_output=True, text=True).stdout.strip()}); self.assertEqual(r.returncode, 0, r.stderr)
        line = self.run_bin(["--version"], binary=str(gobin / "agmux")).stdout.strip(); m = LINE.match(line); self.assertTrue(m, line); v, c, d = m.groups()
        self.assertRegex(v, r"^v\d+\.\d+\.\d+-(0\.)?\d{14}-[0-9a-f]{12}$", "a pseudo-version (vX.Y.Z-yyyymmddhhmmss-rev; after a release tag: vX.Y.(Z+1)-0.yyyymmddhhmmss-rev), not dev"); self.assertEqual(c, head[:7]); self.assertEqual(v.split("-")[-1], head[:12]); self.assertRegex(d, r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$")
        self.assertEqual(d.replace("-", "").replace(":", "").replace("T", "").rstrip("Z"), re.search(r"(\d{14})-[0-9a-f]{12}$", v).group(1), "the time in the pseudo-version is the commit's")

    def test_a_tag_on_the_commit_is_the_version_and_a_change_is_marked_dirty(self):
        clone = self.tmp / "clone2"; head = subprocess.run(["git", "-C", str(SRC), "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip()
        subprocess.run(["git", "clone", "-q", str(SRC), str(clone)], check=True, capture_output=True); subprocess.run(["git", "-C", str(clone), "checkout", "-q", head], check=True, capture_output=True); subprocess.run(["git", "-C", str(clone), "tag", "v0.0.0-t"], check=True, capture_output=True)
        env = {"GOBIN": str(self.tmp / "gb2"), "GOMODCACHE": subprocess.run(["go", "env", "GOMODCACHE"], capture_output=True, text=True).stdout.strip()}; r = self.go(["install", "."], clone, env); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertRegex(self.run_bin(["--version"], binary=str(self.tmp / "gb2" / "agmux")).stdout, rf"^agmux v0\.0\.0-t \({head[:7]}, ")
        (clone / "README.md").write_text((clone / "README.md").read_text() + "\nx\n"); r = self.go(["install", "."], clone, env); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertRegex(self.run_bin(["--version"], binary=str(self.tmp / "gb2" / "agmux")).stdout, r"^agmux v0\.0\.0-t\+dirty \(", "what Go says about a modified tree is passed on")


class Routing(Base):
    """every other way in is as it was: exit code, what is on stdout / stderr"""
    def test_help_unknown_and_wrong_argument_counts(self):
        r = self.run_bin([]); self.assertEqual((r.returncode, r.stdout), (2, "")); self.assertTrue(r.stderr.startswith(USAGE_FIRST))
        r = self.run_bin(["bogus"]); self.assertEqual((r.returncode, r.stdout), (2, "")); self.assertTrue(r.stderr.startswith('agmux: unknown command "bogus"\n\n' + USAGE_FIRST), r.stderr)
        for a in (["-x"], ["help", "x"], ["show", "x"], ["show", "x", "y"]):
            r = self.run_bin(a); self.assertEqual((r.returncode, r.stdout), (2, ""), a); self.assertIn(USAGE_FIRST, r.stderr)
        for a in (["help"], ["-h"], ["--help"]):
            r = self.run_bin(a); self.assertEqual((r.returncode, r.stderr), (0, "")); self.assertIn("commands:", r.stdout)
        self.assertEqual(self.run_bin(["help"]).stdout, self.run_bin(["--help"]).stdout)

    def test_the_commands_that_take_arguments(self):
        r = self.run_bin(["show"]); self.assertEqual((r.returncode, r.stderr), (0, "")); self.assertEqual(len(r.stdout.splitlines()), 9); self.assertTrue(all(len(l.split("\t")) == 8 for l in r.stdout.splitlines()))
        r = self.run_bin(["avatars"]); self.assertEqual((r.returncode, r.stderr), (0, "")); self.assertIn("builtin", r.stdout); self.assertIn("bot-01", r.stdout)
        r = self.run_bin(["avatars", "import"]); self.assertEqual(r.returncode, 2); self.assertEqual(r.stdout, "")
        r = self.run_bin(["avatars", "import", "/nonexistent.png", "--name", "zz"]); self.assertEqual((r.returncode, r.stdout), (1, "")); self.assertTrue(r.stderr.startswith("agmux:"), r.stderr)
        r = self.run_bin(["config"]); self.assertEqual((r.returncode, r.stderr), (0, "")); self.assertIn("[keys]", r.stdout); r = self.run_bin(["config", "--path"]); self.assertEqual((r.returncode, r.stderr), (0, "")); self.assertTrue(r.stdout.strip().endswith("agmux/config.toml"))
        r = self.run_bin(["config", "x"]); self.assertEqual((r.returncode, r.stdout), (2, ""))
        r = self.run_bin(["statusline", "--", "cat"], stdin='{"model":{"display_name":"x"}}'); self.assertEqual((r.returncode, r.stderr), (0, "")); self.assertIn("display_name", r.stdout)
        r = self.run_bin(["board", "--bogus"]); self.assertEqual((r.returncode, r.stdout), (2, "")); r = self.run_bin(["board", "x"]); self.assertEqual((r.returncode, r.stdout), (2, ""))

    def test_setup_is_a_dry_run_without_write_and_writes_only_the_temp_home_with_it(self):
        s = self.home / ".claude" / "settings.json"; s.write_text('{"theme":"dark"}'); r = self.run_bin(["setup"]); self.assertEqual((r.returncode, r.stderr), (0, "")); self.assertIn("agmux statusline", r.stdout); self.assertEqual(s.read_text(), '{"theme":"dark"}', "a dry run changes nothing")
        r = self.run_bin(["setup", "x"]); self.assertEqual((r.returncode, r.stdout), (2, ""))
        r = self.run_bin(["setup", "--write"]); self.assertEqual((r.returncode, r.stderr), (0, "")); self.assertIn('"agmux statusline"', s.read_text()); self.assertTrue((self.home / ".claude" / "settings.json.bak").exists(), "the backup"); self.assertIn("theme", s.read_text())


if __name__ == "__main__":
    unittest.main()
