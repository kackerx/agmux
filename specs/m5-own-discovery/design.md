# M5 自家发现层：不再依赖 ccmux（方案，待用户过目）

用户 2026-10-20 拍板方向：能自己做的自己做，可参考 ccmux 的实现。起因 T65（ccmux 把 claude daemon 的备用进程列成会话）。本文基于只读盘点（ccmux 1.4.4 源码、Claude Code 2.1.296–2.1.304 落盘数据、agmux 调用点）。

## 1. 现在从 ccmux 拿什么

- `ccmux show --json` 每 2 s 一次（store.go:103）：id、summary、project、agentType、cwd、mainRepoRoot、gitBranch、status、attentionType、attentionState、pendingTool、tmuxPane、tmuxTarget、trackingMode、subagents[].status、nativeSessionId、logPath、lastPrompt / prompts、lastActivityAt、statusChangedAt。
- `ccmux restart <id>`（r）、`ccmux review <id>`（d，前台）、`ccmux kill <id>`（x）。

## 2. ccmux 是怎么算的（要点）

- 自带 daemon（127.0.0.1:2269），靠三个装进 `~/.claude/settings.json` 的 hook 脚本写 `~/.config/ccmux/session-pids/*.json`，再 watch `~/.claude/projects/**/*.jsonl` 跑状态机（tool_use → working；ExitPlanMode / AskUserQuestion → waiting；end_turn / turn_duration → idle），pane 靠 tty 匹配 `list-panes` 或 ps/lsof 爬进程树，兜底 `capture-pane` 抓 20 行按正则猜。
- summary = pane 标题去掉 spinner（所以后台会话没有 summary）。gitBranch / mainRepoRoot / project 靠 `git rev-parse`。
- 后台会话读 `~/.claude/daemon/roster.json` + `~/.claude/jobs/<short>/state.json`；roster 里每个 worker 都当会话，备用进程（dispatch.source = spare）也进来了 → T65 的根因。
- restart = SIGTERM pid → `tmux send-keys "claude --resume <id>" Enter`；kill = SIGTERM（后台 `claude stop`）；review = 到仓库根 `exec hunk diff --watch`（依赖外部 hunk）。

## 3. Claude Code 自己已经给了什么

- **`~/.claude/sessions/<pid>.json`**（2.1.296+，Claude Code 自己的活会话登记）：`pid、sessionId、cwd、kind(interactive|bg)、tmux("agmux # main:@14.%37")、name、nameSource、status(busy|waiting|idle|shell)、waitingFor("permission prompt"|"input needed")、statusUpdatedAt、procStart、version`；后台条目有 `jobId、agent`；**备用进程有 `spare:true`**。本机 28 个文件全部对应活 pid。这一个文件覆盖 show 的大半字段。
- jsonl 头部：last-prompt、custom-title、agent-name、agent-setting（T37 / T58 已经在读）。
- statusline 载荷（agmux statusline 已存）：session_id、transcript_path、cwd、session_name、model、effort、context_window、version；没有 pid、没有 tmux。
- 后台：`~/.claude/daemon/roster.json`、`~/.claude/jobs/<short>/state.json`（state / tempo / detail / intent / name / linkScanPath）。
- `claude agents --json` 能列活会话，但可能拉起 daemon，不用；直接读文件，零进程。

## 4. 替换方案（最少新代码）

| 能力 | 做法 |
|---|---|
| 发现 | glob `~/.claude/sessions/*.json`；`kill(pid,0)` 活着才算，`procStart` 防 pid 复用；`spare:true` 丢掉（T65 的过滤随之删除） |
| pane / tmux session | 解析 `tmux` 字段：最后一个 `.` 后的 `%N` 是 pane，`:@` 前是 session 名 |
| status / attention / statusChangedAt | busy→working、waiting→waiting、idle/shell→idle；waitingFor permission prompt→permission，否则 question；statusUpdatedAt |
| 后台会话 | `kind == "bg"`，id = jobId；状态 / logPath / 名字从 `jobs/<id>/state.json`（照 ccmux 的 deriveBackgroundState 约 15 行）；attach pane 照旧 list-panes 认领（T60） |
| 名字 | 登记的 `name`，空则 custom-title（T58 现成），再空项目名 |
| logPath | statusline 的 transcript_path，否则 `projects/<cwd 非字母数字转 ->/<sessionId>.jsonl` |
| pendingTool | 已读的 64 KB 尾部里最后一个未配对 tool_use（现成读法） |
| branch / mainRepoRoot / project | 尾部 gitBranch；worktree 用一次 `git rev-parse --git-dir --git-common-dir`，按 cwd 缓存 |
| subagents | 登记的 busy 大概率已含子代理在跑（**未验证**，T67 里先验）；不够再 stat `subagents/agent-*.jsonl` |
| context% / model / effort | 不变，statusline |
| r 重启 | SIGTERM pid → `tmux send-keys -t %N 'claude --resume <sessionId>' Enter`；后台 `claude respawn <id>` |
| x 结束 | SIGTERM；后台 `claude stop <id>` |
| d 审查 | 仓库根 `hunk diff --watch`，没有 hunk 退回 `git diff` 分页 |

## 5. 会失去什么

- 权限提示期间的工具名（Claude 批准后才写 tool_use；ccmux 2.1.209 起同样拿不到）。
- `capture-pane` 正则兜底、ccmux daemon 的推送（我们本来就 2 s 轮询）。
- 非 Claude 的 agent（codex / opencode）：agmux 变成只看 Claude。
- 旧版 Claude Code（< 2.1.296 不写登记）不显示。
- 风险：`sessions/*.json`、roster、jobs 都是 Claude Code 未文档化的内部格式；ccmux 本来也依赖后两者。缓解：登记文件解析失败只丢那一条并在底栏提示版本，不崩。

