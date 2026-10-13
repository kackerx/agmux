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
agmux show      # 每个会话一行：id、名字、分组、状态、模型、上下文%、agent 定义（没有就空）
agmux avatars   # 打印全部头像：先是图片集（id、来源、各状态帧数），再是像素头像
agmux avatars import sheet.png --name me   # 把 pi-agent-portrait 格式的 sheet 切成图片集（见「头像图片」）
```

### 看板键位

| 键 | 作用 |
|---|---|
| `j` `k` `h` `l` / 方向键 | 按屏幕位置在卡片间移动 |
| `ctrl+d` / `ctrl+u`、卡片区滚轮 | 卡片多到一屏放不下时滚动：按键滚半个正文高，滚轮每格 3 行，都不动焦点；顶栏写着 `↓ more` / `↑ more` 表示上下还有卡。按键把焦点移出可见区时视图回到跟着焦点 |
| `enter` / 再点一次卡片 | 跳到这个会话的 pane 并关掉看板 |
| `/` | 模糊搜索：边打边过滤卡片（名字、分支、目录；fzf 语法：空格 = 且、`'精确`、`^前缀`、`后缀$`、`!排除`，有大写才区分大小写），命中的字符黄底深字；`enter` 保留过滤，`esc` 清空 |
| `n` / `N` | 保留着过滤时，在命中的卡之间按布局顺序跳到下一张 / 上一张（首尾回绕） |
| `s` | 定位（像 flash.nvim）：按 `s` 后输入要找的字符——名字、分支、目录里的子串，不分大小写；命中处黄底，后面一格出现标签字母，按标签焦点就跳过去（只移焦点，不跳转）；继续打字缩小范围，`backspace` 删一个字，`esc` 取消。标签永远不是任何命中后面紧跟的字符，所以接着打字不会撞上标签 |
| `f` | 折叠 / 展开焦点所在的组 |
| `g` | 换分组方式，按 `cwd`（目录，默认）→ `project`（ccmux 的项目：worktree 归到主仓库名下）→ `session`（tmux session，没有 pane 的后台会话归 `(no pane)`，排最后）→ `state`（working / waiting / done / idle 四组，按这个顺序）循环，底栏显示 `g group:<当前>`；换了之后各组都是展开的（折叠不跨方式），焦点还在原来的卡上；cwd 以外的组标题直接写名字，搜索不往标题里画路径命中 |
| `p` | 开关右侧预览（终端至少 100 列）；预览里会画出 pane 的光标（块状 = 反色，竖线 / 下划线 = focus 色下划线；光标被隐藏或预览往回滚时不画） |
| `tab` | 打开预览（没开的话）并进入；在预览里时除 `tab` 外所有按键都发给那个 pane（含 `ctrl-c`、`shift+tab`），再按 `tab` 回到卡片（预览留着，`p` 才关它），滚轮往回翻。直通里 `tab` 到不了 pane，要补全请在真终端里按 |
| `i` | 回复：开预览并把按键交给这个会话（等于 `p` + `tab`），之后打的字直接进它的 pane；`tab` 回到卡片 |
| `H` | 发 `/handoff` |
| `r` | 重启会话 |
| `x` | 杀掉会话（按 `y` 确认） |
| `d` | 用 ccmux review 看改动 |
| `a` | 给这个会话换头像：在这张卡正下方（放不下就在上方）弹出 5 列 × 4 行的头像格子，先是图片集（内置的，再是你的），最后是像素头像；`h` `j` `k` `l` / 方向键移动，`enter` 绑定，`esc` 取消；超过 20 个按行滚动，当前穿的那个一开始就选着，底下的消息行显示选中的 id |
| `q` / `esc` | 退出（预览获焦时先按 `tab` 回到卡片；保留着 `/` 过滤时 `esc` 先清空过滤） |
| `Q`（动作 `exit`） | 真正退出看板进程；`agmux board --stay` 下 `q` / `esc` 和 `enter` 跳转都只收起浮窗，进程留着，只有它能结束进程 |

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
- 启动脚本里写的是普通（不带 `-h`）session 环境也认：`tmux set-environment -t "$target" AGMUX_CLIENT "$client"`。`-h` 的优先；两者都没有才按最近活动猜。普通的缺点见上：之后新开的 pane 会继承它。

