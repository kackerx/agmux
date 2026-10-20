# Credits

## ccmux

[ccmux](https://github.com/epilande/ccmux) (MIT, Copyright (c) 2026 epilande) is the tool agmux was first built on, and the way it finds and reads coding agents' sessions is what agmux's own discovery layer was adapted from, once agmux stopped calling it:

- how a session is found (the files Claude Code keeps, the tmux pane by process), and how Claude Code's background jobs turn into a status (`internal/store/claude.go`);
- the pi extension that tells a running pi session to the outside (`internal/pi/agmux.js`).

agmux has no code of ccmux's in it beyond the ideas and that extension, which is a rewrite of ccmux's.

## Pictures

The built-in avatar pictures have their own sources and licenses: [internal/avatar/sets/CREDITS.md](internal/avatar/sets/CREDITS.md).
