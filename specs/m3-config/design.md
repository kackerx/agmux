# M3 配置与定制（设计）

目标：agmux 给别人用也顺手——文案全英文；键位、颜色、头像都能改；在面板里就能给 agent 换头像。

## 文案
- 所有界面文字改英文：底栏键位提示、消息行（no pane / no tmux client / review failed…）、确认提示、`--help`。状态词本来就是英文。README 保持中文。
- 底栏默认：`j/k h/l move · enter jump · / search · s flash · f fold · p preview · tab focus preview · i reply · H handoff · r restart · x kill · d review · a avatar · q quit`（直通时：`keys go to the pane · tab back to the cards`）。

## 配置文件
- 路径 `~/.config/agmux/config.toml`【用户定】（设了 `$XDG_CONFIG_HOME` 就是 `$XDG_CONFIG_HOME/agmux/config.toml`，默认值等价），没有就全用默认值。TOML 用 `github.com/BurntSushi/toml`。
- `agmux config` 打印**生效**的完整配置（默认值合并用户配置后），用户复制出来改即可；`agmux config --path` 打印路径。
- 配置错误（未知键名、未知动作、键名解析不了、颜色格式错）：启动时 stderr 一行指出文件、键、原因，退出 1。不静默忽略。
- 键位 `[keys]`：动作 = 一个或多个键名（bubbletea 键名：`j`、`down`、`ctrl+k`、`enter`、`tab`、`esc`、`shift+tab`…）。动作列表与默认：
  ```toml
  [keys]
  up = ["k", "up"]         down = ["j", "down"]
  left = ["h", "left"]     right = ["l", "right"]
  jump = ["enter"]         fold = ["f"]
  preview = ["p"]          focus_preview = ["tab"]   leave_preview = ["tab"]
  reply = ["i"]            handoff = ["H"]
  search = ["/"]           flash = ["s"]
  restart = ["r"]          kill = ["x"]
  review = ["d"]           avatar = ["a"]
  quit = ["q", "esc"]      confirm = ["y"]
  ```
  查重规则：用户显式写的绑定优先——用户把键 K 绑给动作 A，而 K 在默认里属于用户没写的动作 B，则 B 自动失去 K；B 一个键都不剩就**不绑任何键、底栏不显示它**（不报错——否则升级新增只有一个默认键的动作会让老配置启动失败，比如 `fold=["a"]` 遇上 T18 的 avatar）；只有用户自己写空列表才报错；用户自己写的两个动作共用一个键才报错。`confirm` 和 `leave_preview` 只在各自模式生效，不参与和看板键的查重。`ctrl+c` 任何动作都不能绑（报错），它固定为：卡片焦点时退出 agmux，预览直通时转发给 pane。键名只接受 bubbletea 会发出的：`ctrl+i` / `ctrl+m` 要写成 `tab` / `enter`，`alt+space` 内部是 `"alt+ "`。
- 错误消息、kill 确认提示、"session is gone" 都用 `attention` 色，不另设 error 色。`effort` 色作用于卡片第 3 行 `·effort` 段（读 payload 的 `effort.level`）。预览获焦时只有 `leave_preview` 的键被拦截，其它全部直通（和 M2 一样）。底栏提示按配置渲染（每个动作显示第一个键名）。
- 颜色 `[colors]`：值是 `#rrggbb` 或 ANSI 0–255 的数字字符串。
  ```toml
  [colors]
  working = "#ffb86c"   idle = "#585b70"   done = "#50fa7b"   attention = "#ff6188"
  focus = "#ff8ebe"     name = "5"         branch = "12"      model = "14"   effort = "13"   dir = "11"
  bar_low = "#a6e3a1"   bar_mid = "#f9e2af"   bar_high = "#ff6188"   bar_empty = "#585b70"
  muted = "#6272a4"     # 次要文字：相对时间、idle 状态词、底栏、(n)、分隔符
  disabled = "#45475a"  # background 会话上置灰的底栏键
  ```
  边框、状态词、眼睛闪红、上下文条、焦点都从这里取；`attention` 同时用于 waiting；`idle` 同时是预览框边框色。默认值 = 现在的样子，不改观感。
