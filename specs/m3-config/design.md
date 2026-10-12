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

## `/` 模糊搜索过滤（用户 2026-10-12 新增；细节等 sqlmux 那边的实现回复后对齐）
- 按 `/`：顶栏那一行变成输入框 `/ ▏`，边打字边过滤：匹配字段 = 会话名、分支、cwd（三者拼成一个候选串各自匹配，取最高分），fzf 风格子序列匹配（大小写不敏感；连续命中、词首命中加分），不匹配的卡片隐藏、整组没命中的组隐藏，剩下的卡片按原布局重排（列数不变）。
- 匹配到的字符在卡片上高亮（focus 色）。
- enter：保留过滤，焦点落到第一张匹配卡片，回到正常键位（此时底栏显示 `/ <query> · esc clear`）；esc：清空过滤恢复全部。过滤态下 `/` 再按可以改查询。
- 空查询 = 不过滤。没有任何命中时卡片区显示 "no match"。
- 实现：自写子序列打分（~40 行），不引第三方库，除非 sqlmux 用的库明显更好。

## `s` 全局定位（flash）
- 按 `s`：每张可见卡片（含折叠标题）左上角边框上盖一个 1–2 字符的标签（focus 色反白），标签序列用 home row 优先：`a s d f g h j k l ; q w e r u i o p …`，卡片数超过单字母数时用两字母组合（第一字母按下后只保留以它开头的标签）。
- 按下完整标签：焦点移到那张卡（不直接跳转，和 nvim flash 一样只移光标）；esc 或按了不存在的字符：取消、标签消失。
- 过滤态下也能用，只给可见卡片编号。
- 这和 T14 的 `s`（回复）冲突：回复改成 `i`（insert），T17 的配置里都可改。
