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
| （默认不绑）`group_cwd` / `group_project` / `group_session` / `group_state` | 直接切到那个分组方式，不用按 `g` 循环；和 `g` 切到同一方式时行为一样（各组展开、焦点留原卡），已经在该方式就什么也不做。在配置里给它们键才有用，例如 `group_state = ["G"]`；写 `[]` 表示不绑 |
| `p` | 开关右侧预览（终端至少 100 列）；预览里会画出 pane 的光标（块状 = 反色，竖线 / 下划线 = focus 色下划线；光标被隐藏或预览往回滚时不画） |
| `tab` | 打开预览（没开的话）并进入；在预览里时除 `tab` 外所有按键都发给那个 pane（含 `ctrl-c`、`shift+tab`），再按 `tab` 回到卡片（预览留着，`p` 才关它），滚轮往回翻。直通里 `tab` 到不了 pane，要补全请在真终端里按 |
| `i` | 回复：开预览并把按键交给这个会话（等于 `p` + `tab`），之后打的字直接进它的 pane；`tab` 回到卡片 |
| `H` | 发 `/handoff` |
| `r` | 重启会话 |
| `x` | 杀掉会话（按 `y` 确认） |
| `d` | 用 ccmux review 看改动 |
| `a` | 给这个会话换头像：在这张卡正下方（放不下就在上方）弹出 5 列的头像格子（行数按头像数，25 个就是 5 行，全部显示），先是图片集（内置的，再是你的），最后是像素头像；`h` `j` `k` `l` / 方向键移动，`enter` 绑定，`esc` / `q` / 再按一次 `a`（动作 `avatar` 的键，改了键就是改后的那个）取消；屏幕放不下全部行时才按行滚动，滚动时上 / 下边框用灰色写 `↑ n more` / `↓ n more`（上 / 下被滚掉的头像数）；当前穿的那个一开始就选着，底下的消息行显示选中的 id |
| `t`（动作 `pin`） | 置顶 / 取消置顶焦点卡：置顶的卡在它所在的组里排最前，卡片名字前有 `▲`；多张置顶按置顶先后排（先顶的在前），取消后回到组内默认的名字序。置顶和分组方式无关（切到 project / session / state 仍在各组最前），记在 `$XDG_STATE_HOME/agmux/pins.json`（会话名 → 置顶时间；和头像绑定一样按名字，所以会话 `r` 重启、`/handoff` 换了 ID 仍置顶，没有会话用这个名字**满 60 秒**时条目才自动删，`r` 重启中途那几秒看不到会话不算；没有名字的会话按 project 当名字，同项目的这类会话一起置顶，和头像绑定一致），重启后还在；文件坏了当作都没置顶，底栏报一行，修好或删掉即可。搜索过滤、`s` 定位、折叠照常，`agmux show` 不变 |
| `m`（动作 `mark_read`） | 把焦点卡的 done 标成已读（变 idle），不用开预览，和预览 / `tab` / `enter` 走同一条已读路径（同一个 `seen.json`），底栏一行 `read <名字>`；焦点卡不是 done、或在折叠的组标题上时什么也不发生 |
| `q` / `esc` | 退出（预览获焦时先按 `tab` 回到卡片；保留着 `/` 过滤时 `esc` 先清空过滤） |
| `Q`（动作 `exit`） | 真正退出看板进程；`agmux board --stay` 下 `enter` 跳转从不结束进程、`q` / `esc` 在浮窗里也只收起浮窗，只有它能结束一个浮窗里的看板 |

后台会话（`claude --bg` 起的）没有 tmux pane：在它的卡片上按 `enter` / `i` / `tab`，看板先在 tmux session `agmux-bg` 里开一个窗口跑 `claude attach <id>`（session 不在就新建，窗口名是卡片名、目录是会话的 cwd，不抢你的当前窗口），再照常跳过去（`enter`）或在预览里进直通（`i` / `tab`），之后预览、直通、光标都和普通会话一样；`p` 只看，不 attach，预览区写 `background session · enter or i attaches it`。看板每次轮询用 `tmux list-panes -a` 按启动命令 `claude attach <id>` 认出这个 pane（不存状态文件，重启看板、开多块看板都一致；attach 退出、窗口关掉，卡片就回到没有 pane，再按再开）。直通里 `←` 回 claude 的 agent 视图、`Ctrl+Z` 退出 attach 都是 claude 自己的行为。`H`（发 /handoff）在后台会话上也是先 attach、等窗口出画面再发。新窗口没有 client 时按预览框的大小开（不是 manual，`enter` 跳过去就跟着你的终端变大）。没有 pane 的非后台会话仍然不能跳转和发送，底栏对应键变暗。`x` / `r` 对后台会话还是 `ccmux kill` / `restart` 的现状。

卡片上的名字：ccmux 的 summary；没有（后台会话 `claude --bg -n <名字>` 起的就是这样）时用会话 jsonl 里最后一条 `custom-title` 的名字（每个会话最多整文件扫一次，之后只看新增的部分）；都没有才用项目名。后台会话的卡片第二行右端有灰色的 `bg`，预览标题里 pane 的位置写 `bg`（有 pane 时 `bg · %id`）；`agmux show` 的最后一列是 `bg`（后台会话）或空。

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

**常驻看板：`agmux board --stay`。** `enter` 跳转后看板进程默认会退出，下次要重起。加 `--stay`，`enter` 跳转（把你的 tmux client 切到那个 pane）后看板进程**不退出**，轮询照旧，焦点、头像都留着。两种用法：