- 头像 `[avatars]`：
  ```toml
  [avatars]
  default = "hash"            # 或某个 sprite id，所有没绑定的会话都用它
  [avatars.bind]
  "sqlmux-orchestrator" = "bot-03"   # 键是会话名
  ```
  绑定键改成**会话名**（没有名字才用 session ID）：session ID 每次重启 / 交接都变，按名字才留得住。面板里选的头像写到 `$XDG_STATE_HOME/agmux/avatars.json`（同样按名字），优先级：state 文件 > 配置文件 > default。

## 面板里选头像
- `a`（`keys.avatar`）在焦点卡片上打开居中的浮层列表：每行一个 sprite（2 行渲染）+ `id  name`，当前绑定的那行用 focus 色标出；`up`/`down` 移动，`jump`（enter）绑定并关闭，`quit`（esc）取消。绑定立刻写 avatars.json，卡片下一帧换头像。
- 浮层打开时其它键无效；预览获焦时 `a` 不拦截（直通）。
- 折叠标题或无名字的会话：按名字绑不了就按 session ID 绑，消息行提示 "bound to session id (name missing)"。
- 「会话名」= 卡片上显示的名字（summary，没有就是 project），同一 project 里没 summary 的会话共用一个绑定——用户在配置里写的就是卡片上看到的。
- 浮层打开时光标停在**当前头像**那一行（直接 enter 等于不改）。
- 旧 avatars.json 里按 session ID 写的条目不迁移（ID 每次重启都变）。
- 折叠标题上按 `a`：不动作，消息行 "fold a group? pick a card first"。
- avatars.json 的写入要先在锁内重读文件再合并这一条（两个看板同时开、或看板开着手改文件都不能丢条目）；文件读坏就拒写并报错；rename 成功后才更新内存。轮询时按 mtime 变化重读 avatars.json，另一个看板改的头像下次轮询就生效。文件内容是 `null` 当作空 map。
- 绑定键 = summary，summary 若随对话变化绑定会失效——接受（用户 /rename 过的会话名不变）。

## `/` 模糊搜索过滤（用户 2026-10-12 新增；细节照 sqlmux 的实现对齐：~/proj/sqlmux internal/ui/match.go、internal/app/tree.go）
- 算法：和 sqlmux 一样直接用 fzf 的 `github.com/junegunn/fzf/src/algo`（MIT）`FuzzyMatchV2`，得分和高亮位置与 fzf 完全一致；它不是稳定 API，**固定版本**、只在 `internal/fuzzy/match.go` 一个文件里封装（`Filter(pattern, items) []Match{Index, Score, Pos}`），`init()` 里 `algo.Init("default")`。
- 扩展语法（同 sqlmux）：空格分词 AND，`'精确`、`^前缀`、`后缀$`、`!排除`；不做 `|` 和 `\ `。smartcase：词里有大写才区分大小写。
- 匹配字段：每张卡拼成一串 `name + " " + branch + " " + cwd缩写` 去匹配，三段都能高亮。
- 排序：得分只用来决定 **enter 落到哪张卡**（得分最高的那张）；卡片布局仍按组 / 名字固定顺序，不按得分重排（看板不是列表，位置跳动比排序更碍事）。同分保持原布局顺序；空模式 = 不过滤。
- 过滤态（模式非空）下折叠不生效：有命中的组临时展开，清空过滤后恢复原折叠状态；过滤态下按 `f` 不做事，消息行 "clear the filter to fold"（翻转看不见的折叠状态会在清空后冒出来）。`n/total`：n = 显示的卡数（命中的，加上直通锁定保留的那张），total = 全部会话数，折叠不参与计数——模式非空时全部展开所以 n 就是屏上卡数；模式为空时不过滤、折叠照旧，显示 `9/9` 而屏上可能只有 6 张（折叠标题自己标着藏了几张）。
- 直通锁定的那张卡即使不再匹配也保留显示（否则会误报 session is gone）。
- 没有命中时 `/` 仍可用（改查询），enter 不动；输入框里查询为空时 enter 只关输入框，焦点不动。保留着没命中的过滤时 esc，焦点回到按 `/` 之前那张。
- 保留过滤后清空（esc），焦点留在当前卡，不回到按 `/` 之前的那张；若当前卡所在组恢复折叠后藏起来了，焦点落到该组的折叠标题（输入框里 esc 回到按 `/` 之前那张时同样处理）。
- 同分保序指**布局顺序**（组 / 名字），不是轮询给出的顺序。
- 输入框只收文本：Cc 控制字符和 Cf 格式字符（U+202E 之类）都换成空格。
- 输入框只接受文本键：粘贴里的控制字符（`\r` `\t` ESC…）换成空格或丢弃；alt+字符不当文本。输入框开着时点预览 = 关掉输入框再进直通。
- 输入框：顶栏那一行变成 `/ ▏`，边打字边过滤——不匹配的卡片隐藏、整组没命中的组隐藏、顺序不变、命中字符画成 `colors.match` 底色（默认 #f2d06b，像 nvim 的 Search）+ 深字 #2d2a2e（T22；原 focus 色前景作废），右侧 dim 显示 `3/14`。`enter`：退出输入框但保留过滤，焦点落到得分最高的卡；保留过滤态下 `n` / `N`（动作 `next_match` / `prev_match`）在命中卡里按布局顺序跳下一张 / 上一张、到头回绕，底栏在原整条前面加 `n/N next/prev · esc clear ·`，没保留过滤时不做事；`esc`（和 `ctrl+c`）：清空过滤并退出，焦点留在原卡；过滤态下再按 `/` 改查询；**保留着过滤时按 esc 先清空过滤**（不管 esc 有没有绑给 quit；只认 `esc`，alt+esc 不算；再按才是 quit，和 sqlmux 的列表浮层一致）；没有命中时卡片区显示 "no match"。
- 坑（sqlmux 踩过）：`ctrl+h` 在输入框里当退格；高亮位置是 rune 偏移，画的时候按字素簇换算列宽；名字截断加 `…`。

