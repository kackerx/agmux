# Built-in image sets: sources and licenses

The pictures of the first group are MIT-licensed, and each was checked in its source repository. The second and third groups were made for this project and have no outside license; see their notes.

## aza_choi, cipher, default, forge, moss, nova, oana, quill, red, sprocket

- Source: https://github.com/testy-cool/pi-agent-portrait (`emotes/<name>/`)
- Author: testy-cool, based on pi-emote by Eiji Onchi (cgxeiji)
- License: MIT, Copyright (c) 2026 Eiji Onchi and testy-cool (`LICENSE-pi-agent-portrait`)
- `aza_choi` comes from pi-emote, https://github.com/shennguyenrs/pi-emote (MIT, Copyright (c) 2026 Eiji Onchi, `LICENSE-pi-emote`), as shipped in pi-agent-portrait.
- Used: the idle, think, read, write, tool, hi and success directories, without the alternate takes (`*_v2.png` and so on); the other states pi-agent-portrait has (talk, failure, compact, bash, search, heard, error, sleep, wait, interrupted) are not used. `aza_choi_nobg`, the `ascii*` sets, `test` and `_unused` are not included.
- Changes: scaled from 128 × 128 to 64 × 64 (2 × 2 averaging).

## developer, researcher, operator, tester, designer

- Source: made for this project with GPT in the pi-agent-portrait format (one directory per state), by the project owner.
- Used: the idle, think, read, write, tool, hi and success directories, without the alternate takes.
- Changes: each frame cut to its centered 3:2 part (126 × 84 from 128 × 128) by `tools/embedsets`; re-run it to rebuild these from the originals.

## naruto, sasuke, sakura, kakashi, itachi

- Source: made for this project with GPT in the pi-agent-portrait format, by the project owner. They are fan art of characters from Naruto (Masashi Kishimoto, Shueisha, Pierrot), who are not covered by any license given here.
- Used and changed: as above.
