"""T35 `agmux avatars import <sheet.png> --name <id> [--layout 30|19] [--force]`: cuts a pi-agent-portrait sheet (6 x 5 cells, `--layout 19`: 5 x 4) into a user image set under
$XDG_CONFIG_HOME/agmux/avatars/<id>/ with pi's file names, 128 x 128 each, the grid found where the dark background ends, every cell cut as a centred square of 97%.
Spec: specs/m3-config/tasks.md「第六批」T35."""
import os, pathlib, shutil, stat, subprocess, sys, unittest
from lib import *
import test_t28 as t28
from test_t18 import overlay, LISTING, N_SETS, find_grid
from test_t3 import ready, last_text_row


class Base(t28.Base):
    def setUp(self):
        super().setUp(); self.sheet = self.env.tmp / "sheet.png"; self.dir = self.cfg / "agmux" / "avatars"

    def write_sheet(self, **kw):
        data, self.sw, self.sh = sheet_png(**kw); self.sheet.write_bytes(data); return data

    def run_cmd(self, *args, cwd=None):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=30, stdin=subprocess.DEVNULL, cwd=cwd)

    def files(self, d):
        return sorted(p.relative_to(d).as_posix() for p in d.rglob("*") if p.is_file())

    def leftovers(self):
        return [p.name for p in self.dir.iterdir() if p.name.startswith(".")] if self.dir.exists() else []


