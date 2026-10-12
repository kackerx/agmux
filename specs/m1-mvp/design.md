# M1 MVP：看板浮窗 `agmux board`

全屏看板，在 tmux popup（用户的 fl-* 浮窗，约 232 列，底色 #221e1e）里打开。全局约定（选型、数据模型、测试约定）见 [../README.md](../README.md)。
下面是 M1 验收后的现状；「M1 验收改版」标注的是用户 2026-10-12 验收初版后改掉的地方。

## 数据源
### A. `ccmux show --json`【已实测】每 2s 轮询
数组，每项用到的字段：`id`、`summary`（会话名，可能为 null → 退回 `project`）、`agentType`、`cwd`、`project`、`mainRepoRoot`、`gitBranch`、
`status`（idle / working / waiting）、`attentionType`、`attentionState`、`pendingTool`、`tmuxPane`（background 模式下 null）、`trackingMode`、
`subagents[]`（agentId / status / pendingTool）、`nativeSessionId`、`logPath`、`lastActivityAt`、`updatedAt`。
### B. 状态栏透传 `agmux statusline`【M1 验收改版】
Claude Code 只在刷新状态栏时把带 `context_window.used_percentage` 的 payload 喂给 `statusLine.command`（hooks 里没有这份数据，jsonl 里没有模型窗口大小），所以这是唯一出口。agmux 自己提供包装，不依赖任何第三方状态栏脚本：
- `agmux statusline [-- <原命令> [args…]]`：读完 stdin，原子写（tmp + rename）到 `$XDG_STATE_HOME/agmux/<session_id>.json`（session_id 取 payload 的 `session_id`，就是 ccmux 的 nativeSessionId），再把同一份 payload 喂给原命令的 stdin，透传它的 stdout 和退出码；没给原命令就什么都不输出、退出 0。写文件失败不影响透传（状态栏不能因为 agmux 挂掉）。
- `agmux setup`：读 `~/.claude/settings.json` 的 `statusLine.command`，打印改动前后；`--write` 才真改（先备份到 `settings.json.bak`）：`X` → `agmux statusline -- X`；没有 statusLine 就设成 `agmux statusline`；已经是 agmux statusline 则不动。只改这一个键，其它内容原样保留。
- agmux 读文件：`context_window.used_percentage`（`rate_limits` 下也有同名字段，别取错）、`model.display_name`、effort。百分比**截断**取整（29.6 → 29）。effort 的字段名要拿第一份真实 payload 核实，在那之前不读；model.display_name 同样先核实。
- 细则：
  - 原命令是 Claude Code 的 `statusLine.command`，本身是 shell 字符串。`agmux statusline -- X` 只认 `--` 后**一个**参数（多个就按空格拼起来），用 `sh -c X` 跑；setup 改写时把 X 用单引号包起来（内部 `'` 按 `'\''` 转义），这样管道、`$HOME` 这些都留给 agmux 里的那层 sh。
  - stdin 不是 JSON 或没有 `session_id`：不写文件，照样透传，退出码跟原命令（没原命令则 0）。
  - 原命令起不来：文件照写，stderr 一行 `agmux statusline: <错误>`，退出 127。
  - `agmux setup` 输出固定三行：`statusLine.command:` / `  before: <原值，无则 (none)>` / `  after:  <新值>`，然后一行 `dry run: add --write to apply` 或 `written (backup: <路径>)`；已经是 agmux statusline 时输出 `already set: <值>` 退出 0。
  - `--write`：settings.json 不存在 → 新建只含 statusLine 的文件；JSON 损坏或没写权限 → stderr 报错、退出 1、原文件不动。
  - settings.json 是软链接（用户真机就是，指向 dotfiles 仓库）：解析到目标文件再写，软链接本身保留，`.bak` 放在目标文件旁边。
  - `session_id` 只接受 `[A-Za-z0-9_-]+`，否则当作没有 session_id（不写文件），绝不写到状态目录之外。
