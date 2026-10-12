"""A pane that asks for bracketed paste (CSI ?2004h) and shows the bytes it receives, repr()-style, one growing line per read.
Run as: sh -c 'stty raw -echo; exec python3 -u paste_probe.py'."""
import os, sys

sys.stdout.write("\x1b[?2004h")
sys.stdout.flush()
buf = b""
while True:
    b = os.read(0, 4096)
    if not b:
        break
    buf += b
    sys.stdout.write("\r\nGOT " + repr(buf.decode("utf-8", "replace")))
    sys.stdout.flush()
