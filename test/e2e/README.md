# agmux 黑盒 e2e

在真实终端（tmux）里跑 `agmux board` / `agmux show` / `agmux avatars`，按键、鼠标、resize、宽字符、配置加载都走真的；`ccmux` 用 PATH 上的假脚本代替，**绝不会碰真的 ccmux 或你的 tmux**。

## 跑法

```sh
go build -o /tmp/agmux . && cd test/e2e
AGMUX_BIN=/tmp/agmux python3 -m unittest -v                 # 全部，约 2 分钟
AGMUX_BIN=/tmp/agmux python3 -m unittest -v test_t4.Keys    # 只跑一组
```

- 需要 `tmux`（3.x）和 `python3`（3.10+，只用标准库）。`AGMUX_BIN` 缺省为 `~/go/bin/agmux`。`AGMUX_SRC` = 被测二进制对应的源码树（读内置头像图集、README；缺省是本仓库，共享工作区里别人改一半的文件会被读到，所以测某个 commit 时指向它的 worktree）。
- **tmux 必须用独立 socket `-L agmux-test`**（`lib.py` 里写死为缺省值），绝不碰用户自己的 tmux server。多个角色同机跑时用 `AGMUX_TEST_SOCK=<别的名字>` 换 socket（开发 `agmux-dev`、审查 `agmux-review`），因为每次测完会 `kill-server`，共用一个名字会互相把对方的 server 杀掉。
- 同一个 socket 名同时只允许一个套件在跑（`.lock-<socket 名>` 文件上的 flock，不同 socket 名互不阻塞），第二个会排队。

## 怎么测的

- 每个用例自带环境：临时的 `HOME`、`XDG_STATE_HOME`、假 `ccmux`（`show --json` 返回 `Env.write_show()` 写入的 JSON，其它子命令把 argv 记进日志，`fail` / `showfail` / `review.hold` 标志文件控制它的行为），跑完清理，可以和别的测试同时跑（socket 名不同时）。
- 等条件不等时钟：界面要变化时轮询到期望状态出现为止（带超时）。只有断言「什么都没发生」时才用固定短等待。
- 屏幕用 `capture-pane -e` 读，解析成带颜色的格子（`lib.parse_screen`），宽字符占两格。
- 键 → 期望的 `ccmux` 命令行，逐条断言假 ccmux 记下的 argv。ESC 后留 0.4s 间隔再发下一个键。
- 鼠标：`Mouse` 往 pane 注入 SGR 鼠标字节；`PopupMouse` 起一个挂在 pty 上的真 tmux client（`mouse on`），在 `display-popup` 里跑看板，往 client 的 pty 写鼠标字节，走完整条真实路径。

## 文件

