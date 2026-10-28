"""T63 the release pipeline, as far as it can be run here (what needs GitHub is for the first tag): `npm/build.mjs <tag> <dir of the archives> <out>` makes the shell package @kackerx/agmux and the four platform packages
@kackerx/agmux-{darwin,linux}-{arm64,x64} (3 files each: bin, LICENSE, package.json; all at the tag's version; the shell's optionalDependencies name the four); laid out as npm installs them (node_modules/@kackerx/...) the
shell's bin/agmux.js runs the platform package's binary with the arguments, stdio and exit code as they are, and says what to do when that package is not there (exit 1, `go install github.com/kackerx/agmux@latest`);
`.github/scripts/tap-formula.sh` prints a Formula/agmux.rb that parses (Ruby 4 of brew: the system Ruby 2.6 does not know the `ldflags:` shorthand) and carries the tag, the sha, the commit, the date, the license; the
GoReleaser config and the workflows say what the spec says. The archives here are made by hand from the binary under test (same names as GoReleaser's, a real `goreleaser release --snapshot` was run by hand, not here:
it needs the network). Spec: specs/m3-config/tasks.md「第十二批」T63. No tmux, nothing is pushed or published."""
import io, json, os, re, shutil, stat, subprocess, tarfile, tempfile, unittest
from lib import *

TAG = "v0.0.0-t"
PLATFORMS = [("darwin", "arm64", "darwin", "arm64"), ("darwin", "amd64", "darwin", "x64"), ("linux", "arm64", "linux", "arm64"), ("linux", "amd64", "linux", "x64")]  # GOOS, GOARCH, node platform, node arch
NODE = shutil.which("node")
BREW_RUBY = next(iter(sorted(pathlib.Path("/opt/homebrew/Library/Homebrew/vendor/portable-ruby").glob("*/bin/ruby"))), None)


def make_archives(d, binary, version="0.0.0-t"):
    """four tar.gz named as GoReleaser names them, each with LICENSE, README.md and agmux (the binary under test in all of them)"""
    d.mkdir(parents=True, exist_ok=True)
    for goos, goarch, _, _ in PLATFORMS:
        with tarfile.open(d / f"agmux_{version}_{goos}_{goarch}.tar.gz", "w:gz") as t:
            for name, data, mode in (("LICENSE", (SRC / "LICENSE").read_bytes(), 0o644), ("README.md", b"readme\n", 0o644), ("agmux", pathlib.Path(binary).read_bytes(), 0o755)):
                ti = tarfile.TarInfo(name); ti.size = len(data); ti.mode = mode; t.addfile(ti, io.BytesIO(data))


class Base(unittest.TestCase):
    def setUp(self):
        self.tmp = pathlib.Path(tempfile.mkdtemp(prefix="agmux-t63-")); self.addCleanup(lambda: shutil.rmtree(self.tmp, ignore_errors=True))
        self.arch = self.tmp / "dist"; make_archives(self.arch, BIN); self.out = self.tmp / "out"

    def build(self, tag=TAG, archives=None, out=None):
        return subprocess.run([NODE, str(SRC / "npm" / "build.mjs"), tag, str(archives or self.arch), str(out or self.out)], capture_output=True, text=True, timeout=60)

    def pkg(self, name): return json.loads((self.out / name / "package.json").read_text())

    def install(self, with_platform=True):
        """npm's layout: tmp/inst/node_modules/@kackerx/{agmux, agmux-<os>-<cpu>}; returns the shell's bin script"""
        nm = self.tmp / "inst" / "node_modules" / "@kackerx"; nm.mkdir(parents=True); shutil.copytree(self.out / "agmux", nm / "agmux", symlinks=True)
        here = f"{os.uname().sysname.lower()}-{ {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'x64'}[os.uname().machine] }"
        if with_platform: shutil.copytree(self.out / f"agmux-{here}", nm / f"agmux-{here}", symlinks=True)
        return nm / "agmux" / "bin" / "agmux.js", here

    def node(self, script, args=(), stdin=""):
        env = {"PATH": f"{self.tmp / 'ccbin'}:/usr/bin:/bin:" + os.path.dirname(NODE), "HOME": str(self.tmp / "home"), "XDG_STATE_HOME": str(self.tmp / "state"), "XDG_CONFIG_HOME": str(self.tmp / "cfg"), "LANG": "en_US.UTF-8"}
        (self.tmp / "home").mkdir(exist_ok=True); return subprocess.run([NODE, str(script), *args], env=env, input=stdin, capture_output=True, text=True, timeout=60)


