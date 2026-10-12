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
- enter 跳转：`tmux switch-client -t <pane>`（在 popup 里和 `ccmux switch` 行为一致，开工第一件事实测）。
- `s` 发送、`H` 交接：`tmux send-keys -t <pane> -l -- <text>` 再 `tmux send-keys -t <pane> Enter`。
- `r` restart、`d` review、`x` kill 仍走 ccmux（restart 要 resume 逻辑、review 要 hunk、kill 要杀 agent 进程而不只是 pane）。

## 不做
- sidebar 紧凑卡片、详情页、5h 限额：顺延到 M2.5（原备忘：详情页要大头像、mission、心跳曲线、子 agent 小头像、.handoff/ 历史；顶栏右侧放 rate_limits 的 5 小时限额）。
- 预览宽度可调和记忆：先固定 40%。
