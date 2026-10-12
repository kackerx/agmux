# agmux 设计（S1：看板浮窗）

agmux = 基于 ccmux 的 TUI，用来看和管理 tmux 里跑着的 coding agent。
S1 只做 `agmux board`：全屏看板，在 tmux popup（fl-* 浮窗，约 180–200 列，底色 #221e1e）里打开。

## 技术选型【定】
- Go（go 1.26）+ bubbletea v1 + lipgloss v1；模块名 `github.com/kackerx/agmux`；`go install` 到 ~/go/bin，和 ~/proj/hudiff 一个路数。
- 头像用真彩半块字符 ▀▄ 画（1 列 = 1 像素宽，半行 = 1 像素高），不用 kitty 图形协议。
- 不自己重写 ccmux 已有能力：跳转 / 重启 / 杀 / 审查 / 发送都 exec `ccmux <子命令>`。
- 路径不写死 /Users/xxx，用 `os.UserHomeDir` / `$XDG_STATE_HOME`。

## 数据源
### A. `ccmux show --json`【已实测】每 2s 轮询
数组，每项用到的字段：`id`、`summary`（会话名，可能为 null → 退回 `project`）、`agentType`、`cwd`、`project`、`mainRepoRoot`、`gitBranch`、
`status`（idle / working / …）、`attentionType`、`pendingTool`、`tmuxPane`（background 模式下 null）、`trackingMode`、
`subagents[]`（agentId / status / pendingTool）、`nativeSessionId`、`logPath`、`lastActivityAt`、`updatedAt`、`backgroundDetail`。
### B. HUD 状态文件 `$XDG_STATE_HOME/agmux/<session_id>.json`（默认 ~/.local/state/agmux/）【待接线】
Claude Code 每次刷新状态栏把 payload JSON 喂给 ~/.config/bin/claude-hud；在那脚本里加一行 `printf '%s' "$payload" > …/<session_id>.json` 落盘（这改动在 ~/.config 仓库，要用户批准）。
agmux 按 `nativeSessionId` 找文件，取：`context_window.used_percentage`（注意取紧跟 `remaining_percentage` 的那个，rate_limits 里有同名字段）、模型、effort、`session_name`。
写入方用 `printf > tmp && mv` 原子落盘，但 agmux 读到解析失败的文件仍按不存在处理，不报错。
除 used_percentage / session_name 外，其余字段名未核实：拿到第一份真实 payload 再定，dev 先按文件不存在处理。
### C. jsonl 兜底（只取模型）
文件可达 16MB，不能整读：从末尾读最后 64KB，倒序找最后一条 `"type":"assistant"`，取 `message.model`。
不从 jsonl 推算上下文百分比（各模型窗口大小不同，算不准）；B 没接上时百分比显示 `--`，进度条全空。

## 数据模型
```go
type Session struct {
    ID, Name, AgentType, CWD, GroupKey, Branch string
    Status, Attention, PendingTool, TmuxPane string
    Model, Effort string; ContextPct int   // ContextPct<0 = 未知
    Subagents []Subagent; Shells int        // S1 Shells 固定 0（S3 的 mod 才能数）
    AvatarID string; LastActivity time.Time
}
```
- `GroupKey` 由一个 `func(Session) string` 算，S1 = CWD；以后换 project / mainRepoRoot 只改这个函数。
- 角色、通信边、Postgres 持久化：S1 不加字段，但 `Session` 和 `Store` 是以后唯一要改的地方。
- `Store` 接口：`Sessions() []Session`，S1 实现 = 轮询 A + 合并 B/C 的内存实现。

## 头像
- `internal/avatar`：一组手绘 sprite（8 像素宽 × 6 像素高 = 8 列 × 3 行），每个有 `ID`（如 `bot-01`）和 `Name`。S1 先画 6 个，像素风，参考 Claude Code 吉祥物。
- 分配：`Bindings` 接口 `Get(sessionID) (id string, ok bool)` / `Set(sessionID, id)`，S1 实现存 `$XDG_STATE_HOME/agmux/avatars.json`；没绑定时按会话 ID 的 hash 取模默认分配。S1 不做选头像 UI。
- 眼睛表达状态：working 时每帧左右扫动；需要处理（`attentionType` 非空）时闪红；idle 时变暗。

## 布局与卡片
- 列 = GroupKey（cwd）。列宽固定 44 列（卡片 42 + 边距）；按终端宽度算每行能放几列，放不下折到下一行，不横向滚动。列标题是 cwd 的最后一段（~ 开头的短路径）。
- 卡片 42 列 × 6 行，圆角边框，颜色与 claude-hud 一致（段分隔 ` | ` 灰 #585b70，Nerd Font 图标）：
```
╭────────────────────────────────────────╮
│ ▀▄▀▄▀▄▀▄ qa-acceptance-pap-be-dev ● 1m │   名字（洋红）· 状态点 · 上次活动相对时间
│ ▄▀▄▀▄▀▄▀  ⏵ working · Bash  ■■■■□□□□ 41% │   状态 + pendingTool · 上下文条
│ ▀▄▀▄▀▄▀▄  ⑂ feature/qa-acc…  Opus·high │   分支 · 模型·effort
│  cc       ⚙ 1 shell ⚇ 2 agents [⇄ 交接] │   agentType 缩写 · shell 数 · subagent 数 · 交接按钮
╰────────────────────────────────────────╯
```
- 上下文条：■（U+25A0）10 格，空格暗灰 #585b70；<30 绿 #a6e3a1，30–60 黄 #f9e2af，≥60 红 #ff6188。未知时 10 格全暗 + `--`。
- 状态色（点 + 边框）：working 绿、idle 暗灰、需要处理（attentionType 非空）红且边框变红。不单独拎出需要处理的会话。
- 超长文本按列宽截断加 …。
- 名字取 `summary`，null 时取 `project`。
- 紧凑变体（3 行，给 S2 sidebar）：S1 不实现，但卡片渲染函数接受「高度」参数预留。

## 交互
- 焦点卡片边框高亮；方向键 / hjkl 移动（j/k 列内，h/l 跨列），鼠标点卡片 = 选中。
- enter 或双击 / 单击已选中的卡片：`ccmux switch <id>` 然后退出 TUI（popup 关掉才能看到目标 pane）。
- `H` 或点 `[⇄ 交接]`：`ccmux send <id> /handoff`（agent 正在 working 时会排到下一轮，这是 ccmux 的行为）。
- `r` = `ccmux restart <id>`，`x` = `ccmux kill <id>`（先 y/n 确认），`d` = `ccmux review <id>`，`s` = 提示输入一行后 `ccmux send <id> <text>`。
- `q` / esc 退出。`ccmux` 子命令的 stderr 显示在底栏一行，不弹窗。
- `tmuxPane` 为 null（background）的会话：switch / send 置灰，底栏提示。

## 测试约定
- tmux 黑盒测试一律用独立 socket：`tmux -L agmux-test …`，绝不碰用户的 tmux server。
- `ccmux show --json` 用 fixture（`testdata/show.json`，从真机抓）喂给 store 做单测；真机 e2e 只做 `agmux board` 能起来、能渲染出 ≥1 张卡片、q 退出。
