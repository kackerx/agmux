# agmux specs

agmux = 基于 ccmux（v1.4.3）的 CLI + TUI，用来看和管理 tmux 里跑着的 coding agent。
动机：ccmux 自带的 picker / sidebar 全是纯文本行，不直观；视觉参考 Scope app 的 Sessions 卡片（像素头像 + 名字 / 分支 / 上下文条 / 状态 / 模型）。

## 里程碑
| 里程碑 | 内容 | 状态 | 文档 |
|---|---|---|---|
| M1 MVP | 看板浮窗 `agmux board`：按 cwd 分列的卡片、像素头像、键位 / 鼠标操作、`agmux statusline` 透传拿上下文 | T1–T8 通过；T9 审查通过、T13 审查测试中 | [m1-mvp/design.md](m1-mvp/design.md) · [m1-mvp/tasks.md](m1-mvp/tasks.md) |
| M2 预览 | 右侧预览窗口：tmux 抓屏 + 按键直通回复；switch / send 迁到 tmux；sidebar 和详情页顺延 | 全部通过；整套回归 fa040a5（d27522d 上 342 条） | [m2-preview/design.md](m2-preview/design.md) · [m2-preview/tasks.md](m2-preview/tasks.md) |
| M3 配置与定制 | 界面文案全英文；配置文件：键位、状态色 / 焦点色、头像；面板里选头像 | T16–T33 全部通过（c2b26dd，整套回归 574 条；T28–T33 = 头像改 kitty 真图、状态对齐 pi-emote、格子选择框、内置 pi-agent-portrait 10 角色）；待用户验收 | [m3-config/design.md](m3-config/design.md) · [m3-config/tasks.md](m3-config/tasks.md) |
| M4 agmux mod | Claude Code 插件：后台 shell 明细、`$.session.usage()` 直读上下文、`session.receive` 拦截交接 | 备忘 | [m4-mod.md](m4-mod.md) |
| 之后 | 用户手动选择 / 绑定头像的 UI；拓扑视图；Postgres 持久化；分组键切换 | backlog | 见下「预留扩展点」 |

## 技术选型【定】
- Go（go 1.26）+ bubbletea v1 + lipgloss v1；模块名 `github.com/kackerx/agmux`；`go install` 到 ~/go/bin，和 ~/proj/hudiff 一个路数。
- 头像用真彩半块字符 ▀▄ 画（1 列 = 1 像素宽，半行 = 1 像素高），不用 kitty 图形协议。
- **tmux 优先，ccmux 只做发现**（用户 2026-10-12 定）：能用 tmux 直接做的——`switch-client`、`send-keys`、`capture-pane`、`kill-pane`——直接调 tmux，不经过 ccmux；ccmux 只用于会话发现（`show --json`，没有替代）和它独有的能力（`restart` 知道怎么 resume、`review` 起 hunk）。理由：tmux 体量大、接口稳定，ccmux 小项目升级可能变。M1 里走 ccmux 的 switch / send 在 M2 迁到 tmux。
- 路径不写死 /Users/xxx，用 `os.UserHomeDir` / `$XDG_STATE_HOME`。

## 数据模型
```go
type Session struct {
    ID, Name, AgentType, CWD, GroupKey, Branch string
    Status, Attention, PendingTool, TmuxPane string
    Model, Effort string; ContextPct int   // ContextPct<0 = 未知
    Subagents []Subagent; Shells int        // M1 Shells 固定 0（M3 的 mod 才能数）
    AvatarID string; LastActivity time.Time
}
```
- `GroupKey` 由一个 `func(Session) string` 算，M1 = CWD；以后换 project / mainRepoRoot 只改这个函数。
- 角色、通信边、Postgres 持久化：M1 不加字段，但 `Session` 和 `Store` 是以后唯一要改的地方。
- `Store` 接口：`Sessions() []Session`，M1 实现 = 轮询 A + 合并 B/C 的内存实现。

## 预留扩展点（现在不做，架构留位置）
- 拓扑视图：同一 cwd 下 agent 的角色关系图（会话名自带角色：xxx-orchestrator / -code-reviewer / -fullstack-dev / -test-engineer）和通信边（cross-session message 的 from / from-name）。数据模型要能加「会话 → 角色」和「边（谁发给谁、何时）」。
- 持久化：任务进度和状态以后落 Postgres；数据访问都在 `Store` 接口后面，换实现不动 UI。
- 分组键可切换：cwd → project / mainRepoRoot / repo，只改 `GroupKey` 函数。
- 多 agent 类型：头像色带和名字前图标已按 agentType 区分，新类型加一行映射。

## 测试约定
- tmux 黑盒测试一律用独立 socket，绝不碰用户的 tmux server；每个角色用自己的 socket 名，互不 kill-server：测试 `-L agmux-test`，开发 `-L agmux-dev`，审查每个任务一个 `-L agmux-review-<任务>`（同时跑的几个审查互相杀过 server）。e2e 的 lib.py 退出时会对所在 socket `kill-server`，所以跑它的 socket 不能有别人的会话。scratchpad 目录同理，只动自己建的。
- `ccmux send` 的正文一律放在 `--` 之后（ccmux 会把 `-` 开头的文本当选项）。
- `ccmux show --json` 用 fixture（`testdata/show.json`，从真机抓，不带真实消息文本）喂给 store 做单测；真机 e2e 只做 `agmux board` 能起来、能渲染出 ≥1 张卡片、q 退出。

## 流水线约定
- 角色：orchestrator（本文档的维护者）、agmux-dev、code-reviewer、agmux-tester。一批工作先审查再测试，测试结论附证据才算通过。
- orchestrator 只按路径提交 specs/ 和自己的 memory，绝不 `git commit -a`（dev 在同一工作树上干活）。
- 任务状态写在各里程碑的 tasks.md 里，spec 问题的答复写回对应条目，同一件事只留一种说法。
