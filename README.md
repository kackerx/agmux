# agmux

在 tmux 浮窗里用卡片看板查看和管理正在跑的 coding agent（Claude Code、Codex…）：谁在干活、谁在等你、上下文用了多少，一键跳过去或直接回一句。会话列表来自 [ccmux](https://github.com/epilande/ccmux)，跳转、发送、预览都直接走 tmux。

## 前置

- tmux 3.x
- ccmux（`ccmux show --json` 能列出会话）
- Nerd Font（分支图标 nf-dev-git_branch U+E725）
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
| `/` | 模糊搜索：边打边过滤卡片（名字、分支、目录；fzf 语法：空格 = 且、`'精确`、`^前缀`、`后缀$`、`!排除`，有大写才区分大小写）；`enter` 保留过滤，`esc` 清空 |
| `s` | 定位：每张卡左上角出现一个字母，按它焦点就跳过去（离当前焦点近的卡字母更顺手）；`esc` 取消 |
| `f` | 折叠 / 展开焦点所在的组 |
| `p` | 开关右侧预览（终端至少 100 列） |
| `tab` | 进入预览；在预览里时除 `ctrl+]` 外所有按键都发给那个 pane（含 `tab`、`ctrl-c`），`ctrl+]` 回到卡片，滚轮往回翻 |
| `i` | 回复：开预览并把按键交给这个会话（等于 `p` + `tab`），之后打的字直接进它的 pane；`ctrl+]` 回到卡片 |
| `H` | 发 `/handoff` |
| `r` | 重启会话 |
| `x` | 杀掉会话（按 `y` 确认） |
| `d` | 用 ccmux review 看改动 |
| `a` | 给这个会话换头像：`j`/`k` 选，`enter` 绑定，`esc` 取消 |
| `q` / `esc` | 退出（预览获焦时先按 `ctrl+]` 回到卡片；保留着 `/` 过滤时 `esc` 先清空过滤） |

没有 tmux pane 的后台会话不能跳转和发送，底栏对应键会变暗。

### tmux 浮窗绑定

```tmux
bind-key a run-shell 'tmux display-popup -w 90% -h 90% -E "AGMUX_CLIENT=#{client_name} agmux board"'
```

`enter` 跳转时要知道切哪个终端。浮窗里 tmux 没有「当前 client」，agmux 默认选最近活动、且不在看板自己所在 session 里的那个 client。经 `run-shell` 把 `#{client_name}` 传给 `AGMUX_CLIENT` 就能精确指定；`display-popup` 的 `-e` 和命令里的 `#{…}` 不会被展开，所以要套一层 `run-shell`。

浮窗里再 attach 一个常驻 session 的用法（浮窗里跑 `tmux attach -t <session>`，看板在那个 session 早就开着的 shell 里）：进程环境变量传不进去，就在启动脚本里、attach 之前把它写进那个 session 的**隐藏**环境，agmux 找不到进程环境变量时会读它：

```sh
tmux set-environment -h -t "$target" AGMUX_CLIENT "$(tmux display -p "#{client_name}")"
```

- 要 `-h`：普通 session 变量会被之后新开的 pane 继承成进程环境变量，而进程环境优先，就会用到过时的 client。
- 要双引号：这行通常写在 `run-shell '…'` 里，单引号会让整个绑定语法错。
- 从另一个浮窗跳过来的分支不能用 `#{client_name}`（那是浮窗内部的 client），要把当前浮窗 session 隐藏环境里的值抄过去：

  ```sh
  tmux set-environment -h -t "$target" AGMUX_CLIENT "$(tmux show-environment -h -t "$current" AGMUX_CLIENT | cut -d= -f2-)"
  ```
- agmux 只认还 attach 着、且不在看板自己 session 里的 client；不符合就当没设，按最近活动猜。

只有拿到了 `AGMUX_CLIENT`（进程环境或 session 环境），跳转后 agmux 才会把浮窗里的那个 client detach 掉、浮窗随之关闭；拿不到就只切换，浮窗留着。

### 上下文百分比

卡片上的上下文条来自 Claude Code 的状态栏数据。让 agmux 包住你现有的状态栏命令：

```sh
agmux setup           # 只打印改动前后
agmux setup --write   # 改 ~/.claude/settings.json 的 statusLine.command，原文件备份成 .bak
```

原来的状态栏照常显示；只改 `statusLine.command` 这一个键。`settings.json` 是软链接时改的是链接指向的文件，`.bak` 也放在那旁边。

### 配置

`~/.config/agmux/config.toml`（设了 `XDG_CONFIG_HOME` 就在它下面），没有就全用默认值。`agmux config` 打印生效的完整配置，复制出来改即可；`agmux config --path` 打印路径。

```toml
[keys]          # 动作 = 一个或多个键名（j、ctrl+k、enter、tab、shift+tab、esc、space、f5…）
down = ["n", "down"]
leave_preview = ["ctrl+]"]

[colors]        # #rrggbb 或 ANSI 0–255
working = "#ffb86c"
focus = "#ff8ebe"

[avatars]
default = "hash"                    # 没绑定的会话：hash = 按名字固定挑一个；或写某个头像 id（agmux avatars 看全部）
[avatars.bind]
"sqlmux-orchestrator" = "bot-03"    # 键是会话名
```

头像按**会话名**绑定（没有名字的会话才按 session ID），重启、交接后名字不变就还是那个头像。看板里按 `a` 选的写在 `$XDG_STATE_HOME/agmux/avatars.json`，优先于配置文件。

未知的键、动作、颜色，键名写错，或你写的两个动作用了同一个键：启动时报一行错误（文件、哪一项、原因）并退出。你绑的键如果原本是另一个动作的默认键，那个动作就让出这个键（一个键都不剩才报错）；`confirm` 和 `leave_preview` 只在各自的模式里生效，可以和看板键重复。`ctrl+c` 不能绑；`ctrl+i` / `ctrl+m` 终端发出来就是 `tab` / `enter`，要写成后者。
