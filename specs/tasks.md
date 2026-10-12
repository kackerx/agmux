# 任务清单

状态：待做 / 开发中 / 审查中 / 测试中 / 通过。每项通过的条件是测试那边拿出证据。

## S1 看板浮窗

### T1 骨架 + store（测试通过 @5dec1f9；审查退回 2 条必须改，待修）
- `go mod init github.com/kackerx/agmux`；`agmux board` 子命令；`agmux show` 打印 store 里的会话一行一个（调试用，顺便当测试探针）。
- `internal/ccmux`：exec `ccmux show --json` 解析成 `[]Session`；exec 其它子命令。
- `internal/store`：Store 接口 + 内存实现，每 2s 轮询；合并 HUD 文件（存在才读）和 jsonl 模型兜底（末尾 64KB 倒序找）。
- 验收：`testdata/show.json`（真机抓一份）喂进去，单测断言名字回退、GroupKey、ContextPct 未知 = -1；`agmux show` 在真机打印出 ≥1 行。

### T2 头像（待做）
- 6 个 6×6 像素 sprite（含 agentType 色带），带 ID / Name；半块字符渲染成 6 列 × 3 行；hash 默认分配；`avatars.json` 绑定读写。
- 眼睛三态（扫动 / 闪红 / 变暗）。
- `agmux avatars` 输出：每个 sprite 一块，首行 `<id>  <name>`，接着 3 行渲染结果，块之间空一行。
- `avatars.json` 格式：`{"<sessionID>":"<avatarID>"}`，没有别的键。
- 验收：单测断言渲染结果 3 行每行 6 个可见格；绑定文件写后读回一致；`agmux avatars` 把 6 个打印出来肉眼看。

### T3 卡片 + 布局（待做）
- 列宽×5 卡片按 design.md 渲染；顶栏 + 底栏键位；按 cwd 分列、最小宽 56 均分、按宽折行；焦点高亮；上下文条配色；状态色和边框色。
- 列数算法：gap=2，n = floor((W+gap)/(56+gap))，n<1 取 1；colW = floor((W-gap*(n-1))/n)，余数留在右边空着。边界：113 列 → 1 列，114 列 → 2 列。
- 验收：单测用固定宽度（113 / 114 / 120 / 200 列）断言列数（1 / 2 / 2 / 3）；`tmux -L agmux-test` 开 200×50 跑 `agmux board`，capture-pane 里能看到卡片边框和会话名。

### T4 交互（待做）
- 先在 `tmux -L agmux-test` popup 里实测 bubbletea 能否收到鼠标事件，不能就只留键盘并记到 design.md。
- `x` 确认：按 `y` 执行，其它任意键取消；`s` 输入：enter 发送，esc 或空文本取消。
- 键位和鼠标照 design.md；`ccmux` 子命令通过一个可替换的 runner 接口 exec（测试里换成记录命令的假 runner）。
- 验收：单测断言按键 → 期望的 ccmux 命令行；tmux 黑盒：按 q 退出、enter 后进程退出。

## 待用户定
- [ ] 批准在 ~/.config/bin/claude-hud 里加一行落盘 payload（数据源 B）。批准后 dev 抓一份真实 payload 定字段名。
