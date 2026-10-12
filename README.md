# agmux

在 tmux 浮窗里用卡片看板查看和管理正在跑的 coding agent（Claude Code、Codex…）：谁在干活、谁在等你、上下文用了多少，一键跳过去或直接回一句。会话列表来自 [ccmux](https://github.com/epilande/ccmux)，跳转、发送、预览都直接走 tmux。

## 前置

- tmux 3.x
- ccmux（`ccmux show --json` 能列出会话）
- Nerd Font 3.5 以上（agent 图标用到 `nf-cod-claude` / `nf-cod-openai`）
- 真彩终端

## 安装

```sh
go install github.com/kackerx/agmux@latest
```

## 用法

```sh
agmux board     # 全屏看板，建议开在 tmux 浮窗里
agmux show      # 每个会话一行：id、名字、分组、状态、模型、上下文%
agmux avatars   # 打印全部头像
```

### 看板键位

| 键 | 作用 |
|---|---|
| `j` `k` `h` `l` / 方向键 | 按屏幕位置在卡片间移动 |
| `enter` / 再点一次卡片 | 跳到这个会话的 pane 并关掉看板 |
| `f` | 折叠 / 展开焦点所在的组 |
| `P` | 开关右侧预览（终端至少 100 列） |
| `tab` | 进入 / 离开预览；在预览里时除 `tab` 外所有按键都发给那个 pane（含 `ctrl-c`），滚轮往回翻 |
| `s` | 回复：开预览并把按键交给这个会话（等于 `P` + `tab`），之后打的字直接进它的 pane |
| `H` | 发 `/handoff` |
| `r` | 重启会话 |
| `x` | 杀掉会话（按 `y` 确认） |
| `d` | 用 ccmux review 看改动 |
| `q` / `esc` | 退出（预览获焦时先按 `tab` 回到卡片） |

没有 tmux pane 的后台会话不能跳转和发送，底栏对应键会变暗。

### tmux 浮窗绑定

```tmux
bind-key a run-shell 'tmux display-popup -w 90% -h 90% -E "AGMUX_CLIENT=#{client_name} agmux board"'
```

`enter` 跳转时要知道切哪个终端。浮窗里 tmux 没有「当前 client」，agmux 默认选最近活动、且不在看板自己所在 session 里的那个 client。经 `run-shell` 把 `#{client_name}` 传给 `AGMUX_CLIENT` 就能精确指定；`display-popup` 的 `-e` 和命令里的 `#{…}` 不会被展开，所以要套一层 `run-shell`。

浮窗里再 attach 一个常驻 session 的用法（浮窗里跑 `tmux attach -t <session>`，看板在那个 session 早就开着的 shell 里）：进程环境变量传不进去，就在启动脚本里、attach 之前把它写进那个 session 的环境，agmux 找不到进程环境变量时会读它：

```sh
tmux set-environment -t "$target" AGMUX_CLIENT "$(tmux display -p '#{client_name}')"
```

只有拿到了 `AGMUX_CLIENT`（进程环境或 session 环境），跳转后 agmux 才会把浮窗里的那个 client detach 掉、浮窗随之关闭；拿不到就只切换，浮窗留着。

### 上下文百分比

卡片上的上下文条来自 Claude Code 的状态栏数据。让 agmux 包住你现有的状态栏命令：

```sh
agmux setup           # 只打印改动前后
agmux setup --write   # 改 ~/.claude/settings.json 的 statusLine.command，原文件备份成 .bak
```

原来的状态栏照常显示；只改 `statusLine.command` 这一个键。`settings.json` 是软链接时改的是链接指向的文件，`.bak` 也放在那旁边。