@unittest.skipUnless(NODE, "needs node")
class Packages(Base):
    def test_five_packages_with_three_files_each_at_the_tags_version(self):
        r = self.build(); self.assertEqual(r.returncode, 0, r.stderr); names = sorted(p.name for p in self.out.iterdir()); self.assertEqual(names, ["agmux", "agmux-darwin-arm64", "agmux-darwin-x64", "agmux-linux-arm64", "agmux-linux-x64"])
        for n in names:
            files = sorted(str(p.relative_to(self.out / n)) for p in (self.out / n).rglob("*") if p.is_file()); self.assertEqual(files, ["LICENSE", "bin/agmux.js" if n == "agmux" else "bin/agmux", "package.json"], n)
            self.assertEqual((self.out / n / "LICENSE").read_bytes(), (SRC / "LICENSE").read_bytes(), f"{n}: MIT asks for a copy of the license"); self.assertEqual(self.pkg(n)["version"], "0.0.0-t", f"{n}: the tag without its v"); self.assertEqual(self.pkg(n)["license"], "MIT")

    def test_the_shell_names_the_four_platform_packages_and_the_bin(self):
        self.build(); p = self.pkg("agmux"); self.assertEqual(p["name"], "@kackerx/agmux"); self.assertEqual(p["bin"], {"agmux": "bin/agmux.js"}); self.assertEqual(p["files"], ["bin"])
        self.assertEqual(p["optionalDependencies"], {f"@kackerx/agmux-{os_}-{cpu}": "0.0.0-t" for _, _, os_, cpu in PLATFORMS}); shell = json.loads((SRC / "npm" / "agmux" / "package.json").read_text()); self.assertEqual(p["repository"], shell["repository"]); self.assertIn("kackerx/agmux", json.dumps(p["repository"])); self.assertIn("engines", p)

    def test_each_platform_package_says_where_it_runs_and_holds_that_archives_binary(self):
        self.build()
        for goos, goarch, os_, cpu in PLATFORMS:
            n = f"agmux-{os_}-{cpu}"; p = self.pkg(n); self.assertEqual((p["name"], p["os"], p["cpu"], p["files"]), (f"@kackerx/{n}", [os_], [cpu], ["bin"]))
            b = self.out / n / "bin" / "agmux"; self.assertTrue(os.access(b, os.X_OK), f"{n}: the binary keeps its mode"); self.assertEqual(b.read_bytes(), pathlib.Path(BIN).read_bytes())

    def test_each_archives_own_binary_goes_to_its_package(self):
        d = self.tmp / "dist2"; d.mkdir()
        for goos, goarch, os_, cpu in PLATFORMS:
            with tarfile.open(d / f"agmux_0.0.0-t_{goos}_{goarch}.tar.gz", "w:gz") as t:
                data = f"binary of {goos}/{goarch}".encode(); ti = tarfile.TarInfo("agmux"); ti.size = len(data); ti.mode = 0o755; t.addfile(ti, io.BytesIO(data))
        r = self.build(archives=d); self.assertEqual(r.returncode, 0, r.stderr)
        for goos, goarch, os_, cpu in PLATFORMS: self.assertEqual((self.out / f"agmux-{os_}-{cpu}" / "bin" / "agmux").read_text(), f"binary of {goos}/{goarch}")

    def test_a_tag_without_the_v_and_a_second_run_over_the_same_out_dir(self):
        self.assertEqual(self.build(tag="1.2.3").returncode, 0); self.assertEqual(self.pkg("agmux")["version"], "1.2.3"); (self.out / "stale").write_text("x")
        self.assertEqual(self.build(tag="v1.2.4").returncode, 0); self.assertEqual(self.pkg("agmux")["version"], "1.2.4"); self.assertFalse((self.out / "stale").exists(), "the out dir is made again from nothing")

    def test_a_missing_archive_is_an_error_that_names_it_and_leaves_no_half_publishable_set(self):
        (self.arch / "agmux_0.0.0-t_linux_arm64.tar.gz").unlink(); r = self.build(); self.assertNotEqual(r.returncode, 0); self.assertIn("no archive for linux_arm64", r.stderr)

    def test_without_arguments_it_is_a_usage_error(self):
        r = subprocess.run([NODE, str(SRC / "npm" / "build.mjs")], capture_output=True, text=True); self.assertEqual(r.returncode, 2); self.assertIn("usage: node npm/build.mjs", r.stderr)

    def test_npm_pack_lists_only_what_belongs_in_a_package(self):
        if not shutil.which("npm"): self.skipTest("no npm")
        self.build()
        for n in sorted(p.name for p in self.out.iterdir()):
            r = subprocess.run(["npm", "pack", "--dry-run", "--json"], cwd=self.out / n, capture_output=True, text=True, timeout=120, env={**os.environ, "npm_config_cache": str(self.tmp / "npmcache")}); self.assertEqual(r.returncode, 0, r.stderr)
            files = sorted(f["path"] for f in json.loads(r.stdout)[0]["files"]); self.assertEqual(files, ["LICENSE", "bin/agmux.js" if n == "agmux" else "bin/agmux", "package.json"], n)


