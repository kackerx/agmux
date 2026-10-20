"""T1 regressions that are black-box checkable via `agmux show` (no tmux needed)."""
import subprocess, time, unittest
from lib import *


def show(env, extra_env="", path=None, timeout=30):
    p = path if path is not None else f"{env.bin}:/usr/bin:/bin"
    cmd = f"env -i PATH={shlex.quote(p)} HOME={shlex.quote(str(env.home))} XDG_STATE_HOME={shlex.quote(str(env.state))} {extra_env} {shlex.quote(BIN)} show"
    t0 = time.time()
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)
    return r, time.time() - t0


class Show(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.fx = build_fixture(self.env, with_hud=False)

    def test_hud_truncates(self):
        for body, want in (('{"context_window":{"used_percentage":41.6,"remaining_percentage":58.4},"rate_limits":{"used_percentage":7}}', "41%"),
                           ('{"rate_limits":{"five_hour":{"used_percentage":99}},"context_window":{"used_percentage":29.6}}', "29%"),
                           ('{"context_window":{"used_percentage":59.99}}', "59%"), ('{"context_window":{"used_percentage":null}}', "--"),
                           ('{"context_window":', "--")):
            with self.subTest(body=body):
                self.env.hud("n-a1", body)
                r, _ = show(self.env)
                row = [l.split("\t") for l in r.stdout.splitlines() if l.startswith("a1\t")][0]
                self.assertEqual(row[5], want)

    def test_no_home_errors_and_writes_nothing(self):
        cwd = self.env.tmp / "cwd"; cwd.mkdir()
        r = subprocess.run(f"cd {cwd} && env -i PATH={shlex.quote(str(self.env.bin))}:/usr/bin:/bin {shlex.quote(BIN)} show", shell=True, capture_output=True, text=True)
        self.assertEqual(r.returncode, 1); self.assertIn("HOME", r.stderr); self.assertEqual(list(cwd.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