| 文件 | 覆盖 |
|---|---|
| `test_t1.py` | `agmux show`：HUD 百分比截断、没有 HOME 报错、ccmux 挂住超时、ccmux 留下占着 stdout 的子进程 |
| `test_t2.py` | `agmux avatars` 输出格式（6 块，每块 `<id>  <name>` + 2 行 × 6 格）/ 管道无 ANSI / tty 真彩；看板里默认分配、绑定、坏文件、眼睛三态 |
| `test_t3.py` | 列数与列宽（最小列宽 40：81 / 82 / 124 / 200 / 208 / 232 列等）、resize、卡片三行字段与让位顺序、名字 + 时间 / 分支 / 条·状态·计数·模型 三行（无 agent 图标）、边框色 = 状态色 / 焦点 #ff8ebe 粗线、6 格上下文条与阈值、模型缓存、`agmux statusline` 抓到的 model / 百分比显示到卡片、多行 stderr |
| `test_t4.py` | 每个键的命令行（send 正文在 `--` 之后）、x 确认、粘贴不当命令、d 的 ExecProcess（回来后鼠标仍可用、失败时底栏显示 stderr 最后一行）、后台会话置灰（提示只出现一次，和错误行同时可见）、连按、鼠标、2s 轮询、会话消失的竞态、popup 里的真鼠标 |
| `test_t6.py` | `agmux statusline`（逐字节透传、退出码、sh -c、写失败不影响透传、session_id 白名单、原子写）和 `agmux setup`（文案、`--write` 保留其它键、`.bak`、损坏 / 无权限、软链接、端到端）；只用临时 HOME，收尾校验真 `~/.claude/settings.json` 哈希没变 |
| `test_t9.py` | 空间导航：120 列（2 列 3 带）下 j 走遍整列（跨带）再停住、k 原路回来、j/k 不退让；h/l 取纵向重叠最多的卡，没有重叠时退到最近一列里纵向最近的卡；方向键等价；折叠标题作为一行高的卡参与；焦点从屏幕上读（#ff8ebe 粗线边框），不用按 enter |
| `test_t10.py` | enter / H / i（T17 起回复键，原 s）直接走 tmux：假 `tmux`（PATH 里记录 argv 的脚本）下的 client 选择（`$AGMUX_CLIENT` / session 环境 / 启发式 / 普通 pane 单终端 / 没有 client）、`detach-client -t` 只在 AGMUX_CLIENT 时做、会话名含空格、display-message 失败退让、`send-keys -l --` 后 ≥150ms 再 Enter、`i` 开预览并进入直通；真 tmux（`-L agmux-test`）嵌套浮窗：外层 client 切到目标、浮窗按规则关闭、目标在浮窗自己 session 里时外层仍 attached、真 pane 收到文本 |
| `test_t11.py` | 预览窗口：P 开关、92 列预览 + 卡片区 3×45、标题行、pane 末尾行带颜色且不漏到边框、<100 列拒绝 / 100 列 60 宽、换焦点预览跟着换、没有 pane 提示、resize（含 ≤5 行不崩）、点另一张卡立即抓屏、折叠 / 展开后预览跟随；被预览的是真 tmux pane |
| `test_t12.py` | 按键直通 + 本地滚动：tab 进入（预览没开先开）、tab 离开且预览不关（T21：tab 到不了 pane，shift+tab 与 ctrl+] 照常转发）、边框 #ff8ebe、目标失效后锁定并吞掉除 tab（esc 也行）以外的所有按键（诱饵 pane 零字节、ccmux 零调用）、`;` 结尾、F13 忽略、粘贴走 load-buffer + paste-buffer -p（真 pane 验证 bracketed paste）、滚轮到顶停住、点标题行/底栏不进直通、按键 / 中文 / `-` 开头 / 粘贴 / 特殊键到达 pane（`fixtures/catv.py` 做按字节回显）、连发按键保持顺序、滚轮每格 3 行、点击获焦；复审二轮（`PassthroughReview2`，tmux 走记录 argv 的包装器）：锁定失效后目标回来仍锁定、空粘贴不调 load/paste-buffer 且无红字、粘贴失败后 buffer 已删、直通中缩到 99 列回卡片并提示 `preview closed: terminal too narrow`、焦点卡消失后 scroll offset 归零 |
| `test_t17.py` | 配置文件（临时 `XDG_CONFIG_HOME`）：无文件 = 默认；`down=["n","down"]` 后 n 下移、j 无动作、底栏同步；`reply` 默认 `i`、`s` 不再进直通；`working="#123456"` 边框转义码；重复绑定 / 坏颜色 / 未知键 / 坏 TOML 各一行 stderr、退出 1、不进 alt screen；`agmux config` 输出写回后行为一致且幂等、`--path`；真 pane 下直通只拦 `leave_preview`（默认 tab；配成 ctrl+g 时 ctrl+] / tab / ctrl+c 全转发；配回 ctrl+] 时它能退、tab 转发）；T17 审查：显式绑定优先的查重（`up=["j"]`、`confirm=["enter"]`、`leave_preview` 默认 tab，两个显式绑定同键才报错）、ctrl+c 不可绑、ctrl+i / ctrl+m 报错、`alt+space` 折叠、`keys = 3` / `colors = "dark"` 报 must be a table；默认动作的唯一默认键被用户占走时不绑不报错（底栏不显示、`agmux config` 打成 `# kill: no key …` 且粘回合法），用户自己写 `kill=[]` 才报错 |
| `test_t18.py` | 头像：`a` 浮层（标题 `avatar for <名字>`、6 个 sprite = `agmux avatars`、当前头像 focus 色、居中、光标起点 = 当前头像、j/k 夹边）、enter 写 `avatars.json` = `{名字: id}` 且卡片立即换 / 重启仍在 / 同名共用、esc·q 不写文件（已有文件逐字节不变）、浮层里 x f p i H r d y h l tab 全无效、无名字绑 session id（消息行提示）、损坏的 avatars.json 不覆盖；`[avatars] default / bind` 与 state 文件的优先级、旧的按 session ID 的条目不迁移、`avatar` 键可改；审查：手改文件不被覆盖（重读合并写）、状态目录只读时报错且内存 / 重启都不变、`null` 文件不崩、浮层压在双宽字符上不移位、折叠标题上 `a` 的提示、外部改文件下次轮询生效（坏文件保留旧绑定、修好无需重启）；配置错误（未知 sprite / 非表值等）一行 stderr 退出 1、`agmux config` 含 `[avatars]` 且回环幂等；真 pane 直通里 `a` 转发 |
| `test_t19.py` | `/` 模糊过滤：顶栏 `/ query▏` + `n/9` 计数、边打边滤（只画命中卡、空组隐藏）、ctrl+h 退格、no match；扩展语法（模糊 / `'精确` / `^前缀` / `后缀$` / `!排除` / 空格 AND / smartcase）；三字段（名字 / 分支 / 目录）命中；命中字符 focus 色（目录命中高亮组标题、中文按字符高亮不破版）；enter 保留过滤并聚焦第一张、esc / ctrl+c 清空并回到按 `/` 时的卡（不退出）、再按 `/` 改查询；鼠标点过滤后布局里的卡；真 pane：预览跟随可见卡、直通里 `/` 转发 |
| `test_t22.py` | 搜索高亮 = 黄底深字（`colors.match` #f2d06b，转义码 48;2;242;208;107，焦点卡上也是黄底、不再有粉色前景、目录命中高亮组标题、配置可改、坏值报错）；保留过滤后 `n` / `N` 按布局序跳下一张 / 上一张并回绕（从屏上画着焦点的那张出发、只访问命中的卡、之后 enter 从新焦点跳转）、esc 后无效、没设过滤时无效、输入框里是文本、无命中不崩；底栏 `n/N next/prev · esc clear ·` 有哪个键显示哪个；键位可配、被用户绑走 n 时不报错 |
| `test_t20.py` | `s` flash（T23：flash.nvim 式，先打子串再按标签）：`s` 后无标签、底栏 `flash: type to narrow · label to jump · esc`、消息行 `s br▏`；打字 → name / branch 的大小写不敏感子串命中黄底、命中后一格一个标签（focus 底深字）、组标题单独搜（dir 只在组标题）、每卡名字+分支只一处命中一个标签；标签字母避开命中后紧跟的字符（alpha / alps / alpine、20 张 alp-NN 跳过 p）、按离焦点的距离分配、第 27 个起大写；按标签只移焦点（零调用）、展开组标题 → 组内第一张、折叠标题 → 标题；退格 / ctrl+h / 无命中 / esc / 非文本键忽略 / 鼠标吞掉；过滤态只搜可见卡；轮询插卡挪动布局后黄底和标签跟着走、字母不变；消失卡的标签忽略（已知缺陷 expectedFailure：字母被打进查询）；保留过滤时 esc 先清空、`quit=["q"]` 时仍成立、ESC ESC 不清；`flash` 键可改；真 pane 直通里 `s` 转发 |
| `test_t24.py` | 预览里画 pane 的光标（真 pane，`capture-pane -e` 的原始转义里读反色 / 下划线）：块状 → 文本后一格反色（空白格也画）、`tput civis` 不画、`\e[3..6 q` 的下划线 / 竖线 → 该格 focus 色下划线、`\e[0..2 q` → 反色；直通里随输入移动；宽字符占 2 格；tab 展开到 8 列制表位且光标列对齐；`seq 1 200` 光标在被裁掉的末尾空行 → 框最底行首格；cursor_x 超出预览宽度不画；往回滚不画、回到底部又画；换卡跟着换 |
| `test_t25.py` | `[layout] max_columns`：232 列 =5 → 一行 5 列（colW = floor((W-2(n-1))/n)）、默认 / =4 → 4 列、180 列 =5 仍 4 列（最小列宽 40）、=1、resize 实时生效、导航按 5 列；0 / 9 / -1 / 字符串 / 小数 / 布尔 / 未知键各一行 stderr 退 1；1 与 8 合法；`agmux config` 打印并回环 |
| `test_t26.py` | `colors.border`：设了 → 所有非焦点卡（working / done / 告警 / idle）边框都是该色（18;52;86）、焦点卡仍 focus 色（可配）、状态词颜色不变、焦点移动后旧焦点卡回到 border 色；空串 / 不配 = 跟状态；ANSI 索引合法；只有 border 允许空串；`agmux config` 打印并回环 |
| `test_t27.py` | effort：`effort.level` → 第 3 行 `模型·level`（点 muted、level 用 colors.effort，换色核对 rgb）；缺字段 / null / 空串 / 顶层字符串 / 其它名字的 effort 字段 → 无 `·`，模型和上下文条照常；类型错（数字/数组/对象/布尔/顶层字符串）→ 同样照常（b35eb49 上 8 条 expectedFailure）；窄列时 model·effort 一起从右截、条和状态词保留；statusline 重跑后实时增删 |
| `test_t28.py` | T28 头像真图：夹具自检（PNG 生成、`write_avatar_set`、fake tmux 的 `client_termname` / `allow-passthrough` 应答 `Env.set_term`、`kitty_commands/kitty_images/kitty_deletes`、`ph_cells`、裸 pty 录原始字节 `PtyRun`（自动应答 OSC 11 / CSI 6n）、`upload_ids`）；kitty + passthrough on → 每张卡 6×2 占位符（每格带行列变音符、前景色 = 该帧 id，按状态取 working/idle/attention/done 帧、500ms 换帧）；其它 termname / passthrough off / renderer=pixel → 像素；renderer=image 跳过终端检测、passthrough off 回落并底栏报一行；tmux 外按 TERM / TERM_PROGRAM；探测问的 client = enter 用的那个（AGMUX_CLIENT 优先）；上传序列（分块 ≤4096、q=2、tmux 里每块包 `\ePtmux;` 且 ESC 加倍、tmux 外先且只有一次 `\e[?1049h`、退出不发 a=d、id 随内容哈希稳定）；用户集（认出、同名覆盖内置、缺状态回 idle、缺 idle 报底栏、只认 .png、符号链接目录、`hash` 保留名、`._` 与点目录不算）；`agmux config` 的 `renderer` |
| `test_t29.py` | T29 头像入口：`a` 浮层先列图片集（只有 id）再列 sprite，用户集按名字序接在内置集后，同名集遮住 sprite，光标停在当前穿的那项，enter 绑集 id（avatars.json + 「X wears id」），像素模式集行 = 按集 id hash 的 sprite、图片模式集行 = idle 帧占位符且不动画，绑集后卡片下一帧换图；列表比屏高（120×30、20 个用户集）光标始终在画面内、到底 enter 绑 bot-06；`agmux avatars` 先集（id、来源、各状态帧数）再 sprite、被跳过的集一行 stderr；README 配置示例可直接加载且 `[avatars]` 默认值与 `agmux config` 一致 |
| `test_t30.py` | T30 头像状态对齐 pi-emote：17 态夹具（`build_states_fixture`）每张卡对帧 id——think（含只有 subagent 在跑）/ read（Read Grep Glob WebFetch WebSearch）/ write（Write Edit MultiEdit NotebookEdit）/ tool（其它，含 mcp）/ hi（attention 或 waiting，优先于 tool）/ success / idle；回落链 read·write·tool→think→idle、hi·success→idle（不借兄弟态）；旧目录名 working/attention/done 当别名、新名优先；`agmux avatars` 按新名计数；idle 眨眼（第 0 帧常态、其余帧每 8 tick 露一个 tick、64 tick 恰好 8 次、各卡按 key 错开）；其它状态每 500ms 一帧 |
| `test_t31.py` | T31 头像填满 6 格：上传载荷 = 居中 3:2 裁剪、高 ≤128（128²→128×85、200×100→150×100 且是中间那块、1024²→192×128 且最近邻取样点逐点核对、已是 3:2 的小图字节不变）；控制串不带 x/y/w/h；坏 PNG / 截断 / >4096 跳过，每集一行 stderr 与底栏；跳完无 idle 的集整个跳过；id 随内容变、同帧两次同 id |
| `test_t32.py` | T32/T33 `a` 选择框 = 焦点卡下方 5 列格子（格宽从屏幕读出，T33 起 10）：顺序（内置集、用户集按名、最后 sprite）、名字行、像素模式集格 = 按 id hash 的 sprite、图片模式集格 = idle 第 0 帧占位符且不眨；按设计的 gridPlace 公式核对多种屏幕 × 每张卡（下方 / 上方 / 少画几行 / x 夹到屏内）；h/l 跨行、j/k 整行、短末行落末尾、>4 行滚动光标行居中；enter 绑定 / esc q 取消 / 消息行 id（sprite 带 name）/ 底栏；bot-06 状态文件落 hash、config 里绑 bot-06 报「was removed」 |
| `test_t34.py` | T34 卡片区滚动（22 张卡 / 120×30）：滚轮每格 3 行不动焦点、到顶到底停；ctrl+d / ctrl+u 半个正文高（13）；手动偏移保留，直到按键把焦点移出可见区才重新跟随；点击按滚动后的屏幕位置选卡；顶栏 `↑ more` / `↓ more` 三态与过滤态不显示；预览区上的滚轮只滚预览；开板后第一次滚动 / ctrl+d 不丢（4aab625 上的缺陷，b8c0a36 修）；`agmux config` 多 page_down / page_up、可改键、被别的绑定占键则无键 |
| `test_t35.py` | T35 `agmux avatars import <sheet.png> --name <id> [--layout 30\|19] [--force]`：6×5 假 sheet（深色网格、四周浅色，每格各色且左右有亮边）→ 30 个 pi 文件名、128²、每个像素 = 对应格颜色（中间 97% 正方形不含亮边）；`--layout 19`（5×4，第 20 格浅色空格）→ 前 19 个；flag 在 sheet 前后都行；已存在需 `--force`（整个替换、失败保留旧集、无 `.import-*` 残留）；非 PNG / 缺文件 / 无深色背景 / 坏 layout / 坏名字退出 1，用法错退出 2；导入后 `agmux avatars` 列七状态帧数、`a` 里可选并可绑、上传按 3:2 裁剪 |
| `test_t36.py` | T36 状态行：` · agents` 与 `⚇ n` 用 working 色（工具名仍 muted，跟 colors.working 走）；100 个无绑定会话图片模式全是占位符头像、像素模式全是 sprite、绑 sprite 仍像素 |
| `test_t37.py` | T37 卡片第 2 行右侧 agent 角色（colors.agent #bd93f9）：来源 = jsonl 尾 64KB 最后一条 agent-setting > 头 64KB 第一条 > 无；resume 换 agent、很久后才 --agent、坏值 / 其它行型忽略、后写入下一轮询出现；分支先截；无分支只显示右侧；`agmux show` 第 7 列；config 默认色 |
| `test_t38.py` | T38 tmux 里图片直写 client tty（PtyTty 假终端）：裸 APC、无 \ePtmux; 包装、无 ?1049h、refresh-client -t；termname / tty 来自 `list-clients -F` 五字段；选 client（最近、非本 session、嵌套 tmux-/screen- 跳过、AGMUX_CLIENT 进程 / -h / 普通 / -AGMUX_CLIENT 不再问普通）；auto 判像素底栏说明；tty 非设备 / 打不开 / 不读 2s / 慢读者的回落与等待 |
| `test_t40.py` | T40 `[layout] flow`：默认 bands、stack（组 k 进列 k mod n、列独立堆叠、无层对齐）、非法值一行报错、`agmux config` 打印；组标题 `▾ 路径 (n) ━━━` 画满列宽 muted、折叠同样、占满无线；T39 几何（列距 1、卡内无边距）在 lib 的 GAP / AV_X 常量里 |
| `test_t41.py` | T41 分支名放不下：逐段去 `/` 前缀并加 `…`、只剩末段仍放不下才尾截（无前导 `…`）、末段刚好放下则光秃末段、无 `/` 尾截、有 agent 按让出的宽度；宽度 40–80 全扫（期望函数按 spec 写，不抄产品）；`/` 搜索高亮只画显示出来的字符、`…` 不带 match 底色（分支行和名字行）、flash 读显示出来的文字 |
| `test_t42.py` | T42 组标题 ━ 与组名同色（默认 dir、`colors.dir` 跟着变、折叠且有焦点时 focus 色、计数 `(n)` 仍 muted） |
| `test_t43.py` | T43 分组方式：`[layout] group_by` 校验 / 打印、`g` 按 cwd→project→session→state 循环、底栏 `g group:<当前>`（在 `f fold` 后）、四种分组的整屏大纲对照 `oracle()`（`(no pane)` 最后、`(no project)`、session 名取首个 ` # ` 前、state 固定序且空组不画）、轮询里换组、折叠清空、焦点不动（折叠标题上按 g 落到该组第一张卡）、名字标题不画路径命中、flash 读标题、透传里 `g` 到 pane、flow 两种模式、改键与 T17 键规则 |
| `test_t44.py` | T44 `agmux board --stay`：真 tmux 两个 server（内层放会话和 `agboard`，外层 tmux 的 pane 里 `tmux attach` 当用户终端，真 `display-popup -E "tmux attach"` 当浮窗）——enter 跳转 + popup 关 + 进程在 + 再 attach 焦点 / 保留的过滤不变、q / esc 只收起、Q 退出、别的 session 上的 tmux-* client 不被 detach、自己 session 上非嵌套 termname 的 client 不被 detach、收起期间轮询不停、降级（普通 pane / 唯一 client / 无 client 时 q 退出；跳转不退出见 t47）；假 tmux 看调用序列（switch 在前 detach 在后、detach / list-clients 失败进程不退并报错）；参数错退出码 2、底栏有 popup 时 `q hide · Q quit`、`exit` 默认 Q |
| `test_t47.py` | T47 `--stay` 在普通 pane 里也常驻（真 tmux）：enter 后 client 到目标 pane、进程仍在、没有 client 被 detach、切回看板 pane 焦点仍在原卡、再 enter 还能跳；q / esc / Q 在无 popup 时退出；无 client 时 enter 不退；底栏无 popup（无 client / 只有用户自己的 client）`q quit`，真 `display-popup` attach 后 2.5 s 内变 `q hide · Q quit`，popup 关掉 2.5 s 内变回，再开又变；不带 --stay 永远 `q quit`；（不在 tmux 提示 `--stay only works inside tmux` 在 t44 Cli；假 tmux 的 jump 无 popup 调用序列在 t44 Fake）|
| `test_t49.py` | T49 内置 20 套：`agmux avatars` 按固定顺序列 20 套（pi 10 → developer / researcher / operator / tester / designer → naruto / sasuke / sakura / kakashi / itachi）+ 空行 + 5 个像素；新 10 套每套 7 个状态各 2 帧；用户同 id 套替换内置且位置不变、别的名字排在 20 套后；选择框（4 行 × 5 列，滚动）顺序与光标行位置、enter 绑定新套；整个上传 281 帧走 client tty、每个 APC 完整 <1024 字节、126×84、10 张脸互不相同、用户 designer 替换后 281-14+1；hash 默认能落到新套。T18 / T32 / T35 的选择框用例改成 `sweep()`（光标从头走到尾收集所有格） |
| `test_t50.py` | T50 `agmux config` 打印带注释的完整模板：默认输出（无文件 / 空文件）与 `internal/config/testdata/config.example.toml` 逐字一致；每个动作 / 颜色 / 布局 / 头像项一行且带 `# default:`；改了 max_columns / flow / group_by / border / name / flash / quit / renderer / default 后只有这 9 行的值变、注释里的 default 不变；输出写回文件再 `agmux config` 结果相同；`[avatars.bind]` 的绑定排序、含引号 / 反斜杠 / 制表 / BEL / ESC / DEL / NUL / 中文 / emoji / U+2028 的名字用 `\uXXXX` 转义并能读回；坏配置仍退出码非 0 且 stdout 为空。旧的「看值」用例改用 `lib.plain_config()`（去掉注释的 `key = value` 行） |
| `test_t51.py` | T51 `group_cwd` / `group_project` / `group_session` / `group_state` 四个动作（默认不绑）：配 `group_state = ["G"]` 后 G 直接到 state 分组（working 在最前）、再按 g 循环到 cwd；折叠清掉、焦点留原卡（含另一个组被折叠时）、结束手动滚动；已在该分组再按 G 折叠保留；四个键从任一分组到任一分组；默认配置四个键无反应而 g 照旧；搜索框里的 G 是文字、直通里的 G 给 pane（回到卡片上才切）；`group_state = ["g"]` 拿走 g 之后循环无键；`agmux config` 四行在 group 之后 restart 之前、带 `# default: none`、写回相同；`[]` 只对这四个合法（`up = []`、`group = []` 报错）、一键一动作、显式键抢默认键；底栏没有新文案 |
| `test_t45.py` | T45 / T48 看过的 done 算已读：只在开预览 / 进直通 / enter 跳转时记 `seen.json`（id → statusChangedAt）——焦点停 3 s、来回路过、popup 里停着都不算（仍是 done、无条目）、卡按 idle 画（状态词、边框色）、重启仍 idle、statusChangedAt 变了回 done 且停着不再自动读、ccmux 清掉 attentionState 或会话没了条目删（别的条目留）、null 不记（预览也不）、坏 seen.json 底栏一行不覆盖、waiting / attention 永不算（预览也不）；`--stay` 真 tmux：popup 里停着不算、预览 / enter 立即记 |
| `test_t46.py` | T46 直写 client tty 的图片不被 tmux 自己的输出截断：pty 当 client tty、读得慢（队列一直满），另一线程每 2 ms 往同一 tty 写 BEL（等同 set-titles），数 APC 里被插进 BEL 的个数（修前 292/321，修后 0）；每个序列 <1024 字节、载荷 ≤768 且非末块是 4 的倍数；tmux 外和 passthrough 仍是 4096。真 kitty 的 15 套图集对比是手工做的（见 m3 结论），没进套件 |
| `test_t7.py` | 固定排序（假 ccmux 每次轮询换数组顺序，卡片不动；同名按 id；组按 cwd 字符串）、`f` 折叠 / 展开（标题 ▾ ↔ ▸、其它组位置不动、折叠标题可获焦点且不响应 H/r/d/s/x、enter 展开、轮询不展开、不跨运行记忆） |

`fixtures/catv.py` 是假 pane 里用的原始模式回显（BSD `cat -v` 会把被拆开的 UTF-8 弄乱码）；`fixtures/paste_probe.py` 是要求 bracketed paste 并回显收到字节的 pane。

`internal/store/store_ext_test.go`、`internal/avatar/avatar_ext_test.go` 是同一轮测试写的补充 Go 测试（模型缓存、64KB 边界、并发刷新、avatars.json 读写与坏文件、sprite 渲染）。

## 夹具

`lib.build_fixture()` 造 9 个会话 / 5 个分组（按 cwd 排序）：a1 working + HUD 41% + 2 个子 agent、a2 名字回退到 project、a3 attentionType 非空 + 超长中文名、b1 原始 idle 但有子 agent 在跑、b2 background（无 pane）、c1 / c2 HUD 30% / 59% 和 done、d1 waiting + HUD 60%、e1 超长 cwd + attentionState read。改夹具要同时看 `NAMES` 和各用例里写死的期望。