## 6. 任务切法（第十四批起）

- T67 store 换成读登记文件（含后台 + 备用过滤 + 名字 / 状态 / pane），Show 接口保留便于测试注入；`agmux show` 同源。
- T68 r / x / d 不走 ccmux。
- T69 e2e 夹具从假 ccmux 二进制改成假 `$HOME/.claude/sessions` 目录 + 假 jobs（23 个文件要改，tester 的大头）。
- T70 删 internal/ccmux、README 前置条件去掉 ccmux、CREDITS 保留对 ccmux 实现的致谢；ccmux 的 hook 脚本留在用户 settings.json 里由用户自己卸。

## 7. dev 预读提出的点（2026-10-20，T67 定稿时要落实）
- done 的来源：现在是 ccmux 的 attentionState（working→idle 且未在看那个 pane 时置 unread）。换登记后没有这个字段，done = status idle 且 statusUpdatedAt 新于 seen.json 里的已读记录（key 仍是 id + statusChangedAt → 改用 statusUpdatedAt）。这是最容易出行为差异的地方，T67 验收要单列：working→idle 变 done；预览 / 直通 / enter / m 之后变 idle；再次 working→idle 又 done；后台会话同样适用（ccmux 以前对后台从不置 unread，这是一个改善）。
- 子代理在跑时登记是否 busy：T67 先在真机只读验（找一个正在跑子代理的会话对比）；不是就 stat `subagents/agent-*.jsonl` 的 mtime。
- T65 的 spare() 和 ccmux.Session 的 LastPrompt / Prompts 随 T67 一起删；testdata/show.json 换成登记文件格式的夹具。
- pid 存活检查：每 2 s 约 30 次 kill(pid,0) + 读 JSON，可忽略。

## 8. 用户决定（2026-10-20）：codex 不要，pi 必须保留；来源做成策略抽象
- 用户主用 Claude Code + pi。ccmux 现在列出的 34 个会话里 32 claude、2 pi。
- 结构：store 里一个 `Source` 接口（列出该 agent 的活会话：id、pid、cwd、pane、name、status、attention、statusChangedAt、transcript、Background），两个实现 `claude`（登记文件）和 `pi`（待调查：ccmux 的 PiHookAdapter 怎么做、pi 的扩展 API 能否自己写一个登记文件）。Poller 把各 Source 的结果合并。codex / opencode 不实现，接口留着就是扩展点，不预埋代码。
- pi 调查进行中，结果补在第 9 节。

## 9. pi 来源（调查结论，2026-10-20）
- 装的是 `@earendil-works/pi-coding-agent` 1.1.0（`~/.pi` → `~/.config/.pi` 软链；@oh-my-pi 是另一个 agent）。pi 自己没有活会话登记；会话 jsonl 在 `~/.pi/agent/sessions/--<cwd 的 / 换 ->--/<ISO>_<uuid>.jsonl`，首行 `{"type":"session","id","cwd"}`，名字是 `session_info{name}` 条目，不记分支；pane 标题 `π - <name> - <dir>`。
- ccmux 对 pi 的做法：往 `~/.pi/agent/extensions/ccmux.js` 装一个扩展，按 session_start / agent_start / agent_end / session_shutdown 写 `~/.config/ccmux/session-pids/pi-<id>.json`（state 只有 idle / working，无 attention、无 pendingTool）；pane 靠 pid 往上爬到 pane_pid。**这台机器上这个扩展从未装过**，所以现在两个 pi 会话只是扫进程 + 读屏幕猜的，logPath / 状态全空。
- pi 扩展 API 有全部需要的事件：session_start / session_shutdown、session_info_changed(name)、agent_start / agent_end、tool_execution_start / end(toolName)、ui_prompt_start / end（pi 唯一的「等待」态：扩展弹对话框时）；ctx 给 cwd、sessionManager.getSessionId / getSessionFile / getSessionName、hasUI。
- **定稿**：agmux 自带一个几十行的 pi 扩展（照 ccmux 的 ccmux.js 写），由 `agmux setup --write` 装到 `~/.pi/agent/extensions/agmux.js`（和 statusline 一样可 dry-run），每个有 UI 的 pi 会话写 `~/.pi/agent/agmux-sessions/<pid>.json`：`{pid, sessionId, cwd, transcript, name, status: idle|working|waiting, pendingTool, updatedAt}`，shutdown 删文件，原子写。agmux 的 pi Source 读这个目录，`kill(pid,0)` 死的丢；pane = 从 pid 沿 ppid 爬到某个 pane 的 pane_pid（一次 `ps -axo pid,ppid` + `list-panes -a -F '#{pane_id}\t#{session_name}\t#{pane_pid}'`，每 2 s 一次，和 T60 的 list-panes 合并成一条）。分支 `git -C cwd branch --show-current`，按 cwd 缓存。模型 / 上下文：pi 没有 statusline，空。r = SIGTERM 后在 pane 里 `pi -c`（ccmux 的做法；dev 查 `pi --help` 有没有按 id 恢复的参数，有就用）；x = SIGTERM。
- 不做无扩展的兜底（扫进程猜状态）：没装扩展就不显示 pi，底栏提示一句 `pi: run agmux setup --write`（只在发现有 `pi` 进程而目录为空时）。现跑着的两个 pi 会话要重启后才会出现。
- 失去：对比 ccmux 几乎没有（ccmux 对 pi 本来就只有 idle / working），少了屏幕正则的限速检测。