- 解析失败的文件按不存在处理；background 会话和非 Claude 的 agent 没有状态栏，永远 `--`。文件 M1 不清理。
### C. jsonl 兜底（只取模型）
文件可达 16MB，不能整读：从末尾读最后 64KB，倒序找最后一条 `"type":"assistant"`，取 `message.model`。
找不到（真机约 8%：末尾是大 attachment 或超长 assistant 行）就沿用该会话上一次轮询到的模型，store 里按会话 ID 缓存；首次就找不到显示空。
不从 jsonl 推算上下文百分比（各模型窗口大小不同，算不准）；B 没接上时百分比显示 `--`，进度条全空。

## 头像
- `internal/avatar`：一组手绘 sprite，**6 像素宽 × 4 像素高**（半块字符渲染成 6 列 × 2 行；第二轮验收：4×4 太小，且要和上下文条同宽；M1 的 6×6 版用户嫌喧宾夺主，M1 验收改版改小），每个有 `ID`（如 `bot-01`）和 `Name`。6 个，像素风。底边一行像素是 agentType 色带（claude #ffb86c、codex #50fa7b、其它 #6272a4）。
- agent 图标放在名字前面，和色带同色：claude  U+EC82（nf-cod-claude）、codex  U+EC81（nf-cod-openai）、其它  U+EC20（nf-cod-robot）。Nerd Fonts 3.5 起才有前两个，用户的 Maple Mono NF CN 已核实有。
- 分配：`Bindings` 接口 `Get(sessionID) (id string, ok bool)` / `Set(sessionID, id)`，M1 实现存 `$XDG_STATE_HOME/agmux/avatars.json`；没绑定时按会话 ID 的 hash 取模默认分配。M1 不做选头像 UI。
- 眼睛表达状态，优先级从高到低：需要处理（`attentionType` 非空或 status waiting）闪红 → working 扫动 → done 正常 → idle 变暗。
- `avatars.json` 损坏：绑定按全无处理，错误文案（带路径和修复提示）显示在底栏，不退出。