## `s` 全局定位（flash）
- 照 flash.nvim（sqlmux internal/app/jump.go 已是这套，直接对齐）：**先打子串缩范围，再按标签跳**（T23，用户验收时拍板；T20 的「直接给标签」作废）。
- 按 `s`：进入 JUMP 态，底栏 `flash: type to narrow · label to jump · esc`，其它内容整体 dim 作背景。每打一个字符，在每张**屏幕上看得见**（上边框在窗口内；过滤隐藏的、滚出屏幕的都不算）的卡片（含折叠标题）的 name / branch 文字里找**子串**（dir 只在组标题里找——卡片上看不到 cwd；大小写不敏感，不用 fzf 模糊；每张卡名字 + 分支合起来只取第一处命中、一个标签，组标题单独一处），命中字符画 `colors.match` 底色，命中子串后一格盖一个标签（focus 色底、深色字）。
- 标签字母表同 sqlmux：`asdfghjklqwertyuiopzxcvbnm`，用完再来一轮大写；**顺序按离当前焦点卡的距离**（`|dy|*宽 + |dx|`，同行近者先）分配；**跳过**任何一处命中后紧跟的下一个字符（大小写都跳），这样继续打字和按标签不会撞。52 个不够的卡不给标签、不可跳。
- 打一个字符就出标签。继续打字缩小范围、标签重新分配；轮询后按新布局重扫位置和跳过集合，但「卡 → 标签」保留：这张卡还有命中且它的标签没进新的跳过集合就沿用原标签（用户正要按的字母不在手底下变掉），否则重新分配；退格（含 ctrl+h）删一个字符；无命中时没有标签，可以继续打或 esc；按到标签：焦点移到那张卡、退出 JUMP（只移焦点不跳转）；esc 退出、焦点不动；按了既不是标签也不是可打印字符的键忽略不退出；按到的标签对应的卡已在轮询中消失 → 忽略、留在 JUMP（重扫时把消失命中的字母记为「刚空出」：按它忽略、也不分给新命中，直到打字 / 退格 / 重新按 s）。消息行显示配置的 flash 键名。JUMP 态下鼠标点击和滚轮一律吞掉。
- 标签字色按 focus 色的亮度自动选深（#282a36）或浅（#f8f8f2），不写死。
- 这和 T14 的 `s`（回复）冲突：回复改成 `i`（insert），T17 的配置里都可改。

