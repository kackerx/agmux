# M2 任务清单

状态：待做 / 开发中 / 审查中 / 测试中 / 通过。M1 的 T9 过了再开。

### T10 switch / send 迁到 tmux（待做）
- enter → `tmux switch-client -t <pane>`；`s` / `H` → `tmux send-keys -l -- <text>` + `Enter`。Runner 接口加 tmux 命令，假 runner 记录。
- 开工第一件事：在 `tmux -L agmux-dev` 的 display-popup 里实测 switch-client 能否从 popup 切到目标 pane 并关掉 popup；不行就记原因、保留 ccmux switch。
- 验收：单测按键 → tmux 命令行；黑盒假 tmux（PATH 里的脚本记录 argv）+ 真 tmux 两种各跑一次。

### T11 预览窗口（待做）
- `P` 开关、40% 宽、最少 60 列、不足 100 列不开；`capture-pane -p -e` 每 500ms；标题行；卡片区重排；焦点换卡片预览跟着换；无 pane 时提示。
- 验收：黑盒 232×50 开预览后卡片区变 2 列、预览区出现目标 pane 的内容（假 pane 里 `seq 1 200`，预览显示末尾行且有颜色转义）；关预览恢复 4 列。

### T12 按键直通 + 本地滚动（待做）
- `tab` 切焦点；预览获焦边框 #ff8ebe；可打印 / 粘贴 → `send-keys -l --`，特殊键 → tmux 键名；转发后立刻抓屏；滚轮本地滚动，转发按键归零。
- 验收：黑盒在假 pane 里跑 `cat`，预览获焦输入「你好 world」+ enter，pane 里 `cat` 回显同样内容；esc、ctrl-c、方向键到达 pane（用 `cat -v` 或 `showkey` 类脚本验证）；tab 回来后 q 能退出 agmux 而不是发给 pane。
