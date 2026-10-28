// Package statusline is `agmux statusline` (wraps Claude Code's status line command and
// saves each payload for the board) and `agmux setup` (installs that wrapper in settings.json).
package statusline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kackerx/agmux/internal/i18n"
	"github.com/kackerx/agmux/internal/xdg"
)

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Run is `agmux statusline [-- <command>…]`: save stdin as $XDG_STATE_HOME/agmux/<session_id>.json,
// then feed it to `sh -c <command>` and pass its stdout and exit code through. Saving is best
// effort: the status line must keep working whatever happens to agmux's copy.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	payload, _ := io.ReadAll(stdin)
	save(payload)
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	command := strings.Join(args, " ")
	if command == "" {
		return 0
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(payload), stdout, stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		if code := exit.ExitCode(); code >= 0 {
			return code
		}
		return 1 // killed by a signal
	}
	fmt.Fprintf(stderr, "agmux statusline: %v\n", err)
	return 127
}

// save writes payload atomically (tmp + rename) if it is a JSON object with a safe session_id.
func save(payload []byte) {
	var p struct {
		SessionID any `json:"session_id"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	id, _ := p.SessionID.(string)
	if !sessionIDRe.MatchString(id) { // also keeps the path inside the state dir
		return
	}
	dir, err := xdg.StateDir()
	if err != nil || os.MkdirAll(dir, 0o755) != nil {
		return
	}
	writeAtomic(filepath.Join(dir, id+".json"), payload, 0o644)
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, perm)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

const agmuxCmd = "agmux statusline"

// Setup is `agmux setup [--write]`. It only ever touches statusLine.command (and adds
// statusLine.type when it creates statusLine); every other byte of settings.json keeps its value and order.
func Setup(args []string, stdout, stderr io.Writer) int {
	write := false
	switch {
	case len(args) == 1 && args[0] == "--write":
		write = true
	case len(args) != 0:
		fmt.Fprintln(stderr, "usage: agmux setup [--write]")
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "agmux setup: cannot find ~/.claude/settings.json: %v (set $HOME)\n", err)
		return 1
	}
	path := filepath.Join(home, ".claude", "settings.json")
	// settings.json is often a symlink into a dotfiles repo: edit the target, keep the link
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if path, err = filepath.EvalSymlinks(path); err != nil {
			fmt.Fprintf(stderr, "agmux setup: settings.json is a broken symlink: %v\n", err)
			return 1
		}
	}
	orig, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "agmux setup: %v\n", err)
		return 1
	}
	before, updated, err := setCommand(orig)
	if err != nil {
		fmt.Fprintf(stderr, "agmux setup: %s: %v; fix the file and run again\n", path, err)
		return 1
	}
	if before == agmuxCmd || strings.HasPrefix(before, agmuxCmd+" ") {
		fmt.Fprintln(stdout, i18n.T("setup.already_set", before))
		return 0
	}
	after := wrap(before)
	shown := before
	if shown == "" {
		shown = i18n.T("setup.none")
	}
	fmt.Fprintln(stdout, i18n.T("setup.command", shown, after))
	if !write {
		fmt.Fprintln(stdout, i18n.T("setup.dry_run"))
		return 0
	}
	out, err := updated(after)
	if err == nil {
		err = replaceFile(path, orig, exists, out)
	}
	if err != nil {
		fmt.Fprintf(stderr, "agmux setup: %v; settings.json was not changed\n", err)
		return 1
	}
	if exists {
		fmt.Fprintln(stdout, i18n.T("setup.written_bak", path))
	} else {
		fmt.Fprintln(stdout, i18n.T("setup.written_new", path))
	}
	return 0
}

// replaceFile replaces path with out, keeping a .bak of the original. The temp file is created first,
// so an unwritable directory fails before anything (backup included) is left behind.
func replaceFile(path string, orig []byte, exists bool, out []byte) error {
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}
	// rename only needs the directory writable; a read-only settings.json must still mean "no"
	if exists {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		f.Close()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings.json.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(out)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, perm)
	}
	if err == nil && exists {
		err = os.WriteFile(path+".bak", orig, perm)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// wrap turns the original command into `agmux statusline -- '<original>'`; the original is a
// shell string, so it is single-quoted for the sh -c that Run uses.
func wrap(before string) string {
	if before == "" {
		return agmuxCmd
	}
	return agmuxCmd + " -- '" + strings.ReplaceAll(before, "'", `'\''`) + "'"
}

type member struct {
	key string
	val json.RawMessage
}

// members decodes a JSON object keeping key order and each value's raw bytes.
func members(data []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var ms []member
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		ms = append(ms, member{t.(string), v})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON object")
	}
	return ms, nil
}

func encode(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // keep <, >, & as written
	enc.Encode(v)
	return bytes.TrimRight(b.Bytes(), "\n")
}

func object(ms []member) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(encode(m.key))
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// set replaces key's value or appends it.
func set(ms []member, key string, val json.RawMessage) []member {
	for i := range ms {
		if ms[i].key == key {
			ms[i].val = val
			return ms
		}
	}
	return append(ms, member{key, val})
}

// setCommand reads statusLine.command from settings.json (nil = no file) and returns a
// function that produces the file with it replaced.
func setCommand(orig []byte) (before string, updated func(string) ([]byte, error), err error) {
	var top []member
	if orig != nil {
		if top, err = members(orig); err != nil {
			return "", nil, err
		}
	}
	var sl []member
	for _, m := range top {
		if m.key != "statusLine" {
			continue
		}
		if sl, err = members(m.val); err != nil {
			return "", nil, errors.New("statusLine is not an object")
		}
		for _, f := range sl {
			if f.key == "command" && json.Unmarshal(f.val, &before) != nil {
				return "", nil, errors.New("statusLine.command is not a string")
			}
		}
	}
	return before, func(after string) ([]byte, error) {
		if sl == nil {
			sl = []member{{"type", encode("command")}}
		}
		top := set(top, "statusLine", object(set(sl, "command", encode(after))))
		var out bytes.Buffer
		if err := json.Indent(&out, object(top), "", "  "); err != nil {
			return nil, err
		}
		out.WriteByte('\n')
		return out.Bytes(), nil
	}, nil
}