## 头像用真图（T28，2026-10-13）
用户：头像保持现在 6 列 × 2 行的大小，只要更清晰、可以不像素，参考 pi-emote / pi-agent-portrait 直接贴图，支持自定义和动画。spike 结论（dev 在本机 kitty 0.49 + tmux 3.7c 浮窗里截图核实）：kitty graphics 的 **Unicode 占位符**可行——图先上传，卡片里放占位符字符，tmux 当普通文字处理，裁剪 / 重绘 / 截断都正常；c、r 同时给时 kitty **保持长宽比居中**（正方形图在 6×2 里实际占中间 4 列，两边留白，不用自己补边）。

- **图片集**：一个目录 = 一个头像，目录名就是它的 id（和 sprite id 同一命名空间，重名以用户目录为准）。结构 `<id>/<状态>/*.png`，状态 = `idle` / `working` / `attention` / `done`，对应卡片现在的 Dim / Scan / Alert / Normal 四种神态；一个状态下多张 PNG 按文件名排序就是帧，随 500ms 的 frame tick 循环；状态目录缺了用 `idle` 的帧，`idle` 也没有的集不算（启动时报一行到底栏，不退出）。只认 `.png`。图不做变暗 / 闪红处理，神态全靠作者画。
- **来源**：内置集 go:embed（第一套 `aza_choi`，来自 pi-emote，MIT，README 署名；帧映射：idle → idle.png，working → think_1..4，attention → hi_1..4，done → talk 四帧；预缩到 64×64 减小上传量）；用户集在 `$XDG_CONFIG_HOME/agmux/avatars/<id>/`。启动时**一次性上传所有集的所有帧**（内置 + 用户，spike 实测 12 张 12ms），运行中不再上传——所以 `a` 换头像不需要新传；看板开着时新建的用户目录下次启动才生效。
- **上传**：bubbletea 启动之前写 stdout：每帧一个固定 id（基数 0x00c0de00 起按加载顺序编号，重启覆盖同 id），`a=T,U=1,f=100,q=2,i=,c=6,r=2`，base64 按 ≤4096 分块 `m=1/0`；tmux 里每块各包一层 `\ePtmux;…\e\\`、里面 ESC 加倍；tmux 外直接写。`q=2` 必带（否则 kitty 的回复进 tmux 变成按键）。退出时每个 id `a=d,d=I`。
- **渲染**：Render 的图片路径输出 2 行 × 6 格，每格 U+10EEEE + 行变音符 + 列变音符（每格都带全，不用省略写法，这样被截断后剩下的格子仍自带位置），前景色 `38;2;r;g;b` = 帧 id 的 24 位。ansi.StringWidth 算出来 = 6，版面代码不用改。
- **探测**：`renderer = "auto"`（默认）时：tmux 里 `tmux display-message -p -c <client> '#{client_termname}'`（client 同 enter 跳转用的那个，AGMUX_CLIENT 优先）为 `xterm-kitty` / `xterm-ghostty` 且 `allow-passthrough` 不是 off → 图片；tmux 外看 `TERM=xterm-kitty` 或 `TERM_PROGRAM=ghostty`。**不用 `tmux show-environment TERM_PROGRAM`**（server 在 tmux 里起的会得到 tmux，pi-emote 的方法会误判）。不满足就走现在的像素头像；`renderer = "pixel"` 强制像素、`"image"` 强制图片（不满足时底栏报一行并回落）。回落时图片集绑定的会话显示按集 id hash 出的 sprite。
- **选择与绑定**：`[avatars] default` / `[avatars.bind]` / avatars.json 的值都可以是集 id 或 sprite id；`hash` 在「所有集 + 所有 sprite」里挑。`a` 列表先列图片集（每行 2 行占位符 + id），再列 sprite；像素模式下集那几行用 hash 出的 sprite 代画。`agmux avatars` 打印集（id、来源 builtin/user、各状态帧数）和 sprite。
- 不做：运行中热加载用户目录；ghostty / WezTerm / iTerm2 实测（WezTerm、iTerm2 不支持占位符，走像素）；多 client 同时看同一个 session 的图片 id 冲突（同图同 id，无害）；kill -9 残留的图（下次启动同 id 覆盖）。
