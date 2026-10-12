# M1 任务清单

状态：待做 / 开发中 / 审查中 / 测试中 / 通过。每项通过的条件是测试那边拿出证据。

## 初版 T1–T4（代码 336732a + e2e 套件 1c70e82，审查 / 测试通过，用户已验收并提出改版）
一条能证伪的实测命令：`cd test/e2e && AGMUX_BIN=~/go/bin/agmux python3 -m unittest -v`（约 130 秒，独立 `-L agmux-test` socket，不碰真 tmux / ccmux）。

### T1 骨架 + store（通过）
- `go mod init github.com/kackerx/agmux`；`agmux board` 子命令；`agmux show` 打印 store 里的会话一行一个（调试用，顺便当测试探针）。
- `internal/ccmux`：exec `ccmux show --json` 解析成 `[]Session`；exec 其它子命令。
- `internal/store`：Store 接口 + 内存实现，每 2s 轮询；合并 HUD 文件（存在才读）和 jsonl 模型兜底（末尾 64KB 倒序找）。
- 验收：`testdata/show.json`（真机抓一份）喂进去，单测断言名字回退、GroupKey、ContextPct 未知 = -1；`agmux show` 在真机打印出 ≥1 行。

### T2 头像（通过）
- 6 个 6×6 像素 sprite（含 agentType 色带），带 ID / Name；半块字符渲染成 6 列 × 3 行；hash 默认分配；`avatars.json` 绑定读写。
- 眼睛三态（扫动 / 闪红 / 变暗）。
- `agmux avatars` 输出：每个 sprite 一块，首行 `<id>  <name>`，接着 3 行渲染结果，块之间空一行。
- `avatars.json` 格式：`{"<sessionID>":"<avatarID>"}`，没有别的键。
- 验收：单测断言渲染结果 3 行每行 6 个可见格；绑定文件写后读回一致；`agmux avatars` 把 6 个打印出来肉眼看。

### T3 卡片 + 布局（通过）
- 列宽×5 卡片按 design.md 渲染；顶栏 + 底栏键位；按 cwd 分列、最小宽 56 均分、按宽折行；焦点高亮；上下文条配色；状态色和边框色。
- 列数算法：gap=2，n = floor((W+gap)/(56+gap))，n<1 取 1；colW = floor((W-gap*(n-1))/n)，余数留在右边空着。边界：113 列 → 1 列，114 列 → 2 列。
- 验收：单测用固定宽度（113 / 114 / 120 / 200 列）断言列数（1 / 2 / 2 / 3）；`tmux -L agmux-test` 开 200×50 跑 `agmux board`，capture-pane 里能看到卡片边框和会话名。

### T4 交互（通过）
- 先在 `tmux -L agmux-test` popup 里实测 bubbletea 能否收到鼠标事件，不能就只留键盘并记到 design.md。
- `x` 确认：按 `y` 执行，其它任意键取消；`s` 输入：enter 发送，esc 或空文本取消。
- 键位和鼠标照 design.md；`ccmux` 子命令通过一个可替换的 runner 接口 exec（测试里换成记录命令的假 runner）。
- 验收：单测断言按键 → 期望的 ccmux 命令行；tmux 黑盒：按 q 退出、enter 后进程退出。

## 验收改版 T5–T6（用户 2026-10-12 验收意见）

### T5 卡片改版（待做）
- 列最小宽 56 → 40；头像 6×6 → 4×4（4 列 × 2 行，6 个 sprite 重画，色带保留）；名字前加 agent 图标（claude U+EC82 / codex U+EC81 / 其它 U+EC20，色带同色）。
- 边框色跟状态走（working 橙 / done 绿 / idle 暗灰 / 需要处理 红），焦点 = 同色粗线边框。
- 行内重排：第 1 行 图标+名 · 状态 · 时间；第 2 行 分支 · 模型·effort；第 3 行 上下文条+百分比 · ⚇ agents · ⚙ shell。去掉 prompt 行，删掉 promptLine / Detail 及其测试和 fixture 字段。
- 验收：TestColumns 改成 {113→2, 81→1, 82→2, 232→5×44, 200→4×48}；`agmux avatars` 每块 2 行每行 4 格；卡片单测覆盖三行字段与让位顺序；`tmux -L` 232×50 真机 5 列。

### T6 `agmux statusline` + `agmux setup`（待做）
- 按 design.md「数据源 B」做。statusline：echo 一份假 payload 进去，文件落盘且原命令的 stdout 原样出来、退出码透传；写目录不可写时仍透传。setup：对临时 HOME 下的 settings.json 三种情况（无 statusLine / 有 X / 已是 agmux）打印正确，`--write` 后 JSON 其它键原样、有 .bak。
- 验收：store 读到 `model.display_name` 显示在卡片；effort 字段等用户跑 `agmux setup --write` 后抓到真实 payload 再接（dev 不能改用户的 settings.json）。

### T7 固定排序 + 折叠分组（待做，用户 2026-10-12 第二轮验收意见）
- 组按 cwd 字典序、组内按名字字典序（同名按 id），和 `ccmux show --json` 的数组顺序无关。
- `f` 折叠 / 展开焦点所在的组；收起只剩 `▸ ~/路径 (n)` 标题行，标题可获焦点，再按 f 或 enter 展开；状态只在本次运行内。底栏加 `f 折叠`。
- 验收：单测把 fixture 数组打乱两种顺序喂进去，渲染结果逐字节相同；折叠后该组卡片不渲染、标题变 ▸、其它组位置不变；黑盒：`-L agmux-test` 里连续 3 次轮询（假 ccmux 每次输出不同顺序）capture-pane 一致；按 f 后标题变 ▸、再按 f 恢复。

## 待用户定
- [ ] T6 做完后用户自己跑 `agmux setup --write`，然后把 `~/.local/state/agmux/<自己会话 id>.json` 给 dev 核实 model / effort 字段名。
