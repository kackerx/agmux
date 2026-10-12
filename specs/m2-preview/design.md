# M2 预览窗口（设计）

目标：不离开看板就能看一个会话在干什么、并直接回一句。参考 ccmux picker 的 preview（右侧、40%、按键直通），但抓屏和按键都直接走 tmux，不经过 ccmux（见 README「tmux 优先」）。

## 布局
```
 ▾ ~/proj/sqlmux (4)      ▾ ~/.config (2)   ┃ sqlmux-orchestrator · %7 · ⏵ working · Bash
 ╭─ card ─╮               ╭─ card ─╮        ┃ ╭──────────────────────────────────────────╮
 ╭─ card ─╮               ╭─ card ─╮        ┃ │ tmux capture-pane -e 的内容（保留颜色）    │
 ╭─ card ─╮                                 ┃ │ …最后 N 行，N = 预览高度                  │
                                            ┃ ╰──────────────────────────────────────────╯
 j/k/h/l 卡片 · P 预览 · tab 进入预览 · …     ┃ [预览获焦时] 按键直通 pane · tab 回到卡片
```
- `P` 开关预览；开着时预览占终端宽度 40%（最少 60 列，终端不足 100 列时不开、底栏提示），卡片区按剩余宽度用 M1 的列数算法重排。
- 预览标题行：会话名 · pane id · 状态。内容 = `tmux capture-pane -p -e -t <pane> -S -<N>`，N = 预览可用高度；预览开着时每 500ms 抓一次（卡片仍 2s 轮询）。
- 焦点换卡片，预览跟着换；焦点在折叠标题或 background 会话（无 pane）上时预览显示「没有 pane」。

## 按键直通（回复）
- `tab` 在卡片区和预览之间切焦点；预览获焦时边框变 #ff8ebe，底栏换成提示「按键直通 pane · tab 回到卡片」。
- 预览获焦时，除 `tab` 外**所有**按键原样转给 pane：可打印字符和粘贴用 `tmux send-keys -t <pane> -l -- <text>`，enter / esc / 方向键 / ctrl-x / backspace 等按 tmux 的键名 `tmux send-keys -t <pane> <Key>`（和 ccmux 的做法一致）。这样 Claude Code 的 vim 模式、斜杠命令、shift+tab 都照常工作。
- 转发后立刻抓一次屏，不等 500ms。
- 鼠标滚轮在预览区：直接转成 `send-keys` 的 `Up`/`Down`？不——改成本地滚动：按 `-S -(N+offset)` 抓更早的行；任何转发的按键把 offset 归零。

## switch / send 迁到 tmux
- agmux 调 `tmux` 只按 PATH 找，不写绝对路径，不加 `-L` / `-S`：靠继承的 `$TMUX` 连到自己所在的 server（popup 里就是用户的 server）。测试里把假 tmux 放 PATH、或把 `$TMUX` 指到 `-L agmux-test` 的 socket，调用就不会漏到真 server。
- enter 跳转（实测结论，tmux 3.7c）：popup 里没有「当前 client」，裸 `switch-client` 会挑最近活动的 client，在用户的 fl-* 嵌套浮窗里会把**浮窗内部**那个 client 切走而不是外层终端；且 switch 不会关 popup。所以：
  - 选 client：`$AGMUX_CLIENT` 有就用它（启动脚本可通过 run-shell 的 `#{client_name}` 传进来，精确）；否则在 `list-clients` 里排除 attach 到 agmux 自己所在 session（`$TMUX_PANE` 的 session）的 client，取最近活动的；没有 `$TMUX_PANE`（直接 `display-popup -E agmux board`）就取全部里最近活动的；排除后一个都不剩（agmux 跑在普通 pane 里、只有一个终端）就退回自己 session 里最近活动的那个，且这种情况不 detach。多个外层 client 都久无活动时可能选错，接受，文档里写明用 `AGMUX_CLIENT` 可精确。
  - 命令：`tmux switch-client -c <client> -t <pane>`。
  - 关嵌套浮窗：**只在设了 `$AGMUX_CLIENT` 时**才 detach——对 switch 之前那份 client 列表里、属于自己 session 且不是被选中 client 的每一个执行 `tmux detach-client -t <client_name>`（不用 `-s`：目标 pane 可能就在自己 session 里，`-s` 会把刚切过来的外层终端一起踢掉）。没设 `AGMUX_CLIENT` 就只 switch 不 detach（启发式分不清「浮窗里的 client」和「另一个真终端」，宁可浮窗留着也不踢用户的终端）。README 写明：fl-* 嵌套浮窗要在启动脚本里传 `AGMUX_CLIENT=#{client_name}` 才会自动关浮窗。
  - `display-message -t $TMUX_PANE` 失败（server 全局环境里残留的 TMUX_PANE 指向无关 pane）时当作没有自己的 session 处理，不报错。
  - session 名用 `TrimSuffix("\n")` 而不是 TrimSpace（tmux 保留末尾空格）。
  - send：`send-keys -l -- <text>` 之后等 150ms 再发 `Enter`（ccmux 也这么做，避免 Claude Code 把紧跟的 Enter 当粘贴内容）；`-l` 那步失败就不发 Enter。
  - 不做：ccmux 的 `/seen` 标记（它自己的轮询会补上）、同一 tmux server 核对（接受）。
  - 然后退出。
  - 验收要在用户真实的 fl-* 启动方式下亲手试一次。
- `s` 发送、`H` 交接：`tmux send-keys -t <pane> -l -- <text>` 再 `tmux send-keys -t <pane> Enter`。
- `r` restart、`d` review、`x` kill 仍走 ccmux（restart 要 resume 逻辑、review 要 hunk、kill 要杀 agent 进程而不只是 pane）。

## 不做
- sidebar 紧凑卡片、详情页、5h 限额：顺延到 M2.5（原备忘：详情页要大头像、mission、心跳曲线、子 agent 小头像、.handoff/ 历史；顶栏右侧放 rate_limits 的 5 小时限额）。
- 预览宽度可调和记忆：先固定 40%。
