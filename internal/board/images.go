package board

import (
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/kackerx/agmux/internal/avatar"
	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/store"
)

// useImages: image sets are drawn as pictures (avatar.Placeholder) instead of pixel sprites. Run
// sets it once, after the pictures are uploaded.
var useImages bool

// avatarFace is the avatar's Height/2 lines of avatar.Width cells for a card: a picture of the
// session's AvatarState, or a sprite in the mood.
func avatarFace(s store.Session, mood avatar.Mood, frame int) []string {
	if set, ok := avatar.SetByID(s.AvatarID); ok && useImages {
		return avatar.Placeholder(set.FrameID(s.AvatarState, frame+blinkOffset(s.AvatarKey())))
	}
	return strings.Split(avatar.Render(avatar.SpriteFor(s.AvatarID), s.AgentType, mood, frame), "\n")
}

func isSet(id string) bool { _, ok := avatar.SetByID(id); return ok }

// avatarName is what the board calls an avatar: a sprite's name, an image set's ID.
func avatarName(id string) string {
	if sp, ok := avatar.ByID(id); ok && !isSet(id) {
		return sp.Name
	}
	return id
}

// gridFace is an avatar as the picker shows it: a picture's resting face (idle's first frame), a sprite.
func gridFace(id, agentType string) []string {
	if set, ok := avatar.SetByID(id); ok && useImages {
		return avatar.Placeholder(set.FrameID("idle", 0))
	}
	return strings.Split(avatar.Render(avatar.SpriteFor(id), agentType, avatar.Normal, 0), "\n")
}

// blinkOffset is a card's own 0..7 ticks added to the frame count, so that the idle cards don't all
// blink in the same tick: it comes from the avatar key, which doesn't change while the board runs.
func blinkOffset(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() % 8)
}

// kittyTerm: the TERM values whose terminals draw kitty graphics with Unicode placeholders.
func kittyTerm(t string) bool { return t == "xterm-kitty" || t == "xterm-ghostty" }

// nestedTerm: a client whose TERM is tmux's or screen's is a tmux inside something (a popup that
// attaches to another session), not the terminal that stores pictures.
func nestedTerm(t string) bool { return strings.HasPrefix(t, "tmux") || strings.HasPrefix(t, "screen") }

// ImageMode is whether avatars are drawn as pictures, and the one line to say on the board's bottom
// row. In tmux, Client and TTY are the client of the real terminal: the pictures are uploaded to its tty.
type ImageMode struct {
	On          bool
	Note        string
	Client, TTY string
}

// DetectImages is imageMode for the real tmux and environment. It also tells the avatar package
// whether a hash may pick image sets, so call it before the first poll.
func DetectImages(cfg *config.Config) ImageMode {
	img := imageMode(realRunner{}, os.Getenv, cfg.AvatarRenderer)
	avatar.HashImages = img.On
	return img
}

// imageMode decides whether avatars are drawn as pictures, from [avatars] renderer: "pixel" never;
// "auto" when the terminal is kitty or ghostty; "image" skips the terminal check. In tmux the
// terminal is the TERM of the client chosen as for a jump (not $TERM_PROGRAM, which in tmux is
// whatever started the server); when that client is itself a tmux (a popup that attaches to
// another session) it is the most recently active client that is not. Pictures then go to that
// client's tty, so allow-passthrough, nesting and whether anything is attached yet do not matter.
// Note is the reason for a fallback, for the board's bottom row.
func imageMode(r Runner, getenv func(string) string, renderer string) ImageMode {
	if renderer == "pixel" || len(avatar.Sets) == 0 {
		return ImageMode{}
	}
	force := renderer == "image"
	if getenv("TMUX") == "" {
		return ImageMode{On: force || kittyTerm(getenv("TERM")) || getenv("TERM_PROGRAM") == "ghostty"}
	}
	pc, err := chooseClient(r, getenv)
	if err != nil {
		return ImageMode{On: force, Note: noClient + hintPixel(force)}
	}
	var c tmuxClient
	for _, o := range pc.clients {
		if o.name == pc.name {
			c = o
		}
	}
	if nestedTerm(c.termname) {
		best := -1
		for _, o := range pc.clients {
			if o.termname != "" && !nestedTerm(o.termname) && o.activity > best {
				c, best = o, o.activity
			}
		}
		if best < 0 && force { // nothing but tmux clients: no terminal to write to
			return ImageMode{On: true, Note: "avatars: every tmux client is itself a tmux, no terminal to draw pictures through" + hintPixel(force)}
		}
	}
	if !force && !kittyTerm(c.termname) {
		return ImageMode{Note: fmt.Sprintf("avatars: pixel; tmux client %s is %q, not kitty or ghostty%s", c.name, c.termname, hintPixel(force))}
	}
	if c.tty == "" {
		c.tty = c.name
	}
	return ImageMode{On: true, Client: c.name, TTY: c.tty}
}

