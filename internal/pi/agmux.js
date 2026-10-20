// agmux-extension v1
// pi extension shipped by agmux (`agmux setup --write` installs it): every pi session that has a UI
// keeps <pi agent dir>/agmux-sessions/<pid>.json up to date for the board, and removes it on exit.
// Adapted from the pi extension of ccmux (github.com/epilande/ccmux, MIT).
//
// pi loads *.js extensions as they are, so this stays plain JS.

import { mkdir, rename, unlink, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

const dir = join(process.env.PI_CODING_AGENT_DIR || join(homedir(), ".pi", "agent"), "agmux-sessions");
const file = join(dir, `${process.pid}.json`);

export default function agmux(pi) {
  let session = null; // set at session_start: { sessionId, cwd, transcript, name }
  let agent = false; // between agent_start and agent_end: one whole prompt, every turn and tool call in it
  let prompts = 0; // blocking UI prompts open: pi's only way of waiting for you
  const tools = new Map(); // toolCallId -> toolName, the tools running now
  let status = "idle";
  let statusAt = Date.now();
  let chain = Promise.resolve(); // writes go one after the other: a tick can bring several events

  const call = (f) => {
    try {
      const v = f();
      return typeof v === "string" && v ? v : undefined;
    } catch {
      return undefined;
    }
  };

  function flush() {
    if (!session) return chain;
    const now = Date.now();
    const next = prompts > 0 ? "waiting" : agent ? "working" : "idle";
    if (next !== status) {
      status = next;
      statusAt = now;
    }
    const body = JSON.stringify({
      pid: process.pid,
      sessionId: session.sessionId,
      cwd: session.cwd,
      transcript: session.transcript,
      name: session.name,
      status,
      pendingTool: [...tools.values()].pop() ?? "",
      statusAt,
      updatedAt: now,
    });
    chain = chain
      .then(async () => {
        await mkdir(dir, { recursive: true });
        const tmp = `${file}.tmp.${now}`;
        await writeFile(tmp, body);
        await rename(tmp, file);
      })
      .catch((err) => console.error("[agmux-extension] write failed", err));
    return chain;
  }

  // a session without a UI (print mode) has nobody to look at the board
  const on = (name, handler) =>
    pi.on(name, (event, ctx) => {
      if (!ctx || !ctx.hasUI) return;
      return handler(event, ctx);
    });

  on("session_start", (_event, ctx) => {
    const sessionId = call(() => ctx.sessionManager.getSessionId());
    if (!sessionId) return;
    session = {
      sessionId,
      cwd: ctx.cwd,
      transcript: call(() => ctx.sessionManager.getSessionFile()),
      name: call(() => ctx.sessionManager.getSessionName()),
    };
    agent = false;
    prompts = 0;
    tools.clear();
    status = "idle";
    statusAt = Date.now();
    return flush();
  });

  on("session_info_changed", (event) => {
    if (!session) return;
    session.name = typeof event?.name === "string" && event.name ? event.name : undefined;
    return flush();
  });

  on("agent_start", () => {
    agent = true;
    return flush();
  });

  on("agent_end", () => {
    agent = false;
    tools.clear();
    return flush();
  });

  on("tool_execution_start", (event) => {
    tools.set(event.toolCallId, event.toolName);
    return flush();
  });

  on("tool_execution_end", (event) => {
    tools.delete(event.toolCallId);
    return flush();
  });

  on("ui_prompt_start", () => {
    prompts++;
    return flush();
  });

  on("ui_prompt_end", () => {
    prompts = Math.max(0, prompts - 1);
    return flush();
  });

  on("session_shutdown", async () => {
    session = null;
    await chain;
    try {
      await unlink(file);
    } catch (err) {
      if (err && err.code !== "ENOENT") console.error("[agmux-extension] unlink failed", err);
    }
  });
}