只有拿到了 `AGMUX_CLIENT`（进程环境或 session 环境），跳转后 agmux 才会把浮窗里的那个 client detach 掉、浮窗随之关闭；拿不到就只切换，浮窗留着。

**常驻看板：`agmux board --stay`。** 上面「浮窗里再 attach 一个常驻 session」的用法里，`enter` 跳转后看板进程会退出，session 随之没了，下次要重起。加 `--stay`：`enter` 跳转（和 `q` / `esc`）只把 attach 在看板自己 session 上的那个浮窗 client detach 掉，进程不退，轮询照旧，下次 attach 回来是同一个进程、同一张卡获焦、头像也不用重传；`Q` 才真的退出。

```sh
# 启动脚本：看板跑在常驻 session 里，浮窗只是 attach 上去
tmux has-session -t =fl-agmux 2>/dev/null || tmux new-session -d -s fl-agmux agmux board --stay
tmux display-popup -E "tmux attach-session -t =fl-agmux"
```

- 被 detach 的只是「看板自己 session 上、本身是 tmux 的 client」（浮窗里 `tmux attach` 出来的那个）；看板若跑在普通 pane 里（自己 session 上的 client 就是你的终端），或不在 tmux 里，`--stay` 不会 detach 任何东西，行为和没加一样（`q` 退出、`enter` 跳转后退出），不在 tmux 里时底栏提示 `--stay needs the board in its own tmux session`。
- 底栏在 `--stay` 下是 `q hide · Q quit`；退出键可以用 `[keys] exit` 改。

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
agent = "#bd93f9"   # 卡片第 2 行右侧的 agent 定义名（`claude --agent <name>` 的那个，读自 transcript 开头的 agent-setting 行；没有就不显示）
match = "#f2d06b"   # 搜索命中字符的底色（字固定深色 #2d2a2e）
border = "#585b70"  # 所有非焦点卡的边框统一用这个色（默认空 = 边框跟状态变色）；焦点卡仍是 focus，状态行和头像神态仍按状态着色

[layout]
max_columns = 4                     # 终端够宽时最多几列卡片（1–8）；每列至少 40 列宽，所以窄终端到不了这个数（5 列约需 ≥ 200 列）
flow = "bands"                      # 组怎么排：bands（默认）每 n 个组一行，行高 = 最高的组，矮的组下面留空；stack 第 k 个组进第 k mod n 列，每列自己往下堆（没有空洞，列高不齐）
group_by = "cwd"                    # 启动时的分组方式：cwd | project | session | state；运行中按 g 换