// noClient is the note when list-clients gave nothing to ask.
const noClient = "avatars: no tmux client found to draw pictures through (tmux list-clients is empty or failed)"

func hintPixel(force bool) string {
	if force {
		return "; trying tmux passthrough"
	}
	return ` (renderer = "image" forces pictures, "pixel" hides this note)`
}

// openTTY opens a client's tty for writing; tests replace it.
var openTTY = openClientTTY

// ttyStall is how long a tty may take nothing at all before the upload gives up on it: the outer
// terminal has stopped reading, and the board must still start.
var ttyStall = 2 * time.Second

// openClientTTY opens only a device (a client_name that is not a tty, "client-123", would otherwise
// name a file in the current directory), without blocking and without becoming its controlling terminal.
func openClientTTY(path string) (io.WriteCloser, error) {
	if !strings.HasPrefix(path, "/dev/") {
		return nil, fmt.Errorf("%q is not a device", path)
	}
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	return ttyWriter{fd}, nil
}

// ttyWriter writes to a non-blocking fd: a full tty is waited out while it keeps taking bytes, and
// an error (a stall) when it takes none for ttyStall. (os.File would wait for it for ever.)
type ttyWriter struct{ fd int }

func (w ttyWriter) Write(p []byte) (done int, err error) {
	last := time.Now()
	for done < len(p) {
		n, err := syscall.Write(w.fd, p[done:])
		if n > 0 {
			done, last = done+n, time.Now()
		}
		switch {
		case n > 0 && err == nil, err == syscall.EINTR:
		case err == syscall.EAGAIN, err == nil: // nothing taken (err == nil with n == 0 as well): wait, not spin
			if time.Since(last) > ttyStall {
				return done, fmt.Errorf("the terminal stopped reading (%v with no progress)", ttyStall)
			}
			time.Sleep(time.Millisecond)
		default:
			return done, err
		}
	}
	return done, nil
}

func (w ttyWriter) Close() error { return syscall.Close(w.fd) }

// uploadImages sends the pictures where the terminal that stores them can be reached: to the tty of
// the tmux client in the real terminal, or, if that cannot be opened or written, through tmux's
// passthrough (one tmux layer, an attached client and allow-passthrough on); outside tmux to
// stdout. note is img.Note, or what to say instead when it fell back.
func uploadImages(img ImageMode, r Runner) (note string, err error) {
	if os.Getenv("TMUX") == "" {
		return img.Note, avatar.Upload(os.Stdout, avatar.Sets, avatar.ToTerminal)
	}
	note = img.Note
	if img.TTY != "" {
		f, oerr := openTTY(img.TTY)
		if oerr == nil {
			oerr = avatar.Upload(f, avatar.Sets, avatar.ToTTY)
			f.Close()
			r.Tmux("refresh-client", "-t", img.Client) // if tmux's own drawing was cut into, it draws the screen again
			if oerr == nil {
				return note, nil
			}
		}
		note = fmt.Sprintf("avatars: cannot write to %s (%v); using tmux passthrough, which needs allow-passthrough on", img.TTY, oerr)
	}
	return note, avatar.Upload(os.Stdout, avatar.Sets, avatar.ToPassthrough)
}