1. **浮窗里**（看板跑在自己的常驻 session 里，浮窗只是 attach 上去）：`enter` 切过去并把浮窗 detach 掉，`q` / `esc` 也只是收起浮窗，下次 attach 回来是同一个进程、同一张卡获焦、头像不用重传；`Q` 才真的退出。
2. **普通 pane 里**（直接 `agmux board --stay`）：`enter` 把 client 切到目标 pane，看板留在原 pane 继续跑，切回来焦点还在原来那张卡上；没有浮窗可收，`q` / `esc` 就是退出（`Q` 也行）。

```sh
# 浮窗用法的启动脚本：看板跑在常驻 session 里，浮窗只是 attach 上去
tmux has-session -t =fl-agmux 2>/dev/null || tmux new-session -d -s fl-agmux agmux board --stay
tmux display-popup -E "tmux attach-session -t =fl-agmux"
```

- 被 detach 的只是「看板自己 session 上、本身是 tmux 的 client」（浮窗里 `tmux attach` 出来的那个）；本身不是 tmux 的 client（你的终端）从不 detach；你的终端本身是 tmux（嵌套 / ssh）时，若还有别的 client 在其他 session 上，它可能被当成浮窗。不在 tmux 里 `--stay` 不起作用（没有 pane 可跳），底栏提示 `--stay only works inside tmux`。
- 底栏每秒按 `q` 实际会用的规则问一次 tmux 有没有浮窗可收：有则 `q hide · Q quit`，没有 `q quit`；退出键可以用 `[keys] exit` 改。

### 上下文百分比

卡片上的上下文条来自 Claude Code 的状态栏数据。让 agmux 包住你现有的状态栏命令：

```sh
agmux setup           # 只打印改动前后
agmux setup --write   # 改 ~/.claude/settings.json 的 statusLine.command，原文件备份成 .bak
```

原来的状态栏照常显示；只改 `statusLine.command` 这一个键。`settings.json` 是软链接时改的是链接指向的文件，`.bak` 也放在那旁边。

### 看过的 done 算已读

ccmux 要等你切到那个 pane 才把 done 清成 idle。看板里只有**看过结果**才算已读：在这张 done 的卡上开预览（`p`）、进直通（`tab` / `i`，含点预览区）、或 `enter` 跳转。焦点停在卡上多久、`j` / `k` 路过都不算。已读后卡片按 idle 画，`agmux show` 也一样。记在 `$XDG_STATE_HOME/agmux/seen.json`（会话 ID → 那次 done 的 `statusChangedAt`），重启后还在；会话又干活、再完成一次就重新是 done；ccmux 自己清掉后条目自动删。告警 / 等权限的卡不做已读。文件坏了当作都没看过，底栏报一行，修好或删掉即可。

### 配置

`~/.config/agmux/config.toml`（设了 `XDG_CONFIG_HOME` 就在它下面），没有就全用默认值。`agmux config` 打印生效的完整配置：每个动作、每种颜色、`[layout]`、`[avatars]` 都占一行，行尾注释写着默认值和用途，可选项写在注释里（全英文）。**`agmux config > ~/.config/agmux/config.toml`** 就生成一份完整的配置文件，改要改的行即可（值是当时生效的值，注释里的 `default:` 永远是默认值；已有配置文件时先备份，别直接覆盖）；`agmux config --path` 打印路径。

```toml
[keys]          # 动作 = 一个或多个键名（j、ctrl+k、enter、tab、shift+tab、esc、space、f5…）
down = ["ctrl+n", "down"]
group_state = ["G"]               # 一键直接切到 state 分组（group_cwd / group_project / group_session 同理，默认都不绑）
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
- **内置**：共 25 个头像：20 个图片集和 5 个像素头像。`a` 里的顺序：前 10 套图片集 `aza_choi`、`cipher`、`default`、`forge`、`moss`、`nova`、`oana`、`quill`、`red`、`sprocket`，来自 [pi-agent-portrait](https://github.com/testy-cool/pi-agent-portrait)（MIT，作者 testy-cool，基于 Eiji Onchi 的 pi-emote；`aza_choi` 原出 [pi-emote](https://github.com/shennguyenrs/pi-emote)，MIT）。每一项的来源、作者、许可见 `internal/avatar/sets/CREDITS.md`，许可原文在 `internal/avatar/sets/LICENSE-*`。pi-agent-portrait 的 `draw-portrait` 能从一句描述或一张照片生成一套自己的角色，放进用户目录即可。接着 `developer`、`researcher`、`operator`、`tester`、`designer`，再 `naruto`、`sasuke`、`sakura`、`kakashi`、`itachi`（这 10 套是为这个项目用 GPT 做的，后 5 套是火影忍者角色的同人图，见 CREDITS.md），最后才是 5 个像素头像。用户目录里同名（同 id）的集覆盖内置的，位置不变；你之前自己拷进 `~/.config/agmux/avatars/` 的同名集现在可以删了。重新生成这 10 套内置图用 `go run ./tools/embedsets internal/avatar/sets <原图目录>…`。

未知的键、动作、颜色，键名写错，或你写的两个动作用了同一个键：启动时报一行错误（文件、哪一项、原因）并退出。你绑的键如果原本是另一个动作的默认键，那个动作就让出这个键（一个键都不剩才报错）；`confirm` 和 `leave_preview` 只在各自的模式里生效，可以和看板键重复。`ctrl+c` 不能绑；`ctrl+i` / `ctrl+m` 终端发出来就是 `tab` / `enter`，要写成后者。
