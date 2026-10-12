"""Raw-mode key echo for panes under test: shows every byte it receives like `cat -v` (^M, ^[, ^C ...) but decodes UTF-8 across read boundaries,
which BSD `cat -v` does not (it garbles a multi-byte character that arrives split). Output is appended (never rewritten) and wrapped by hand every
80 columns with a real newline, so long sessions stay comparable. Run as: sh -c 'stty raw -echo; exec python3 -u catv.py'."""
import codecs, os, sys

dec, col = codecs.getincrementaldecoder("utf-8")("replace"), 0
while True:
    b = os.read(0, 4096)
    if not b:
        break
    out = ""
    for ch in dec.decode(b):
        o = ord(ch)
        tok = "^" + chr(o + 64) if o < 32 else "^?" if o == 127 else ch
        out += tok
        col += len(tok)
        if col >= 80:
            out += "\r\n"; col = 0
    sys.stdout.write(out)
    sys.stdout.flush()
