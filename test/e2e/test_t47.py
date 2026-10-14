"""T47 `agmux board --stay` in an ordinary pane stays too: enter switches the client to the card's pane and the board keeps running where it is (nothing is detached: there is no popup); q / esc end it there (no popup to hide);
the footer says `q quit` with no popup and `q hide · Q quit` while a popup client is attached to the board's session, following a popup that comes and goes within a second or two; outside tmux --stay says `--stay only works
inside tmux` and runs as a plain board. T44's popup flow is test_t44 (unchanged). Spec: specs/m3-config/tasks.md T47. Real tmux, two servers, see test_t44.Real."""
import unittest
from lib import *
from test_t44 import Real
from test_t3 import last_text_row

W = 230  # the key line is ~167 wide


class Plain(Real):
    def test_enter_in_an_ordinary_pane_moves_the_client_and_the_board_stays_on_the_same_card(self):
        self.start_board(session="work2"); uc = self.terminal("u", "work2"); self.press("l"); self.assertEqual(self.focused("work2"), ["bravo-29"]); self.press("Enter")
        self.assertTrue(wait(lambda: self.user_session(uc) == "tgt2", 6), f"the client is on bravo-29's pane: {self.clients()}"); self.assertTrue(self.alive("work2"), "the process stays"); self.assertEqual([c["name"] for c in self.clients()], [uc], "nothing was detached")
        tm("switch-client", "-c", uc, "-t", "work2"); time.sleep(0.8); self.assertEqual(self.user_session(uc), "work2"); self.assertEqual(self.focused("work2"), ["bravo-29"], "back on the board: the focus is on the card it was on"); self.assertTrue(self.alive("work2"))
        self.press("h", "Enter"); self.assertTrue(wait(lambda: self.user_session(uc) == "tgt", 6), "and it jumps again"); self.assertTrue(self.alive("work2"))

    def test_q_and_esc_end_the_board_in_an_ordinary_pane(self):
        for key in ("q", "Escape"):
            with self.subTest(key=key):
                sess = f"plain-{key}"; self.start_board(session=sess); uc = self.terminal("u" + key[:1], sess); self.press(key); time.sleep(1.0 if key == "Escape" else 0)
                self.assertTrue(wait(lambda: tm("has-session", "-t", sess).returncode != 0, 6), f"{key}: no popup to hide, so it quits"); self.assertEqual(self.user_session(uc), None, "the session with its only pane is gone")

    def test_capital_q_ends_it_too(self):
        self.start_board(session="plainQ"); self.terminal("uQ", "plainQ"); self.press("Q", sess="uQ"); self.assertTrue(wait(lambda: tm("has-session", "-t", "plainQ").returncode != 0, 6))

    def test_enter_with_no_client_at_all_leaves_the_board_running(self):
        self.start_board(); self.assertEqual(self.clients(), []); tm("send-keys", "-t", "agboard", "Enter"); time.sleep(1.0); self.assertTrue(self.alive())


class FooterLive(Real):
    def bar(self, session="agboard"): return last_text_row(self.screen(session))

    def test_no_client_no_popup_says_q_quit(self):
        self.start_board(w=W); time.sleep(1.5); bar = self.bar(); self.assertIn("q quit", bar); self.assertNotIn("hide", bar)

    def test_an_ordinary_pane_with_only_the_users_client_says_q_quit(self):
        self.start_board(session="work3", w=W); uc = self.terminal("u", "work3", w=W + 10); time.sleep(1.5); bar = self.bar("work3"); self.assertIn("q quit", bar, bar); self.assertNotIn("hide", bar)

    def test_a_real_popup_makes_it_q_hide_and_q_quit_and_closing_it_takes_it_back_within_two_seconds(self):
        self.start_board(w=W); uc = self.terminal(w=W + 10); time.sleep(1.5); self.assertNotIn("hide", self.bar(), "no popup yet")
        p = self.popup(uc, w=W); self.assertTrue(wait(lambda: "q hide · Q quit" in self.bar(), 2.5), f"{self.bar()}")
        self.press("q"); self.assertTrue(self.closed(p)); self.assertTrue(wait(lambda: "q quit" in self.bar() and "hide" not in self.bar(), 2.5), self.bar()); self.assertTrue(self.alive())
        p2 = self.popup(uc, w=W); self.assertTrue(wait(lambda: "q hide · Q quit" in self.bar(), 2.5), "and again when it comes back")

    def test_the_popup_present_from_the_start_is_known_without_a_key(self):
        self.start_board(w=W); uc = self.terminal(w=W + 10); p = self.popup(uc, w=W); self.assertTrue(wait(lambda: "q hide" in self.bar(), 2.5), self.bar())
        self.assertTrue(self.alive()); self.assertEqual(self.focused() != [], True)

    def test_the_plain_board_without_stay_always_says_q_quit(self):
        self.start_board(args="", w=W); uc = self.terminal(w=W + 10); p = self.popup(uc, w=W); time.sleep(2.0); bar = self.bar(); self.assertIn("q quit", bar); self.assertNotIn("hide", bar)


if __name__ == "__main__":
    unittest.main()