@unittest.skipUnless(NODE, "needs node")
class Shell(Base):
    def setUp(self):
        super().setUp(); self.assertEqual(self.build().returncode, 0)

    def test_the_shell_runs_the_platform_binary_version_included(self):
        script, _ = self.install(); want = subprocess.run([BIN, "--version"], capture_output=True, text=True).stdout; r = self.node(script, ["--version"]); self.assertEqual((r.returncode, r.stdout, r.stderr), (0, want, "")); self.assertTrue(r.stdout.startswith("agmux "))

    def test_arguments_stdio_and_the_exit_code_are_the_binarys(self):
        script, _ = self.install(); r = self.node(script, ["--version", "x"]); self.assertEqual(r.returncode, 2); self.assertEqual(r.stdout, ""); self.assertIn("usage: agmux [--version] <command>", r.stderr)
        r = self.node(script, ["avatars", "import", "/nonexistent.png", "--name", "zz"]); self.assertEqual(r.returncode, 1); self.assertTrue(r.stderr.startswith("agmux:"), r.stderr)
        r = self.node(script, ["statusline", "--", "cat"], stdin='{"model":{"display_name":"x"}}'); self.assertEqual(r.returncode, 0); self.assertIn("display_name", r.stdout, "stdin reached the binary, its stdout came back")
        self.assertEqual(self.node(script, ["config", "--path"]).stdout.strip().endswith("agmux/config.toml"), True)

    def test_a_killed_binary_kills_the_shell_the_same_way(self):
        script, here = self.install(); b = self.tmp / "inst" / "node_modules" / "@kackerx" / f"agmux-{here}" / "bin" / "agmux"; b.write_text("#!/bin/sh\nkill -TERM $$\n"); b.chmod(0o755)
        r = self.node(script, ["x"]); self.assertEqual(r.returncode, -15, "the shell died of the same signal as the binary")

    def test_without_the_platform_package_it_says_what_is_missing_and_exits_1(self):
        script, here = self.install(with_platform=False); r = self.node(script, ["--version"]); self.assertEqual(r.returncode, 1); self.assertEqual(r.stdout, ""); self.assertIn(f"@kackerx/agmux-{here}", r.stderr); self.assertIn("go install github.com/kackerx/agmux@latest", r.stderr)
        self.assertIn("optional dependency", r.stderr)

    def test_a_platform_package_that_is_not_runnable_is_one_line_exit_1(self):
        script, here = self.install(); b = self.tmp / "inst" / "node_modules" / "@kackerx" / f"agmux-{here}" / "bin" / "agmux"; b.chmod(0o644); r = self.node(script, ["--version"]); self.assertEqual(r.returncode, 1); self.assertTrue(r.stderr.startswith("agmux:"), r.stderr); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr)


class Formula(unittest.TestCase):
    def run_sh(self, *args):
        return subprocess.run(["sh", str(SRC / ".github" / "scripts" / "tap-formula.sh"), *args], capture_output=True, text=True, timeout=20)

    def test_it_carries_the_tag_the_sha_the_commit_the_date_and_the_license(self):
        sha = "a" * 64; r = self.run_sh("v1.2.3", sha, "abc1234", "2026-10-17T06:01:34Z"); self.assertEqual(r.returncode, 0, r.stderr); f = r.stdout
        for part in ('class Agmux < Formula', 'homepage "https://github.com/kackerx/agmux"', 'url "https://github.com/kackerx/agmux/archive/refs/tags/v1.2.3.tar.gz"', f'sha256 "{sha}"', 'license "MIT"', 'depends_on "go" => :build',
                     '-s -w -X main.version=v#{version} -X main.commit=abc1234 -X main.date=2026-10-17T06:01:34Z', 'system "go", "build", *std_go_args(ldflags:), "."', 'assert_match "agmux v#{version}", shell_output("#{bin}/agmux --version")'):
            self.assertIn(part, f)
        self.assertRegex(f, r'desc "[^"]+"'); self.assertNotIn("\t", f)

    def test_commit_and_date_are_optional_and_say_so(self):
        f = self.run_sh("v0.1.0", "b" * 64).stdout; self.assertIn("-X main.commit=none -X main.date=unknown", f)

    def test_the_tag_and_the_sha_are_required(self):
        for a in ((), ("v1",)): self.assertNotEqual(self.run_sh(*a).returncode, 0, a)

    @unittest.skipUnless(BREW_RUBY, "no brew portable ruby")
    def test_it_parses_with_the_ruby_brew_uses(self):
        f = self.run_sh("v0.0.0-t", "c" * 64, "abc1234", "2026-10-17T00:00:00Z").stdout; r = subprocess.run([str(BREW_RUBY), "-c"], input=f, capture_output=True, text=True); self.assertEqual((r.returncode, r.stdout.strip()), (0, "Syntax OK"), r.stderr)
        r = subprocess.run([str(BREW_RUBY), "-c"], input=f.replace("end\n", "", 1), capture_output=True, text=True); self.assertNotEqual(r.returncode, 0, "(and a broken one does not)")