[avatars]
renderer = "auto"                   # auto：kitty / ghostty 里用图片，其它终端用像素；image：强制图片；pixel：强制像素
default = "hash"                    # 没绑定的会话：hash = 按名字固定挑一个（图片模式只在图片集里挑，像素模式只在像素头像里挑）；或写某个头像 id（图片集 id 或像素头像 id，agmux avatars 看全部）
[avatars.bind]
"sqlmux-orchestrator" = "bot-03"    # 键是会话名；值同样可以是图片集 id
```

头像按**会话名**绑定（没有名字的会话才按 session ID），重启、交接后名字不变就还是那个头像。看板里按 `a` 选的写在 `$XDG_STATE_HOME/agmux/avatars.json`，优先于配置文件。

### 头像图片

头像可以是真图（PNG），不只是像素头像：每个会话在卡片左上角占 6 列 × 2 行，和下面的上下文条一样宽。图取**居中的 3:2 区域**铺满这 6 格（方形图上下各裁掉约 1/6，头顶和下巴别画太满；比 3:2 更宽的图裁左右）。高超过 128 像素的会缩到 128（不会放大小图）；不是有效 PNG、或任一边超过 4096 像素的帧会被跳过，启动时底栏报一行。

- **要求**：终端是 kitty 或 ghostty（用 kitty 图形协议的 Unicode 占位符）。在 tmux 里，看哪个 client 的终端：和 `enter` 跳转用同一个 client（见下面「tmux 浮窗绑定」），它的 `client_termname` 是 `xterm-kitty` / `xterm-ghostty`；若它自己就是 `tmux-*` / `screen-*`（浮窗里 attach 的内层 client），改看最近活动的非 tmux client。图直接写进那个 client 的 tty，所以**不需要 `allow-passthrough`**，嵌套浮窗、常驻 session 先起都行。不满足时自动回落到像素头像，并在底栏说一行原因（哪个 client、它的 termname）；`[avatars] renderer = "pixel"` 强制像素并去掉这行，`"image"` 跳过终端检测。
- **图片集**：一个目录 = 一个头像，目录名就是它的 id（和像素头像同一个命名空间，同名时图片集优先）：

  ```
  ~/.config/agmux/avatars/<id>/{idle,think,read,write,tool,hi,success}/*.png
  ```

  状态名和 [pi-emote](https://github.com/shennguyenrs/pi-emote) 一样，它的角色目录可以原样丢进来（`talk` / `failure` / `compact` 没有对应的触发，放着不用）：`idle` 空闲；`think` 在跑且没有等工具（含只有子 agent 在跑）；`read` 等 Read / Grep / Glob / WebFetch / WebSearch；`write` 等 Write / Edit / MultiEdit / NotebookEdit；`tool` 等其它工具；`hi` 需要你处理（等权限、等回答）；`success` 做完了。某个状态没有帧时往下回落：`read` / `write` / `tool` → `think` → `idle`，`hi` / `success` → `idle`；**没有 `idle` 的集不算**，启动时底栏报一行。旧目录名 `working` / `attention` / `done` 仍认，分别等于 `think` / `hi` / `success`（新名优先）。
  同一个状态下放多张 PNG，按文件名排序就是动画帧，每 500ms 换一帧；文件名以 `_v2` / `_v3`（`_v` 加数字再加 `.png`）结尾的是「备选 take」（pi-agent-portrait 每次进入状态随机挑一个），不当帧、不加载（`idle_2.png` 这样的才是帧）；`idle` 例外：第一张是常态脸，其余的（比如眨眼）每 4 秒只露一帧。只认 `.png`，图不会被变暗或闪红，神态靠画。
- **从 sheet 导入**：`agmux avatars import <sheet.png> --name <id> [--layout 30|19] [--force]` 把 pi-agent-portrait 格式的一张 sheet（默认 6 × 5 共 30 格，从左到右、从上到下；`--layout 19` 是旧的 5 × 4 共 19 格）切成用户图集 `~/.config/agmux/avatars/<id>/`，文件名照 pi 的（`idle/idle.png`、`idle/idle_blink.png`、`hi/hi1.png` …），所以同一套在 pi 里也能用；每格取中间的正方形缩到 128 × 128。切法同 `draw-portrait`：以深色背景的边界定出网格；sheet 要是 PNG、背景深色。已有同 id 的集会报错，加 `--force` 才替换。
- **什么时候生效**：启动时一次性上传所有图（内置的 + 你的），所以看板开着时新建的目录要重开才认；之后在 `a` 里换头像不需要重传。图很小，退出时不删，留在终端里到关闭，下次启动同 id 覆盖。
- **回落**：非 kitty 系终端、没开直通、或 `renderer = "pixel"` 时，绑了图片集的会话显示一个按集 id 固定挑出的像素头像。图片模式下卡片没有像素头像底边的 agentType 色带。
- **内置**：共 15 个头像：10 个图片集和 5 个像素头像。图片集是 `aza_choi`、`cipher`、`default`、`forge`、`moss`、`nova`、`oana`、`quill`、`red`、`sprocket`，来自 [pi-agent-portrait](https://github.com/testy-cool/pi-agent-portrait)（MIT，作者 testy-cool，基于 Eiji Onchi 的 pi-emote；`aza_choi` 原出 [pi-emote](https://github.com/shennguyenrs/pi-emote)，MIT）。每一项的来源、作者、许可见 `internal/avatar/sets/CREDITS.md`，许可原文在 `internal/avatar/sets/LICENSE-*`。pi-agent-portrait 的 `draw-portrait` 能从一句描述或一张照片生成一套自己的角色，放进用户目录即可。

未知的键、动作、颜色，键名写错，或你写的两个动作用了同一个键：启动时报一行错误（文件、哪一项、原因）并退出。你绑的键如果原本是另一个动作的默认键，那个动作就让出这个键（一个键都不剩才报错）；`confirm` 和 `leave_preview` 只在各自的模式里生效，可以和看板键重复。`ctrl+c` 不能绑；`ctrl+i` / `ctrl+m` 终端发出来就是 `tab` / `enter`，要写成后者。
