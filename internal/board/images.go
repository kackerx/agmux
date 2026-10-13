package board

import (
	"os"
	"strings"

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
		return avatar.Placeholder(set.FrameID(s.AvatarState, frame))
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

// kittyTerm: the TERM values whose terminals draw kitty graphics with Unicode placeholders.
func kittyTerm(t string) bool { return t == "xterm-kitty" || t == "xterm-ghostty" }

// ImageMode is whether avatars are drawn as pictures, and the one line to say on the board's bottom row.
type ImageMode struct {
	On   bool
	Note string
}

// DetectImages is imageMode for the real tmux and environment. It also tells the avatar package
// whether a hash may pick image sets, so call it before the first poll.
func DetectImages(cfg *config.Config) ImageMode {
	on, note := imageMode(realRunner{}, os.Getenv, cfg.AvatarRenderer)
	avatar.HashImages = on
	return ImageMode{on, note}
}

// imageMode decides whether avatars are drawn as pictures, from [avatars] renderer: "pixel" never;
// "auto" when the terminal is kitty or ghostty (in tmux: the attached client's TERM, the client
// chosen as for a jump; not $TERM_PROGRAM, which in tmux is whatever started the server) and, in
// tmux, allow-passthrough is on; "image" skips the terminal check but still needs the passthrough,
// and says so when it falls back. note is that one line for the board's bottom row.
func imageMode(r Runner, getenv func(string) string, renderer string) (on bool, note string) {
	if renderer == "pixel" || len(avatar.Sets) == 0 {
		return false, ""
	}
	force := renderer == "image"
	if getenv("TMUX") == "" {
		return force || kittyTerm(getenv("TERM")) || getenv("TERM_PROGRAM") == "ghostty", ""
	}
	if !force {
		pc, err := chooseClient(r, getenv)
		if err != nil {
			return false, ""
		}
		if term, err := r.Tmux("display-message", "-p", "-c", pc.name, "#{client_termname}"); err != nil || !kittyTerm(strings.TrimSpace(term)) {
			return false, ""
		}
	}
	if pt, err := r.Tmux("show-options", "-gv", "allow-passthrough"); err != nil || strings.TrimSpace(pt) == "off" {
		if force {
			return false, "avatars: renderer = \"image\" needs tmux allow-passthrough on (tmux set -g allow-passthrough on); showing pixel avatars"
		}
		return false, ""
	}
	return true, ""
}