class Config(unittest.TestCase):
    """the files say what the spec says"""
    def text(self, p): return (SRC / p).read_text()

    def test_goreleaser(self):
        g = self.text(".goreleaser.yaml")
        for part in ("project_name: agmux", "main: .", "binary: agmux", "CGO_ENABLED=0", "-trimpath", "goos: [darwin, linux]", "goarch: [amd64, arm64]", "-X main.version=v{{.Version}}", "-X main.commit={{.ShortCommit}}", "-X main.date={{.CommitDate}}", 'name_template: "agmux_{{ .Version }}_{{ .Os }}_{{ .Arch }}"', "checksums.txt", 'ignore_tags: ["m*"]'):
            self.assertIn(part, g)
        self.assertNotIn("brews", g); self.assertNotIn("windows", g); self.assertNotIn("casks", g)
        for p in ("^m\\d+[:/]", "^e2e:", "^specs:", "^docs:", "^docs ", "^Merge "): self.assertIn(p, g, "internal commits stay out of the changelog")

    def test_the_changelog_filter_against_this_repos_subjects(self):
        pats = re.findall(r"""['"](\^[^'"]+)['"]""", self.text(".goreleaser.yaml").split("filters:")[1].split("release:")[0]); self.assertGreaterEqual(len(pats), 8, pats)
        subs = subprocess.run(["git", "-C", str(SRC), "log", "--format=%s"], capture_output=True, text=True).stdout.splitlines()
        if len(subs) < 50: self.skipTest("no git history")
        kept = [s for s in subs if not any(re.search(p, s) for p in pats)]; self.assertLess(len(kept), len(subs) // 2, "most of this repo's commits are milestone / spec / test commits"); self.assertEqual([s for s in kept if re.match(r"^m\d+:", s)], [], "no `m1:` .. `m3:` commit is left")
        self.assertTrue(any(re.search(p, "e2e: a test") for p in pats)); self.assertFalse(any(re.search(p, "board: pin a card") for p in pats), "a user-visible change stays")

    def test_workflows(self):
        ci, rel = self.text(".github/workflows/ci.yml"), self.text(".github/workflows/release.yml")
        for part in ("go vet ./...", "go test -race ./...", "gofmt -l", "go mod tidy", "workflow_call"): self.assertIn(part, ci)
        self.assertNotIn("unittest", ci); self.assertNotIn("test/e2e", [l for l in ci.splitlines() if not l.startswith("#")], "the e2e suite is Python on a real tmux: not in CI"); self.assertFalse(any("e2e" in l for l in ci.splitlines() if not l.lstrip().startswith("#")))
        for part in ('tags: ["v*"]', "uses: ./.github/workflows/ci.yml", "goreleaser/goreleaser-action", "GITHUB_TOKEN", "secrets.TAP_GITHUB_TOKEN", "kackerx/homebrew-tap", "tap-formula.sh", "id-token: write", "npm/build.mjs", "npm publish ./out/", "--version | grep -F"):
            self.assertIn(part, rel)
        self.assertIn("agmux-linux-x64", rel)

    def test_readme_installs_three_ways_and_says_the_binary_is_unsigned(self):
        r = self.text("README.md")
        for part in ("brew install kackerx/tap/agmux", "npm i -g @kackerx/agmux", "go install github.com/kackerx/agmux@latest"): self.assertIn(part, r)
        self.assertIn("二进制没有签名", r); self.assertIn("agmux --version", r)

    def test_license_is_mit_with_a_copyright_line_and_gitignore_keeps_dist_out(self):
        l = self.text("LICENSE"); self.assertTrue(l.startswith("MIT License")); self.assertRegex(l, r"Copyright \(c\) 2026 kackerx"); self.assertIn("/dist/", self.text(".gitignore").split())

    def test_the_shell_package_files(self):
        p = json.loads(self.text("npm/agmux/package.json")); self.assertEqual((p["name"], p["license"], p["bin"]), ("@kackerx/agmux", "MIT", {"agmux": "bin/agmux.js"})); self.assertTrue(self.text("npm/agmux/bin/agmux.js").startswith("#!/usr/bin/env node"))
        self.assertTrue(os.access(SRC / "npm" / "agmux" / "bin" / "agmux.js", os.X_OK), "a bin script must be executable in the tarball (npm sets it on install, git keeps the mode for a clone)")


if __name__ == "__main__":
    unittest.main()
