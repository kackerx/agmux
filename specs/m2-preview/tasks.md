# M2 任务清单

状态：待做 / 开发中 / 审查中 / 测试中 / 通过。M1 的 T9 过了再开。

### T10 switch / send 迁到 tmux（通过 @eebad79；用户真机 enter 待验）
- enter → `tmux switch-client -t <pane>`；`s` / `H` → `tmux send-keys -l -- <text>` + `Enter`。Runner 接口加 tmux 命令，假 runner 记录。
- 实测已做（结论见 design.md「switch / send 迁到 tmux」）：按 AGMUX_CLIENT → 排除自身 session 的最近活动 client 选 client，switch 后 detach 自己的 session。
- 单测用假 tmux 返回构造的 list-clients 输出，覆盖：AGMUX_CLIENT 设定 / 嵌套（排除自身 session）/ 直接 popup（无 TMUX_PANE）/ 没有可用 client 时底栏报错不退出。
- 验收：单测按键 → tmux 命令行；黑盒假 tmux（PATH 里的脚本记录 argv）+ 真 tmux 两种各跑一次。

### T11 预览窗口（测试通过 @f0e3354；复审待出）
- `P` 开关、40% 宽、最少 60 列、不足 100 列不开；`capture-pane -p -e` 每 500ms；标题行；卡片区重排；焦点换卡片预览跟着换；无 pane 时提示。
- 验收：黑盒 232×50 开预览后卡片区变 3 列（预览 92 列、卡片区 140 → 3×45；原写 2 列是算错）、预览区出现目标 pane 的内容（假 pane 里 `seq 1 200`，预览显示末尾行且有颜色转义）；关预览恢复 4 列。

### T12 按键直通 + 本地滚动（测试通过 @f0e3354，分号一条确认是套件注入问题已撤；复审中）
- `tab` 切焦点；预览获焦边框 #ff8ebe；可打印 / 粘贴 → `send-keys -l --`，特殊键 → tmux 键名；转发后立刻抓屏；滚轮本地滚动，转发按键归零。
- 验收：黑盒在假 pane 里跑 `cat`，预览获焦输入「你好 world」+ enter，pane 里 `cat` 回显同样内容；esc、ctrl-c、方向键到达 pane（用 `cat -v` 或 `showkey` 类脚本验证）；tab 回来后 q 能退出 agmux 而不是发给 pane。

### T14 `s` = 开预览并进入直通（测试通过 @04ccebe；审查中）
- `s`：预览没开就开，焦点切进预览（和 `P` + `tab` 等价）；焦点卡无 pane 时底栏提示、不动作。删掉旧的 `s` 输入提示及其测试；底栏 `s 发送` 改成 `s 回复`。`H` 不变。
- 验收：单测 s → 预览开 + 获焦 + 后续按键转发；预览已开时 s 只切焦点；无 pane 不动作；黑盒 s 后直接打字 pane 收到。

### T15 AGMUX_CLIENT 也从 session 环境读（通过 @5e45a49；用户真机 enter 待验）
- 进程环境没有 `AGMUX_CLIENT` 时，`tmux show-environment -h -t <own session> AGMUX_CLIENT`（输出 `AGMUX_CLIENT=xxx`；`-AGMUX_CLIENT` 或出错当没有）。值要校验（在 list-clients 里且不在自己 session）。README 的浮窗段：`tmux set-environment -h -t "$target" AGMUX_CLIENT "$(tmux display -p "#{client_name}")"`，并注明从另一个浮窗跳过来的分支不能用 `#{client_name}`（那是浮窗内部的 client），要把当前浮窗环境里的值抄过去。
- 验收：单测 假 tmux 返回 `AGMUX_CLIENT=/dev/outer` → 选它并 detach 自己 session 里的其它 client；返回 `-AGMUX_CLIENT` → 走启发式；进程环境优先于 session 环境。
