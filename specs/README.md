# agmux specs

agmux = 基于 ccmux（v1.4.3）的 CLI + TUI，用来看和管理 tmux 里跑着的 coding agent。
动机：ccmux 自带的 picker / sidebar 全是纯文本行，不直观；视觉参考 Scope app 的 Sessions 卡片（像素头像 + 名字 / 分支 / 上下文条 / 状态 / 模型）。

## 里程碑
| 里程碑 | 内容 | 状态 | 文档 |
|---|---|---|---|
| M1 MVP | 看板浮窗 `agmux board`：按 cwd 分列的卡片、像素头像、键位 / 鼠标操作、`agmux statusline` 透传拿上下文 | T1–T4 通过并验收；T5–T6 改版开发中 | [m1-mvp/design.md](m1-mvp/design.md) · [m1-mvp/tasks.md](m1-mvp/tasks.md) |
| M2 预览 + sidebar + 详情 | 选中卡片看最近对话的预览窗口；替换 `ccmux sidebar` 的紧凑卡片；详情页（大头像、mission、心跳、子 agent） | 备忘 | [m2-preview-sidebar.md](m2-preview-sidebar.md) |
| M3 agmux mod | Claude Code 插件：后台 shell 明细、`$.session.usage()` 直读上下文、`session.receive` 拦截交接 | 备忘 | [m3-mod.md](m3-mod.md) |
| 之后 | 用户手动选择 / 绑定头像的 UI；拓扑视图；Postgres 持久化；分组键切换 | backlog | 见下「预留扩展点」 |

## 技术选型【定】
- Go（go 1.26）+ bubbletea v1 + lipgloss v1；模块名 `github.com/kackerx/agmux`；`go install` 到 ~/go/bin，和 ~/proj/hudiff 一个路数。
- 头像用真彩半块字符 ▀▄ 画（1 列 = 1 像素宽，半行 = 1 像素高），不用 kitty 图形协议。
- 不自己重写 ccmux 已有能力：跳转 / 重启 / 杀 / 审查 / 发送都 exec `ccmux <子命令>`。
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
- tmux 黑盒测试一律用独立 socket，绝不碰用户的 tmux server；每个角色用自己的 socket 名，互不 kill-server：测试 `-L agmux-test`，审查 `-L agmux-review`，开发 `-L agmux-dev`。scratchpad 目录同理，只动自己建的。
- `ccmux send` 的正文一律放在 `--` 之后（ccmux 会把 `-` 开头的文本当选项）。
- `ccmux show --json` 用 fixture（`testdata/show.json`，从真机抓；`lastPrompt` 只保留标签结构，正文换成合成文本，不带真实消息）喂给 store 做单测；真机 e2e 只做 `agmux board` 能起来、能渲染出 ≥1 张卡片、q 退出。

## 流水线约定
- 角色：orchestrator（本文档的维护者）、agmux-dev、code-reviewer、agmux-tester。一批工作先审查再测试，测试结论附证据才算通过。
- orchestrator 只按路径提交 specs/ 和自己的 memory，绝不 `git commit -a`（dev 在同一工作树上干活）。
- 任务状态写在各里程碑的 tasks.md 里，spec 问题的答复写回对应条目，同一件事只留一种说法。