## 布局与卡片【定】
```
 agmux · 13 sessions                                                          ← 顶栏：总数（右侧预留给 5h 限额，M1 不显示）
 ▾ ~/.config (2)               ▾ ~/ProjectsFolder/…/rename-flag (3)    ▾ ~/proj/shopcart-dsl (1)
 ╭─ card ─╮                    ╭─ card ─╮                              ╭─ card ─╮
 ╭─ card ─╮                    ╭─ card ─╮
                               ╭─ card ─╮
 ▾ ~/proj/sqlmux (5)           ▾ ~/proj/sqlmux-e2e (1)
 …
 j/k 卡片 · h/l 跨列 · enter 跳转 · f 折叠 · H 交接 · s 发送 · r 重启 · x 杀 · d review · q 退出   ← 底栏，ccmux 同款
```
- 排序固定，只由数据决定，轮询不会让卡片来回跳：组按 GroupKey（cwd 字符串）字典序；组内按会话名字典序，同名按 id。`ccmux show --json` 的数组顺序不参与排序。
- 折叠：`f` 收起 / 展开焦点所在的组；收起的组只剩标题行 `▸ ~/路径 (n)`，仍占一个列位、标题可被焦点选中（j/k/h/l 把它当一张高度 1 的卡片），再按 `f` 或 enter 展开。折叠状态只在本次运行内记住。
- 列 = GroupKey（cwd）。列最小宽 **40**（用户 232 列的浮窗 → 5 列各 44；200 列 → 4 列各 48），列数 = 终端宽度能放几个 40（含 2 列间距），各列均分撑满；放不下的组折到下一行，不横向滚动。组内卡片竖排。
- 列标题沿用 ccmux：`▾ ~/缩写路径 (n)`，路径黄色（HUD 目录段同色）、`(n)` 灰色，超宽时中间用 … 截断。
- 卡片 = 列宽 × 5 行（边框 2 + 正文 3），圆角边框（44 列示例）：
```
╭──────────────────────────────────────────╮
│ ▄▀▀▀▀▄   mods-tmux          ⏸ idle  2h │   头像 6×4（6 列 × 2 行）· agent 图标 · 名（洋红）· 状态 · 上次活动相对时间
│ ▀▄▄▄▄▀  ⑂ master            Opus 5.5·hi │   分支（橙红，可截断）· 模型（青）·effort（紫），靠右
│ ■■■□□□ 42%         ⚇ 2 agents  ⚙ 1 shell │   上下文条 6 格顶在头像下面 · 子 agent 数 · 后台 shell 数
╰──────────────────────────────────────────╯
```
- 窄时让位顺序：先截分支，再截名字；状态和时间永远在。
- 字段颜色和用户的 claude-hud 段一一对应：名洋红、模型青、effort 紫、目录黄、分支橙红。
- 状态，照 ccmux 1.4.3 的 `getEffectiveStatus` 换算后再显示：原始 idle 且有子 agent 在 working / waiting → `⏵ working · agents`（不算 done，眼睛扫动）；原始 working → `⏵ working`，有 pendingTool 接 pendingTool，不接 agents；`⏸ idle` 蓝灰；`● done` 绿 = 原始 idle、无子 agent 在跑、`attentionState` 非空（"unread" 或 "read" 都算，ccmux 同样）；`⏳ waiting` 红 = status waiting（含 plan_approval，不像 ccmux 那样用青色）。**边框色 = 状态色**（working 橙、done 绿、idle 暗灰 #585b70、需要处理 / waiting 红）。焦点卡片边框换成 **#ff8ebe** 粗线（lipgloss ThickBorder），盖掉状态色——第二轮验收：只换粗线看不出焦点在哪。不单独拎出需要处理的会话。
- 换算放在 store 里做一次，view 只读结果。
- 底栏或消息行的错误只取第一行（ccmux 的 stderr 常是多行），总行数永远等于终端高度。
- 上下文条：■（U+25A0）**6 格**，和头像同宽、顶在头像正下方；百分比数字跟在后面。空格暗灰 #585b70；<30 绿 #a6e3a1，30–60 黄 #f9e2af，≥60 红 #ff6188。未知时 6 格全暗 + `--`。格数四舍五入到 6 格（29% → 2 格，数字仍显示截断后的 29）。
- 最近 prompt 那一行 M1 验收改版去掉（初版曾有，用户嫌挤；`lastPrompt` / `backgroundDetail` 的取行代码一并删，M2 的预览窗口另起）。
- 子 agent / shell 为 0 时该段不显示。
- PR 号（`branchPRs`）M1 不显示：真机全是 null / []，看到真实元素再接。
- 卡片里不画按钮；所有操作走底栏键位。鼠标：点卡片 = 选中，再点 = 跳转。
- 紧凑变体（3 行，给 M2 sidebar）：M1 不实现，但卡片渲染函数接受「高度」参数预留。

## 交互
- 焦点卡片边框高亮；方向键 / hjkl 移动（j/k 列内，h/l 跨列），鼠标点卡片 = 选中。
- enter 或双击 / 单击已选中的卡片：`ccmux switch <id>` 然后退出 TUI（popup 关掉才能看到目标 pane）。
- `H`：`ccmux send <id> /handoff`（agent 正在 working 时会排到下一轮，这是 ccmux 的行为）。
- `r` = `ccmux restart <id>`，`x` = `ccmux kill <id>`（按 y 执行，其它键取消），`d` = `ccmux review <id>`——它是交互式 TUI（内部起 hunk），用 `tea.ExecProcess` 把终端交给它，退出后回看板；`s` = 提示输入一行后 `ccmux send <id> <text>`。
- `f` 折叠 / 展开焦点所在的组（见「布局与卡片」）。
- `q` / esc 退出。`ccmux` 子命令的 stderr 显示在底栏一行，不弹窗。
- `tmuxPane` 为 null（background）的会话：switch / send 置灰，底栏提示。
