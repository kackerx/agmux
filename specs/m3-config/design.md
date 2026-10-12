# M3 配置与定制（设计）

目标：agmux 给别人用也顺手——文案全英文；键位、颜色、头像都能改；在面板里就能给 agent 换头像。

## 文案
- 所有界面文字改英文：底栏键位提示、消息行（no pane / no tmux client / review failed…）、确认提示、`--help`。状态词本来就是英文。README 保持中文。
- 底栏默认：`j/k h/l move · enter jump · / search · s flash · f fold · p preview · tab focus preview · i reply · H handoff · r restart · x kill · d review · a avatar · q quit`（直通时：`keys go to the pane · ctrl+] back to the cards`）。

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
  preview = ["p"]          focus_preview = ["tab"]   leave_preview = ["ctrl+]"]
  reply = ["i"]            handoff = ["H"]
  search = ["/"]           flash = ["s"]
  restart = ["r"]          kill = ["x"]
  review = ["d"]           avatar = ["a"]
  quit = ["q", "esc"]      confirm = ["y"]
  ```
  同一个键绑到两个动作 → 配置错误。预览获焦时只有 `leave_preview` 的键被拦截，其它全部直通（和 M2 一样）。底栏提示按配置渲染（每个动作显示第一个键名）。
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

## `/` 模糊搜索过滤（用户 2026-10-12 新增；细节照 sqlmux 的实现对齐：~/proj/sqlmux internal/ui/match.go、internal/app/tree.go）
- 算法：和 sqlmux 一样直接用 fzf 的 `github.com/junegunn/fzf/src/algo`（MIT）`FuzzyMatchV2`，得分和高亮位置与 fzf 完全一致；它不是稳定 API，**固定版本**、只在 `internal/fuzzy/match.go` 一个文件里封装（`Filter(pattern, items) []Match{Index, Score, Pos}`），`init()` 里 `algo.Init("default")`。
- 扩展语法（同 sqlmux）：空格分词 AND，`'精确`、`^前缀`、`后缀$`、`!排除`；不做 `|` 和 `\ `。smartcase：词里有大写才区分大小写。
- 匹配字段：每张卡拼成一串 `name + " " + branch + " " + cwd缩写` 去匹配，三段都能高亮。
- 排序：得分降序；同分保持原布局顺序（不按长度）；空模式 = 不过滤、顺序不变。
- 输入框：顶栏那一行变成 `/ ▏`，边打字边过滤——不匹配的卡片隐藏、整组没命中的组隐藏、顺序不变、命中字符用 focus 色高亮，右侧 dim 显示 `3/14`。`enter`：退出输入框但保留过滤，焦点落到第一张匹配卡；`esc`（和 `ctrl+c`）：清空过滤并退出，焦点留在原卡；过滤态下再按 `/` 改查询；没有命中时卡片区显示 "no match"。
- 坑（sqlmux 踩过）：`ctrl+h` 在输入框里当退格；高亮位置是 rune 偏移，画的时候按字素簇换算列宽；名字截断加 `…`。

## `s` 全局定位（flash）
- 照 flash.tmux 的做法（每个目标直接给标签，不用先打字）——卡片最多几十张，一键到位比 sqlmux 那种「先打子串再按标签」的 flash.nvim 模式更省；sqlmux 要那样是因为它的目标是满屏单元格。
- 按 `s`：进入 JUMP 态，每张可见卡片（含折叠标题）在上边框左侧盖一个标签（focus 色底、深色字，像 sqlmux 的 yank 高亮），其它内容整体 dim 作背景；底栏显示 `jump: press a label · esc cancel`。
- 标签字母表同 sqlmux：`asdfghjklqwertyuiopzxcvbnm`，用完再来一轮大写；**顺序按离当前焦点卡的距离**（`|dy|*宽 + |dx|`，同行近者先）分配，其余按屏幕顺序（y 再 x）。不做两字母：52 个够用，超出的卡片不给标签、不可跳。
- 按下标签：焦点移到那张卡、退出 JUMP（只移焦点不跳转，和 flash.nvim 的 jump 一样）；esc 退出、焦点不动；按了不是标签的字符忽略不退出。
- 过滤态下只给可见卡片打标签。
- 这和 T14 的 `s`（回复）冲突：回复改成 `i`（insert），T17 的配置里都可改。
