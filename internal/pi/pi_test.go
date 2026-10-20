package pi

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionHasTheEvents(t *testing.T) {
	for _, ev := range []string{"session_start", "session_shutdown", "session_info_changed", "agent_start", "agent_end", "tool_execution_start", "tool_execution_end", "ui_prompt_start", "ui_prompt_end"} {
		if !bytes.Contains(Extension, []byte(`"`+ev+`"`)) {
			t.Errorf("no handler for %s", ev)
		}
	}
	if !bytes.HasPrefix(Extension, []byte("// agmux-extension v")) || !bytes.Contains(Extension, []byte("hasUI")) {
		t.Error("the version line or the hasUI guard is gone")
	}
}

func TestDir(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "/x/agent")
	if d, _ := Dir(); d != "/x/agent" {
		t.Errorf("env: %s", d)
	}
	if d, _ := SessionsDir(); d != "/x/agent/agmux-sessions" {
		t.Errorf("sessions: %s", d)
	}
	if p, _ := ExtensionPath(); p != "/x/agent/extensions/agmux.js" {
		t.Errorf("extension: %s", p)
	}
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("HOME", "/home/u")
	if d, _ := Dir(); d != "/home/u/.pi/agent" {
		t.Errorf("default: %s", d)
	}
}

func setup(t *testing.T, write bool) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	return Setup(write, &out, &errs), out.String(), errs.String()
}

// Setup says what it would do without --write, installs the extension with it, leaves a file that is
// the same, and does not overwrite one that is not; no pi directory, nothing to do.
func TestSetup(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	path := filepath.Join(dir, "extensions", "agmux.js")
	if code, out, _ := setup(t, true); code != 0 || !strings.Contains(out, "not found") {
		t.Errorf("no pi: %d %q", code, out)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("it made pi's directory")
	}
	os.MkdirAll(dir, 0o755)
	if code, out, _ := setup(t, false); code != 0 || !strings.Contains(out, "would install") || !strings.Contains(out, "dry run") {
		t.Errorf("dry run: %d %q", code, out)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a dry run wrote it")
	}
	if code, out, errs := setup(t, true); code != 0 || !strings.Contains(out, "installed "+path) || strings.Contains(out, "would") || errs != "" {
		t.Fatalf("write: %d %q %q", code, out, errs)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, Extension) {
		t.Error("what was written is not the extension")
	}
	if es, _ := os.ReadDir(filepath.Dir(path)); len(es) != 1 {
		t.Errorf("a temp file was left: %v", es)
	}
	if code, out, _ := setup(t, true); code != 0 || !strings.Contains(out, "already installed") {
		t.Errorf("again: %d %q", code, out)
	}
	os.WriteFile(path, []byte("// mine\n"), 0o644)
	if code, out, _ := setup(t, true); code != 0 || !strings.Contains(out, "differs") || !strings.Contains(out, "left alone") {
		t.Errorf("edited: %d %q", code, out)
	}
	if got, _ := os.ReadFile(path); string(got) != "// mine\n" {
		t.Errorf("an edited extension was overwritten: %q", got)
	}
	if !Installed() {
		t.Error("Installed")
	}
	os.Remove(path)
	if Installed() {
		t.Error("Installed without the file")
	}
}

// The extension, run by node with a pi that is a function: the file follows the status, the tool, the name; a
// session without a UI writes nothing; shutdown removes it.
func TestExtensionRuns(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ext.mjs"), Extension, 0o644)
	os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(`
import ext from "./ext.mjs";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
const handlers = {};
ext({ on: (n, h) => (handlers[n] = h) });
const sdir = join(process.env.PI_CODING_AGENT_DIR, "agmux-sessions");
const ctx = (hasUI = true) => ({ hasUI, cwd: "/w/p", sessionManager: { getSessionId: () => "sid-1", getSessionFile: () => "/w/p/s.jsonl", getSessionName: () => "first" } });
const snap = (label) => {
  let files = [];
  try { files = readdirSync(sdir); } catch {}
  const out = { label, files: files.length };
  const f = files.find((x) => x === process.pid + ".json");
  if (f) Object.assign(out, JSON.parse(readFileSync(join(sdir, f), "utf8")));
  console.log(JSON.stringify(out));
};
const fire = async (n, e = {}, c = ctx()) => { await handlers[n](e, c); };
await fire("session_start", {}, ctx(false)); snap("no ui");
await fire("session_start"); snap("started");
await fire("agent_start"); snap("working");
await fire("tool_execution_start", { toolCallId: "1", toolName: "bash" }); snap("tool");
await fire("tool_execution_start", { toolCallId: "2", toolName: "read" }); snap("two tools");
await fire("tool_execution_end", { toolCallId: "2" }); snap("one left");
await fire("ui_prompt_start"); snap("asks");
await fire("ui_prompt_end"); snap("answered");
await fire("session_info_changed", { name: "second" }); snap("renamed");
await fire("session_info_changed", {}); snap("name cleared");
await fire("ui_prompt_end"); await fire("ui_prompt_start"); snap("asks again, after an end too many"); await fire("ui_prompt_end");
await fire("agent_end"); snap("idle");
await fire("session_shutdown"); snap("gone");
`), 0o644)
	cmd := exec.Command(node, filepath.Join(dir, "run.mjs"))
	cmd.Env = append(os.Environ(), "PI_CODING_AGENT_DIR="+filepath.Join(dir, "agent"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var steps []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("%q: %v\n%s", l, err, out)
		}
		steps = append(steps, m)
	}
	by := map[string]map[string]any{}
	for _, s := range steps {
		by[s["label"].(string)] = s
	}
	check := func(label, status, tool, name string) {
		t.Helper()
		s := by[label]
		if s["status"] != status || s["pendingTool"] != tool || (s["name"] == nil && name != "" || s["name"] != nil && s["name"] != name) {
			t.Errorf("%s: %v, want %s %q %q", label, s, status, tool, name)
		}
	}
	if by["no ui"]["files"] != float64(0) {
		t.Errorf("a session without a UI wrote: %v", by["no ui"])
	}
	check("started", "idle", "", "first")
	check("working", "working", "", "first")
	check("tool", "working", "bash", "first")
	check("two tools", "working", "read", "first")
	check("one left", "working", "bash", "first")
	check("asks", "waiting", "bash", "first")
	check("answered", "working", "bash", "first")
	check("renamed", "working", "bash", "second")
	check("name cleared", "working", "bash", "")
	check("asks again, after an end too many", "waiting", "bash", "")
	check("idle", "idle", "", "")
	s := by["started"]
	if s["sessionId"] != "sid-1" || s["cwd"] != "/w/p" || s["transcript"] != "/w/p/s.jsonl" || s["pid"] == nil || s["statusAt"] == nil || s["updatedAt"] == nil {
		t.Errorf("fields: %v", s)
	}
	if by["working"]["statusAt"].(float64) < s["statusAt"].(float64) || by["tool"]["statusAt"] != by["working"]["statusAt"] {
		t.Errorf("statusAt moves with the status only: %v %v %v", s["statusAt"], by["working"]["statusAt"], by["tool"]["statusAt"])
	}
	if by["gone"]["files"] != float64(0) {
		t.Errorf("shutdown left the file: %v", by["gone"])
	}
}
