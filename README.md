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
agmux avatars   # 打印全部头像：先是图片集（id、来源、各状态帧数），再是像素头像
```

### 看板键位

| 键 | 作用 |
|---|---|
| `j` `k` `h` `l` / 方向键 | 按屏幕位置在卡片间移动 |
| `enter` / 再点一次卡片 | 跳到这个会话的 pane 并关掉看板 |
| `/` | 模糊搜索：边打边过滤卡片（名字、分支、目录；fzf 语法：空格 = 且、`'精确`、`^前缀`、`后缀$`、`!排除`，有大写才区分大小写），命中的字符黄底深字；`enter` 保留过滤，`esc` 清空 |
| `n` / `N` | 保留着过滤时，在命中的卡之间按布局顺序跳到下一张 / 上一张（首尾回绕） |
| `s` | 定位（像 flash.nvim）：按 `s` 后输入要找的字符——名字、分支、目录里的子串，不分大小写；命中处黄底，后面一格出现标签字母，按标签焦点就跳过去（只移焦点，不跳转）；继续打字缩小范围，`backspace` 删一个字，`esc` 取消。标签永远不是任何命中后面紧跟的字符，所以接着打字不会撞上标签 |
| `f` | 折叠 / 展开焦点所在的组 |
| `p` | 开关右侧预览（终端至少 100 列）；预览里会画出 pane 的光标（块状 = 反色，竖线 / 下划线 = focus 色下划线；光标被隐藏或预览往回滚时不画） |
| `tab` | 打开预览（没开的话）并进入；在预览里时除 `tab` 外所有按键都发给那个 pane（含 `ctrl-c`、`shift+tab`），再按 `tab` 回到卡片（预览留着，`p` 才关它），滚轮往回翻。直通里 `tab` 到不了 pane，要补全请在真终端里按 |
| `i` | 回复：开预览并把按键交给这个会话（等于 `p` + `tab`），之后打的字直接进它的 pane；`tab` 回到卡片 |
| `H` | 发 `/handoff` |
| `r` | 重启会话 |
| `x` | 杀掉会话（按 `y` 确认） |
| `d` | 用 ccmux review 看改动 |
| `a` | 给这个会话换头像：列表先是图片集再是像素头像，`j`/`k` 选，`enter` 绑定（集和像素头像都行），`esc` 取消 |
| `q` / `esc` | 退出（预览获焦时先按 `tab` 回到卡片；保留着 `/` 过滤时 `esc` 先清空过滤） |

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
down = ["ctrl+n", "down"]
leave_preview = ["tab"]       # 默认和 focus_preview 同一个键（进出都是 tab）；想让 tab 也能到 pane 里，就改成 ["ctrl+]"]

[colors]        # #rrggbb 或 ANSI 0–255
working = "#ffb86c"
focus = "#ff8ebe"
match = "#f2d06b"   # 搜索命中字符的底色（字固定深色 #2d2a2e）
border = "#585b70"  # 所有非焦点卡的边框统一用这个色（默认空 = 边框跟状态变色）；焦点卡仍是 focus，状态行和头像神态仍按状态着色

[layout]
max_columns = 4                     # 终端够宽时最多几列卡片（1–8）；每列至少 40 列宽，所以窄终端到不了这个数（5 列约需 ≥ 200 列）

[avatars]
renderer = "auto"                   # auto：kitty / ghostty 里用图片，其它终端用像素；image：强制图片；pixel：强制像素
default = "hash"                    # 没绑定的会话：hash = 按名字固定挑一个；或写某个头像 id（图片集 id 或像素头像 id，agmux avatars 看全部）
[avatars.bind]
"sqlmux-orchestrator" = "bot-03"    # 键是会话名；值同样可以是图片集 id
```

头像按**会话名**绑定（没有名字的会话才按 session ID），重启、交接后名字不变就还是那个头像。看板里按 `a` 选的写在 `$XDG_STATE_HOME/agmux/avatars.json`，优先于配置文件。

### 头像图片

头像可以是真图（PNG），不只是像素头像：每个会话在卡片左上角占 6 列 × 2 行，图保持长宽比、居中。

- **要求**：终端是 kitty 或 ghostty（用 kitty 图形协议的 Unicode 占位符）；在 tmux 里还要 `set -g allow-passthrough on`（tmux ≥ 3.3）。不满足时自动回落到像素头像（`[avatars] renderer = "pixel"` 可以强制，`"image"` 跳过终端检测，但 tmux 里没开 `allow-passthrough` 仍会回落并在底栏提示）。
- **图片集**：一个目录 = 一个头像，目录名就是它的 id（和像素头像同一个命名空间，同名时图片集优先）：

  ```
  ~/.config/agmux/avatars/<id>/{idle,think,read,write,tool,hi,success}/*.png
  ```

  状态名和 [pi-emote](https://github.com/shennguyenrs/pi-emote) 一样，它的角色目录可以原样丢进来（`talk` / `failure` / `compact` 没有对应的触发，放着不用）：`idle` 空闲；`think` 在跑且没有等工具（含只有子 agent 在跑）；`read` 等 Read / Grep / Glob / WebFetch / WebSearch；`write` 等 Write / Edit / MultiEdit / NotebookEdit；`tool` 等其它工具；`hi` 需要你处理（等权限、等回答）；`success` 做完了。某个状态没有帧时往下回落：`read` / `write` / `tool` → `think` → `idle`，`hi` / `success` → `idle`；**没有 `idle` 的集不算**，启动时底栏报一行。旧目录名 `working` / `attention` / `done` 仍认，分别等于 `think` / `hi` / `success`（新名优先）。
  同一个状态下放多张 PNG，按文件名排序就是动画帧，每 500ms 换一帧；`idle` 例外：第一张是常态脸，其余的（比如眨眼）每 4 秒只露一帧。只认 `.png`，图不会被变暗或闪红，神态靠画。
- **什么时候生效**：启动时一次性上传所有图（内置的 + 你的），所以看板开着时新建的目录要重开才认；之后在 `a` 里换头像不需要重传。图很小，退出时不删，留在终端里到关闭，下次启动同 id 覆盖。
- **回落**：非 kitty 系终端、没开直通、或 `renderer = "pixel"` 时，绑了图片集的会话显示一个按集 id 固定挑出的像素头像。图片模式下卡片没有像素头像底边的 agentType 色带。
- **内置**：`aza_choi`，来自 [pi-emote](https://github.com/shennguyenrs/pi-emote)（MIT，© Eiji Onchi）。许可文本见 `internal/avatar/sets/aza_choi/LICENSE-pi-emote`。

未知的键、动作、颜色，键名写错，或你写的两个动作用了同一个键：启动时报一行错误（文件、哪一项、原因）并退出。你绑的键如果原本是另一个动作的默认键，那个动作就让出这个键（一个键都不剩才报错）；`confirm` 和 `leave_preview` 只在各自的模式里生效，可以和看板键重复。`ctrl+c` 不能绑；`ctrl+i` / `ctrl+m` 终端发出来就是 `tab` / `enter`，要写成后者。