class Cut(Base):
    def check_cells(self, d, names, cols, rows):
        for i, rel in enumerate(names):
            w, h, px, ch = decode_png((d / rel).read_bytes()); self.assertEqual((w, h), (128, 128), rel)
            want = bytes(sheet_colour(i // cols, i % cols)); bad = {bytes(r[x * ch:x * ch + 3]) for r in px for x in range(w)} - {want}
            self.assertEqual(bad, set(), f"{rel}: every pixel is the colour of cell (row {i // cols}, column {i % cols}); the brighter side strips are outside the 97% centre square")

    def test_30_cells_become_30_files_with_pi_names_128_square_and_the_right_colours(self):
        self.write_sheet(); r = self.run_cmd("avatars", "import", str(self.sheet), "--name", "kit"); self.assertEqual(r.returncode, 0, r.stderr); d = self.dir / "kit"
        self.assertIn("imported", r.stdout); self.assertIn(str(d), r.stdout); self.assertEqual(self.files(d), sorted(PI_FRAMES), "pi's names: the same set works in pi")
        self.check_cells(d, PI_FRAMES, 6, 5); self.assertEqual(self.leftovers(), [], "no temporary directory left behind")

    def test_layout_19_is_the_first_19_frames_on_a_5_by_4_grid(self):
        self.write_sheet(cols=5, rows=4, cw=200, ch=160, blank_from=19)
        r = self.run_cmd("avatars", "import", str(self.sheet), "--name", "old", "--layout", "19"); self.assertEqual(r.returncode, 0, r.stderr); d = self.dir / "old"
        self.assertEqual(self.files(d), sorted(PI_FRAMES[:19])); self.check_cells(d, PI_FRAMES[:19], 5, 4)

    def test_flags_may_come_before_or_after_the_sheet(self):
        self.write_sheet()
        for args, name in ((("--name", "a1", str(self.sheet)), "a1"), ((str(self.sheet), "--name", "a2", "--layout", "30"), "a2"), (("--layout", "30", "--name", "a3", str(self.sheet), "--force"), "a3")):
            r = self.run_cmd("avatars", "import", *args); self.assertEqual(r.returncode, 0, f"{args}: {r.stderr}"); self.assertEqual(len(self.files(self.dir / name)), 30)

    def test_a_non_square_sheet_and_other_sizes_cut_the_same_way(self):
        self.write_sheet(cw=100, ch=130, pad=10, edge=0)  # cells taller than wide: the 97% square is as wide as the cell (so no brighter side strips here)
        r = self.run_cmd("avatars", "import", str(self.sheet), "--name", "tall"); self.assertEqual(r.returncode, 0, r.stderr); self.check_cells(self.dir / "tall", PI_FRAMES, 6, 5)

    def test_files_and_directories_are_readable(self):
        self.write_sheet(); self.run_cmd("avatars", "import", str(self.sheet), "--name", "kit"); d = self.dir / "kit"
        self.assertEqual(stat.S_IMODE(d.stat().st_mode), 0o755, "the set directory is not the 0700 of a temp dir"); self.assertTrue(all(stat.S_IMODE(p.stat().st_mode) & 0o444 == 0o444 for p in d.rglob("*.png")))


class Errors(Base):
    def test_an_existing_set_needs_force_and_force_replaces_it_whole(self):
        self.write_sheet(); self.assertEqual(self.run_cmd("avatars", "import", str(self.sheet), "--name", "kit").returncode, 0)
        extra = self.dir / "kit" / "mine" / "own.png"; extra.parent.mkdir(); extra.write_bytes(png())
        r = self.run_cmd("avatars", "import", str(self.sheet), "--name", "kit"); self.assertEqual(r.returncode, 1); self.assertIn("exists", r.stderr); self.assertIn("--force", r.stderr); self.assertTrue(extra.exists(), "nothing was touched")
        r = self.run_cmd("avatars", "import", str(self.sheet), "--name", "kit", "--force"); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertFalse(extra.exists(), "the old set is replaced, not merged"); self.assertEqual(len(self.files(self.dir / "kit")), 30); self.assertEqual(self.leftovers(), [])

    def test_not_a_png_and_a_missing_file_are_errors_that_create_nothing(self):
        (self.env.tmp / "x.png").write_text("not a png"); (self.env.tmp / "x.jpg").write_bytes(b"\xff\xd8\xff\xe0")
        for f in ("x.png", "x.jpg"):
            r = self.run_cmd("avatars", "import", str(self.env.tmp / f), "--name", "kit"); self.assertEqual(r.returncode, 1, f); self.assertRegex(r.stderr, r"PNG"); self.assertFalse((self.dir / "kit").exists())
        r = self.run_cmd("avatars", "import", str(self.env.tmp / "missing.png"), "--name", "kit"); self.assertEqual(r.returncode, 1); self.assertIn("missing.png", r.stderr); self.assertFalse((self.dir / "kit").exists())
        self.assertEqual(self.leftovers(), [])

    def test_a_sheet_with_no_dark_background_is_refused(self):
        light = png_pixels(100, 80, lambda x, y: (250, 250, 250)); self.sheet.write_bytes(light)
        r = self.run_cmd("avatars", "import", str(self.sheet), "--name", "kit"); self.assertEqual(r.returncode, 1); self.assertIn("dark", r.stderr); self.assertFalse((self.dir / "kit").exists()); self.assertEqual(self.leftovers(), [])

    def test_a_failed_force_leaves_the_old_set_alone(self):
        self.write_sheet(); self.run_cmd("avatars", "import", str(self.sheet), "--name", "kit"); before = {f: (self.dir / "kit" / f).read_bytes() for f in self.files(self.dir / "kit")}
        (self.env.tmp / "bad.png").write_text("nope"); r = self.run_cmd("avatars", "import", str(self.env.tmp / "bad.png"), "--name", "kit", "--force")
        self.assertEqual(r.returncode, 1); self.assertEqual({f: (self.dir / "kit" / f).read_bytes() for f in self.files(self.dir / "kit")}, before, "the set that was there is untouched")

    def test_usage_errors_exit_2_with_the_usage_line(self):
        self.write_sheet()
        for args in (("avatars", "import"), ("avatars", "import", str(self.sheet)), ("avatars", "import", "--name", "kit"), ("avatars", "import", str(self.sheet), "extra", "--name", "kit"), ("avatars", "nosuch")):
            with self.subTest(args=args):
                r = self.run_cmd(*args); self.assertEqual(r.returncode, 2, r.stderr); self.assertIn("usage: agmux avatars import <sheet.png> --name <id>", r.stderr)
        r = self.run_cmd("avatars", "import", "--nosuchflag"); self.assertEqual(r.returncode, 2); self.assertIn("flag provided but not defined: -nosuchflag", r.stderr, "an unknown flag: exit 2 with the flag package's own message")
        self.assertFalse((self.dir / "kit").exists())

    def test_bad_layout_and_bad_names_are_refused(self):
        self.write_sheet()
        r = self.run_cmd("avatars", "import", str(self.sheet), "--name", "kit", "--layout", "7"); self.assertEqual(r.returncode, 1); self.assertIn("--layout", r.stderr); self.assertIn("19", r.stderr)
        for bad in ("hash", ".hidden", "a/b", "a\\b", "a b", "a:b", "é", "名字", "-x", "_x", "x\ty", "a*"):
            with self.subTest(name=bad):
                r = self.run_cmd("avatars", "import", str(self.sheet), "--name", bad); self.assertNotEqual(r.returncode, 0, bad); self.assertIn("--name", r.stderr); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr)
        self.assertEqual(self.files(self.dir) if self.dir.exists() else [], [])

    def test_names_the_regex_allows_are_accepted(self):
        self.write_sheet()
        for ok in ("a", "A9", "9a", "my-set_1", "kit.v2", "Z", "a" * 60):
            with self.subTest(name=ok):
                r = self.run_cmd("avatars", "import", str(self.sheet), "--name", ok); self.assertEqual(r.returncode, 0, f"{ok}: {r.stderr}"); self.assertTrue((self.dir / ok).is_dir())

    def test_dash_h_prints_the_usage_line_and_the_flags_and_exits_2(self):
        r = self.run_cmd("avatars", "import", "-h"); self.assertEqual(r.returncode, 2); out = r.stdout + r.stderr
        for s in ("usage: agmux avatars import <sheet.png> --name <id> [--layout 30|19] [--force]", "-force", "-layout", "-name"): self.assertIn(s, out)

    def test_help_lists_the_command(self):
        r = self.run_cmd("help"); self.assertIn("avatars import <sheet.png> --name <id>", r.stdout)
        self.assertEqual(self.run_cmd("avatars").returncode, 0, "plain `agmux avatars` still lists")


class ForceRace(Base):
    """--force moves the old set aside, moves the new one in, and drops the old one only if that worked: whatever happens (another process creating the directory in between),
    a user's set is never lost. Best-effort stress: an intruder keeps (re)creating <id> with a file inside while `import --force` runs over a set that is there."""

    def test_the_old_set_survives_or_the_new_one_is_complete_never_neither(self):
        import threading
        self.write_sheet(); kit = self.dir / "kit"; self.assertEqual(self.run_cmd("avatars", "import", str(self.sheet), "--name", "kit").returncode, 0)
        marker = kit / "mine" / "own.png"; marker.parent.mkdir(); marker.write_bytes(png((1, 1, 1))); stop = threading.Event(); intr = {"n": 0}; quiet = threading.Event(); quiet.set()  # quiet: the intruder may act
        def intruder():
            while not stop.is_set():
                try:
                    if quiet.is_set() and not kit.exists(): kit.mkdir(); (kit / "x").write_text("intruder"); intr["n"] += 1
                except OSError: pass
        th = threading.Thread(target=intruder, daemon=True); th.start(); outcomes = []
        try:
            for _ in range(25):
                r = self.run_cmd("avatars", "import", str(self.sheet), "--name", "kit", "--force"); outcomes.append(r.returncode)
                if r.returncode == 0:
                    quiet.clear(); time.sleep(0.03)
                    self.assertGreaterEqual(len(list(kit.rglob("*.png"))), 30, f"run {len(outcomes)}: exit 0 means the new set is in place"); self.assertFalse(marker.exists(), "and it replaced the old one")
                    marker.parent.mkdir(exist_ok=True); marker.write_bytes(png((1, 1, 1))); quiet.set()  # put the user's file back for the next round
                else:
                    import re as _re
                    m = _re.search(r"the old set could not be put back \(.*?\): it is at (\S+)|the old set is kept at (\S+)", r.stderr); kept = pathlib.Path((m.group(1) or m.group(2)).rstrip(".,;:")) if m else None
                    self.assertTrue(marker.exists() or (kept and (kept / "mine" / "own.png").exists()),
                                    f"run {len(outcomes)}: exit 1 and the user's old set is neither in <id> nor at the path the error names: {r.stderr!r}")
                    if kept and not marker.exists():  # the old set was left at its bak path: put it back for the next round (with the intruder held back)
                        quiet.clear(); time.sleep(0.05); shutil.rmtree(kit, ignore_errors=True); os.rename(kept, kit); quiet.set()
        finally:
            stop.set(); th.join(2)
        print("force-race outcomes", outcomes, "intruder dirs", intr["n"]); self.assertTrue(set(outcomes) <= {0, 1}, outcomes)


class Afterwards(Base):
    def imported(self, name="kit"):
        self.write_sheet(); r = self.run_cmd("avatars", "import", str(self.sheet), "--name", name); self.assertEqual(r.returncode, 0, r.stderr)

    def test_agmux_avatars_lists_it_with_the_seven_state_counts(self):
        self.imported(); out = self.run_cmd("avatars").stdout.split("\n\n")[0].split("\n")
        self.assertEqual(out[-1], "kit  user  idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2", "pi's talk / failure / compact / sleep / wait / ... directories are on disk but not states we use")
        self.assertEqual(out, set_lines(self.cfg))

    def test_the_picker_offers_it_after_the_built_in_sets_and_enter_binds_it(self):
        self.imported(); order = [i for i in LISTING if not i.startswith("bot-")] + ["kit"] + [i for i in LISTING if i.startswith("bot-")]
        self.config("", default=None); t = self.board(tmux=False); t.keys("j"); time.sleep(0.3); t.keys("a"); ov = wait(lambda: overlay(t.snap()), 3); self.assertTrue(ov, t.text())
        self.assertEqual([r["id"] for r in ov["rows"]], order, "built-in sets, then the user's, then the sprites")
        for _ in range(order.index("kit") + 1): t.keys("l"); time.sleep(0.04)
        for _ in range(len(order)):
            cur = [r["id"] for r in overlay(t.snap())["rows"] if r["marker"]]
            if cur == ["kit"]: break
            t.keys("h" if order.index(cur[0]) > order.index("kit") else "l"); time.sleep(0.12)
        self.assertEqual([r["id"] for r in overlay(t.snap())["rows"] if r["marker"]], ["kit"]); t.keys("Enter"); time.sleep(0.8)
        import json
        self.assertEqual(json.loads((self.env.state / "agmux" / "avatars.json").read_text()), {"pj-fallback": "kit"})

    def test_the_board_uploads_its_frames_as_the_3_2_crop_of_128_square(self):
        self.imported(); table, imgs = frame_table(self.env, self.cfg); kit = [k for k in table if k[0] == "kit"]
        self.assertEqual(len(kit), 14, "7 states x 2 frames; the other pi directories are not uploaded"); self.assertEqual({t28.IMG_DIMS(imgs[table[k]]["data"]) for k in kit}, {expected_upload_size(128, 128)})

    def test_a_bound_card_shows_the_imported_idle_frame(self):
        self.imported(); self.config('[avatars]\ndefault = "kit"\n'); table, _ = frame_table(self.env, self.cfg); t = self.board(tmux=True); raw = t.raw(); cs = t28.cards_of(raw)
        idle = {table[("kit", "idle", "idle.png")], table[("kit", "idle", "idle_blink.png")]}
        for name in ("pj-fallback", "charlie-30"): self.assertTrue({c[2] for c in t28.card_avatar(raw, t28.named(cs, name))} <= idle, name)


if __name__ == "__main__":
    unittest.main()
