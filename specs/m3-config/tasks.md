# M3 任务清单

状态：待做 / 开发中 / 审查中 / 测试中 / 通过。M2 T15 过了再开。

### T16 文案英文化 + 预览键 `p`（通过 @348865e / 套件 f742f3a）
- 所有界面字符串改英文（见 design.md「文案」）；`P` → `p`。e2e 和单测的中文断言跟着改。
- 验收：`grep -rP '[\x{4e00}-\x{9fff}]' internal main.go` 只剩测试里的输入样例；底栏与 design.md 一致。

### T17 配置文件：键位 + 颜色（通过 @f24bbe5；规则修正 c1b8fcb 随 T18 复审）
- `internal/config`：默认值 + TOML 合并 + 校验（未知键 / 动作、键名解析、颜色格式、重复绑定 → 一行错误退出 1）；`agmux config` / `--path`。
- 键位表驱动：board 的按键分发查配置，底栏按配置渲染；预览直通只拦 `leave_preview`。颜色全部从配置取。
- 验收：单测 无文件 = 默认；改 `down=["n"]` 后 n 下移 j 直通 / 无动作；重复绑定报错文案；颜色改成 `#123456` 后边框转义码变；黑盒 用临时 XDG_CONFIG_HOME 写一份配置跑看板，capture 看底栏和颜色。

### T18 头像：按名字绑定 + 配置 + 面板选择（通过 @c1b8fcb / 套件 306b968）
- Bindings 键改成会话名（无名用 ID）；`[avatars] default` 与 `[avatars.bind]`；优先级 state > config > default。
- `a` 浮层列表、j/k、enter 绑定写 avatars.json、esc 取消；当前绑定高亮。
- 验收：单测 优先级三层；黑盒 a → 列表出现 6 个 sprite、光标在当前头像行，j 两次 enter 后卡片头像变成当前头像下两格的 sprite、avatars.json 里是 `{"<name>":"<id>"}`，重启看板仍是该头像；esc 不写文件。

### T19 `/` 模糊搜索过滤（通过 @5c42cb1 / 套件 c673e9a；回归整套在 d27522d，之后只改搜索的 enter/esc 几行，t19/t7/t20 重跑过）
- 按 design.md「模糊搜索过滤」：fzf algo 封装在 internal/fuzzy（固定版本）、扩展语法、三字段高亮、实时过滤、`3/14` 计数、enter 保留 / esc 清空、no match、ctrl+h 退格。
- 验收：单测 `orch` 命中 orchestrator、`^sql` 只命中前缀、`!test` 排除、`'main` 精确、smartcase；高亮位置按字素簇；过滤后布局只含命中卡片、组数变化；黑盒 fixture 输入 `orch` 只剩 orchestrator 卡，esc 恢复 9 张。
- 复审退回（reviewer @d27522d，探针在其 scratch cr-t19-d27522d/probes/）：
  1. 同分落点按布局顺序：Filter 前把会话按 groups() 展平后的顺序喂进去（TestProbeEnterTieLayoutOrder）。
  2. 清空过滤后焦点卡被恢复的折叠藏起来 → 焦点落到该组折叠标题，输入框 esc 回 searchFrom 那条路同样处理（TestProbeEscClearIntoFoldedGroup）。
  3. 过滤态下 `f` 不做事 + 消息行 "clear the filter to fold"（TestProbeFoldUnderFilter）。
  4. 测试缺口：收 probes/probe_fold_test.go 的 TestProbeFilterOpensFoldedForEverything 进 keys_test.go（钉住 items/current/方向键/flash/mouse 五处 foldedNow）。
  5. 顺手：输入框过滤条件补 `unicode.Is(unicode.Cf, r)`；n 含直通锁定保留的卡（spec 已写明，不改代码）。
- 第三轮（reviewer @224179b，口径直接从「空模式 = 不过滤」推出，不升级）：
  1. 输入框里查询为空时 enter 只关输入框、焦点不动（现在跳到轮询序第一张）：enter 分支加 `m.query != ""`；收探针 cr-t19-224179b/probes/probe_r2_test.go 的两条。
  2. 保留着没命中的过滤时 esc，焦点留在按 `/` 之前那张（现在落看板第一项）：len(shown)==0 时跳过 esc 前的 current()。
- 定稿（2026-10-12 tester 退回「折叠 + 空模式计数 9/9 但屏上 6 张」）：计数是命中数/会话总数，不是屏上卡数；空模式不过滤、折叠照旧，9/9 正确，不改代码，tester 改断言。

### T20 `s` 全局定位 flash + 回复改 `i`（通过 @00fcdab+b6b75e2，补测 4e5d63c / 套件 fa040a5）
- 按 design.md「全局定位」：标签按离焦点距离分配、小写后大写、背景 dim、按标签移焦点、esc 取消、非标签字符忽略；`reply` 默认键改 `i`，底栏与 README 同步。
- 验收：单测 9 张卡的标签顺序按距离（焦点卡自己是 a）、30 张卡进入大写；黑盒 按 s 后每张卡左上角有标签，按 `d` 焦点到第 3 张（粉边框），esc 取消。

## 遗留（不阻塞验收）
- 宽度 1 的终端按 j / s 会 panic（card() 里 strings.Repeat 负数）；终端本来画不下卡片，M3 审查发现，未修。
- 高度 ≤ 7 时焦点卡比窗口高，flash 进入后零标签只能 esc；无害，未修。

## M2+M3 整批回归（tester 2026-10-12，d27522d 二进制，套件 fa040a5）
- go vet / go test -race 全绿；e2e 342 条全过（1 条按 28ee25e 改断言后过）。每批新用例在上一版二进制上反向验证过会红。
- 没测到：真机 enter 跳转（用户验收）、真实 ccmux 字段变化、终端 <96 或 >232 列 / <20 或 >60 行、>52 张卡、flash 和浮层的鼠标、16 项颜色里只断言了 working / focus / muted、fzf 具体分值。
- T19 第二轮修复（0316993）到了再补三条并重跑 test_t19 / test_t7 / test_t20。

## 第二 + 三批整批回归（tester 2026-10-12，1b5873f，套件 ecae36f）
- go vet / go test -race 全绿；e2e 426 条全过（863s），0 skip / 0 expectedFailure。
- 没覆盖：nvim 插入态与 Claude Code 输入框里的真实光标（用 DECSCUSR 序列代测）、flash 超过 52 张卡、真 ccmux。

## 第二批（用户 2026-10-12 验收意见；2026-10-12 晚开工，顺序 T21→T22→T23→T24）

### T21 预览进出都用 `tab`（通过 @2bb3fb4 / 套件 08a86ab）
- 用户原话：ctrl+] 退出预览焦点不顺手。改成：`tab` = 打开预览（若没开）并进入直通；直通里 `tab` = 回到卡片，预览**不关**；`p` 仍是开/关预览。
- 落地：默认键 `focus_preview = tab`（没开预览时先开再进）、`leave_preview = tab`（原 ctrl+] 不再默认绑，用户可在 config.toml 绑回来）；底栏文案、README、m2-preview/design.md「按键直通」一节同步改（design.md 里「否掉 tab」的理由作废：用户实测后拍板）。
- 代价（已知，用户决定）：直通里 `tab` 到不了 pane——Claude Code 的路径补全、shell 补全要在真终端里按；`shift+tab` 照常直通。
- 验收：黑盒 预览关着按 tab → 预览开 + 边框粉 + 底栏 "keys go to the pane · tab back to the cards"；再按 tab → 边框普通色、预览还在、底栏回卡片提示；p 关预览；直通里 shift+tab 仍 send-keys 到 pane；config `leave_preview=["ctrl+]"]` 时 ctrl+] 也能退。

### T22 搜索高亮改黄底 + 保留过滤后 n/N 上下跳（通过 @2069326 / 套件 943fe64）
- 用户 2026-10-12 验收：命中字符不要粉色前景，要像 nvim 的 Search——**黄底深字**（截图：浅黄底 #f2d06b 左右、字 #2d2a2e）。新增颜色项 `colors.match`（默认那个黄），命中字符画成 match 底 + 深字；design.md「命中字符用 focus 色高亮」那句改掉。
- enter 保留过滤后，`n` / `N` 在命中的卡里按布局顺序跳下一张 / 上一张，到头回绕；新增动作 `next_match` / `prev_match`（默认 n / N，和现有默认键不冲突）；没保留过滤时 n/N 不做事；底栏在保留过滤态显示 `n/N next/prev · esc clear`。
- 定稿（dev 提议照准）：保留过滤态底栏是在原整条底栏前面加 `n/N next/prev · esc clear ·`，不整行替换（enter jump 等提示还在）；n/N 和 j/k 一样从 m.current()（屏幕上画着焦点的那张）出发走下一张 / 上一张，不看原始 m.focus（审查退回：焦点卡被轮询改名后不再命中时，屏上焦点已退到第一张，按 n 却原地不动）。
- 审查退回（reviewer @60c2217，探针 cr-t22-60c2217/probes/）：1. n/N 改走 m.current()，删「隐藏卡」分支（TestProbeNFromDisplayedFocus）；2. 补测布局序 ≠ 得分序时 n 按布局序（收 TestProbeNLayoutOrder）；3. 底栏 n/N 提示有哪个键显示哪个；README 配置示例 `down = ["n", "down"]` 换成不撞默认键的键。
- 验收：黑盒 `/ a` enter 后抓屏带色，命中字符格子是黄底；n 三次焦点按布局序循环；N 反向；esc 后 n 无效。

### T23 `s` flash 改成 flash.nvim 式：先打子串再按标签（通过 @1b5873f / test_t20 35 条；标签跟随布局：卡→标签沿用，消失字母 freed 不分给新命中、按它忽略，分配跳过上一帧画过的字母）
- 用户 2026-10-12 验收：现在「一个字母跳一张卡」不对，要和 flash.nvim 一致——按 `s` 后输入字符，卡片的 name/branch/dir 里**子串命中**（大小写不敏感，不用 fzf 模糊）的位置黄底高亮（同 T22 的 match 色），每张命中卡在命中子串后一格盖一个标签字母（focus 底深字）；标签字母**避开**所有命中后紧跟的下一个字符（这样继续打字和按标签不会撞）；按标签跳过去并退出；继续打字缩小范围；退格；esc 取消；无命中时标签全无、继续打字或 esc。按 sqlmux 的 internal/app/jump.go 做（字母表 asdfghjklqwertyuiopzxcvbnm → 大写、按离焦点距离分配、跳过匹配后紧跟字符）；design.md「全局定位」一节按此重写（T20 的「直接标签」作废：用户实测后拍板）。
- 折叠标题也参与（标题文字里命中）。dev 定稿（照准）：dir 只在组标题里找（卡片上看不到 cwd）；组标题命中的标签放在标题行，展开的组按标签跳到它的第一张卡、折叠的落到标题；消息行显示已打的内容；再次按 s 清空重来；改窗口大小重扫。打一个字符就出标签（flash.nvim 默认）。底栏 `flash: type to narrow · label to jump · esc`。
- 审查退回（reviewer @b4b5351，探针 cr-t23-b4b5351/probes/）：1. 轮询后命中和标签仍画在旧坐标、压到别的卡上——改为轮询后按新布局重扫，「卡 → 标签」保留（design.md 已写）；探针 TestProbeFlashStaleAfterPoll。2. 收 probe_t23b_test.go 三条：TestProbeFlashNameCutByTime（名字宽度扣右侧时间）、TestProbeFlashOpenTitleAfterHit（前卡命中后下一组标题仍搜）、TestProbeFlashTitleJustAbove（标题在窗口上一行不画到顶栏）。3. spec 定稿：每张卡名字 + 分支合起来一处命中一个标签（按现有代码），组标题单独一处。
- 验收：单测 标签避开后续字符（卡名 "alpha" "alps"，打 "al" 后标签不能是 p / s）；黑盒 `s` 打 `br` → 只有 bravo-* 卡有黄底 + 标签，按标签焦点到位；打到无命中时无标签、esc 退出。

### T24 预览里画出 pane 的光标（通过 @29aa7ef / test_t24 13 条；nvim 插入态用 `\e[5 q` 代测，Claude Code 输入框未测）
- 审查退回（reviewer @4e0985d，探针 cr-t24-4e0985d/probes/）：1. 必改：内容填满框、光标停在被裁掉的末尾空行（seq / tail -f 常态）时不画——有光标时末尾空行只裁到光标行为止（取 max(非空行数, cur.row+1) 再取最后 n 行，框少显示最上一行）；探针 TestProbeCursorBlankRowFullBox。2. tab 展开改按 8 列制表位（现在固定 4 格，光标列错位）；探针 TestProbeCursorAfterTab。3. 信息行认不出（旧 tmux 无 cursor_shape）也丢掉第一行、只是不画光标，不当屏幕内容。
- 用户 2026-10-12 验收：预览里看不到光标。能解决：抓屏时顺带 `tmux display -p -t <pane> '#{cursor_x} #{cursor_y} #{cursor_flag} #{cursor_shape}'`（tmux 3.7 有这些变量，已查 man），在预览对应格子上画光标：cursor_flag=0 不画；shape 块状 → 该格反色；竖线/下划线 → 该格加 focus 色下划线（终端里画不出细竖线，用下划线替代）。滚动（scroll 偏移非 0）时不画。预览和直通态都画，直通态随每次抓屏刷新。
- **先实测再写死**：dev 先用一个真 pane 验证 cursor_shape 的取值（0/1/2 还是文字）和 cursor_flag 在 vim 插入态 / Claude Code 输入框里的值，把结果写进 design.md 再实现。
- 验收：黑盒 真 pane 里跑 `printf 'abc'` 后预览第一行第 4 格反色；`tput civis` 后不画；滚动后不画。

### T25 最大列数可配置（通过 @81e6032 / test_t25）
- 用户 2026-10-12 验收：内容显示绰绰有余，想收窄到 5 列。现在 view.go `maxCols = 4` 写死。
- 做法：config.toml 新增 `[layout] max_columns = 4`（1–8，默认 4 不变；最小列宽 40 不动，终端够宽才会真到 5 列：5 列约需 ≥ 200 列）；`agmux config` 打印它；README 配置示例加一行、说明「宽度够时最多几列」。用户自己的 config 写 5。
- 验收：单测 232 列 max_columns=5 → 5 列、=4 → 4 列、180 列 =5 → 4 列（受最小列宽限制）；config 超范围 / 非整数报错一行；黑盒 232×50 夹具配 5 → 抓屏数到 5 张卡一行。

### T26 非焦点卡边框统一灰色（通过 @81e6032 / test_t26；只有 border 允许空串）
- 用户 2026-10-12 验收：边框色跟状态太花哨分散注意力，想全部灰色、只留 focus 粉边框，状态靠卡片里的状态行 / 头像神态区分。
- 做法：新增颜色项 `colors.border`，默认空 = 沿用现在「跟状态」；设了就所有非焦点卡的边框都用它（focus 边框仍是 `colors.focus`）。状态行文字（⏵ working / ● done / 告警）和头像神态不受影响，仍按状态着色。不动 working/idle/done/attention 四项的语义（它们还管状态行和头像）。README 颜色表加一行，示例给 `border = "#585b70"`。
- 验收：单测 border 设了 → working / done / alert 卡的边框转义码都是 border 色、focus 卡仍 focus 色、状态行仍是状态色；没设 → 和现在一样；黑盒 配置后抓屏带色核对。

## 第四批（用户 2026-10-13 上午验收意见）

### T27 接上 effort（通过 @1367e65 / 套件 0a6189f，test_t27 27 条；整套 450 条）
- 复审通过；建议「语法错分支杀不掉变异可删」不改。回归里 test_t10 一条时序问题是测试自己的（时间戳未落盘），tester 已修。没覆盖：真 Claude Code 的 effort payload、超长 effort 截断、colors.effort 写 ANSI 索引。
- 审查退回（reviewer @b35eb49，探针 cr-t27-b35eb49/probes/）：effort 类型不对（{"level":3}、"effort":"high"）时整个状态文件被当未知，pct 和模型一起丢——改成只有语法错误才算未知，*json.UnmarshalTypeError 放过；TestHUD 加两行。让位顺序、colors.effort、变异探针都过。
- 用户跑了 `agmux setup --write`，`~/.local/state/agmux/<id>.json` 里真实字段是 `"effort":{"level":"high"}`。readHUD 读 `effort.level`，卡片第 3 行 `模型·effort` 的 effort 段生效，`colors.effort` 随之生效；缺字段 = 空串、不显示（和现在一样）。m1-mvp/design.md 第 15 行「核实后再读」、m3-config/design.md 第 27 行「看不到效果」两句改掉。
- 验收：单测 payload 带 effort.level → Effort="high"；不带 → ""；黑盒 夹具 json 加 effort 后卡片第 3 行出现 `·high`，颜色是 colors.effort。

### T28 头像换风格（阶段一：样张，待开工；阶段二看用户挑选结果再立）
- 用户原话：头像要更多风格可选——终端里好看、可以不是像素、更拟人、二次元、男女看得出来、机器人等。
- 判断：现在 6×4 像素、单色躯体，画不出发型 / 性别 / 五官；要拟人至少 **8 像素宽 × 8 像素高**（半块渲染 8 列 × 4 行，卡片从 5 行变 7 行、上下文条跟着 8 格）和**每个 sprite 自带调色板**（发 / 肤 / 眼 / 衣 / 点缀各一色，底边仍是 agentType 色带）。非像素（颜文字、emoji）在终端里宽度不稳、无法做眼睛神态，不走。
- 阶段一（dev，不进产品代码、不走审查 / 测试闸门）：在 dev 的 scratchpad 里做一个样张程序（复用 avatar 的半块渲染思路），一次打印全部候选，用户在自己终端里跑一遍看效果挑。候选分 4 组、每组 4–6 个：**人像**（短发男、长发女、马尾、胡子、眼镜…）、**二次元**（大眼睛占两像素高、刘海 / 呆毛、彩色头发，男女各有）、**机器人**（天线、面罩、屏幕脸）、**吉祥物**（猫耳、熊、现在这批 bot 的放大版可留 1–2 个作对照）。每个带 id 和名字；眼睛仍用 `E` 像素（扫动 / 闪红 / 变暗要能照旧做），样张里每个至少给 normal 和 Dim 两态。另附**一张 7 行高的假卡片**（8×4 头像 + 名字 / 分支 / 第 3 行照旧，多出的两行先空着）让用户看占不占地方。
- 阶段二（用户挑完再写）：定尺寸、定保留哪些、卡片 7 行多出的两行放什么（候选：目录、模型·effort 单独一行）、`a` 选头像列表改成按组、README / `agmux avatars`。
- 阶段一 b（用户 2026-10-13 看了 20 个样张：像素块还是太大，要像素游戏那种细腻）：终端一格只能用字符拼，再细只有两条路——**四分块**（▘▝▀▖▌▞▛▗▚▐▜▄▙▟█，一格 2×2 像素，但一格只能有前景 / 背景两色，像素是 1:2 的竖长方形）和**更多格**（半块 12×12 像素 = 12 列 × 6 行，卡片 8 行）。图形协议（kitty / sixel）在 tmux 浮窗里直通不稳、终端不一定支持，不走。第二张样张：同三个角色（人像 Mina、二次元 Hana、机器人 Beep）各画三版并排——A 现在的 8×8 半块（对照）、B 四分块 16×8 在同样 8 列 × 4 行、C 半块 12×12 在 12 列 × 6 行；B 画的时候每个 2×2 块内不超过两色。用户挑一版再决定全套。
- 阶段一 c（用户 2026-10-13 看了对比样张：C 够细但太大；**尺寸要保持现在的大小，只要更清晰**，可以不像素风；参考 pi 的 pi-agent-portrait / pi-emote 直接贴图，并支持用户自定义和动态）：改走**终端图片协议**。两个参考项目都是 MIT，在 tmux 里用 **kitty graphics 的 Unicode 占位符**（U+10EEEE + 行列变音符，前景色编图片 id；图片用 `\ePtmux;…\e\\` 直通上传），占位符对 tmux 是普通文字，跟着 pane / 浮窗裁剪和重绘，所以是「pane-safe」。用户终端：kitty 0.49、tmux 3.7c、allow-passthrough 已开。非 kitty 系终端（无 kitty graphics 的）和没开直通时回落到现在的半块像素头像，不删现有代码。
  - 先做**可行性 spike**（dev，scratchpad，不进产品代码）：一个 bubbletea 全屏程序，在 tmux 浮窗里上传 PNG（可拿 pi-emote 的 aza_choi 帧，MIT）并以 4×2、6×2、8×4 格三种框显示；每 200ms 重绘周围文字、每 500ms 换一张图片 id（动画）；验证：浮窗里能显示、不残影、不闪、重绘不丢；占位符行经 ansi.StringWidth 算出的宽度 = 列数（版面代码不用改）；长宽比是保持还是拉伸；退出时 a=d 删图，重开再传同 id 正常；一张 128×128 PNG 直通上传的耗时。用户在自己的 kitty 里跑看结果。
  - spike 结论（dev，aspike）：可行。c/r 同给时 kitty 保持比例居中；动画 25 张截图无空白、无错位；StringWidth = 列数；a=d 删图有效；12 张 435KB 写出 12ms；直通在浮窗里可用。坑：别用 show-environment 探测终端，用 client_termname；q=2 必带；先上传再进 bubbletea。设计定稿见 design.md「头像用真图」。

### T28 头像用真图（通过 @e7e4f6c / test_t28 43 条，套件 594c3dc；历程 6f605fc → 4648d7d → 0223f6d → e7e4f6c）
- 复审（reviewer @e7e4f6c）：全部关闭；kitty 0.49 回放最终上传字节备用屏 13 张、退出回主屏。未实测：xterm 类终端里 tea 出错路径多写一次 ?1049l 可能恢复旧光标位置（影响极小，不改）。
- 修复定稿：id 哈希含文件内容；`avatar.HashImages` 开关由 main 在首次 Refresh 前经 `board.DetectImages` 设定；`board.Run` 多一个 ImageMode 参数，上传在 Refresh 之后。dev 在真 kitty 里 tmux 外跑过看板，头像和 `a` 浮层都在。
- 审查退回（reviewer @4648d7d，探针 cr-t28-6f605fc/probes/）必改：1. tmux 外上传到主屏、卡片在备用屏，一张都不显示（用 kitty 自己的 Screen 回放核实）→ tmux 外先 ?1049h 再上传；2. 用户集目录是符号链接被静默跳过 → fs.Stat 判目录；3. 「被同名集遮住的 sprite 不进池」无测试守（变异存活）→ 断言 Choices 长度与唯一；4. 选不到 client 回落像素无测试守 → TestImageMode 加行；5. 名为 hash 的用户集劫持 default → 保留名跳过并报。采纳建议：a. 图片 id 改 fnv(集+状态+文件) 24 位；b. 跳过 . 开头文件。spec 定稿：像素模式 hash 池只含 sprite（imageMode 在 poller 首次 Refresh 前定）；用户验收加 tmux 外真 kitty。
- dev 自报：`a` 浮层只认 sprite、`agmux avatars` 未列集（T29）；hash 池模数 6→7，e2e 依赖具体 hash 的断言要跟进；默认配置表多 `renderer = "auto"`；图片模式无 agentType 色带；`renderer = "image"` = 跳过终端名检查但 passthrough off 仍回落。两个看板同开时退出删图会删掉对方的图 → 定稿：退出不删。
- 范围：avatar 包加图片集加载（内置 embed + 用户目录）、占位符渲染、启动上传 / 退出删除、探测与 `renderer` 配置、回落；store / board 的头像赋值接受集 id；现有 sprite 代码保留。`a` 列表、`agmux avatars`、README 放 T29。
- 验收：单测 占位符行 StringWidth=6 且每格带行列变音符、前景色 = id；集加载（缺状态回 idle、无 idle 不算、非 png 忽略、帧按名排序）；探测表（termname × passthrough × renderer）；上传序列分块 ≤4096、tmux 里 ESC 加倍、q=2；退出删除每个 id。黑盒（tester）：夹具 tmux 的 client_termname 给 xterm-kitty 时抓屏里头像格是 U+10EEEE 且 SGR 前景是对应 id、随帧 tick 换 id；给 xterm-256color 时仍是像素头像；renderer=pixel 强制像素；用户目录放一套 PNG 后被认出；缺 idle 的集报底栏不退出。真 kitty 观感由用户验收。

### T29 头像图片集的入口（通过 @4fa9c4c / test_t29 25 条）
- 审查退回（reviewer @04f22be，探针 cr-t29-04f22be/probes/）：1. 列表比屏高时 ▸ 跑出画面、enter 绑看不见的头像（用户集无上限）→ 以 pick 为中心取能放进 m.h−2 行的一段；2. 「遮住的 sprite 不列」无测试守 → 断言 Listing 长度 / bot-03 只出现一次；建议 a 底栏 "wears bot-03" 断言也做。其余（占位符同 id 不冲突、截边保留前景色、光标定位、README 与代码一致、署名 © 2026 Eiji Onchi MIT）都过。
- dev 自报未处理：浮层每项 2 行，加了集后更长，窄终端被裁的老问题照旧。
- `a` 列表先列图片集再列 sprite；`agmux avatars` 打印集与 sprite；config `[avatars] renderer`、`default` 可填集 id；README 新节：目录结构、状态与帧、aza_choi 署名（pi-emote，MIT）、kitty/ghostty + allow-passthrough 要求。

## 第四批整批回归（tester 2026-10-13，e7e4f6c，套件 594c3dc）
- go vet / go test -race 全绿；e2e 530 条全过（947s，tmux 3.7c 与 3.8 各跑一遍一致）。test_t12 一条固定 sleep 的时序问题是测试自己的，已改成轮询。
- 黑盒测不到：tmux 外上传写失败时的 ?1049l 兜底（写的是同一个坏 fd，只靠单测）。
- 没覆盖：真 kitty / ghostty 渲染观感与多 client 同看、id 碰撞、flash / 过滤高亮叠在占位符上、20 个以上集的上传耗时、q=2 在真终端上是否真静默。

## 第五批（用户 2026-10-13 下午验收意见：状态动作、头像等宽、选择框改格子 + 20 个默认头像）

### T30 头像状态对齐 pi-emote（通过 @1d4d5fd+2459066 / test_t30 18 条）
- dev 定稿（照准）：其它 status（background）= idle；hi 优先于 working；别名帧 id 按实际目录名哈希；眨眼 tick>0 且 tick%8==0、多帧按 8/16/24 轮；aza_choi 无 success 回落 idle。
- 设计 design.md「头像图片：状态、宽度、选择框」第 1–3 条。store 里按 pendingTool 算出头像状态（和 getEffectiveStatus 一样放 store 做一次，view 只读）；avatar 包的帧选择按新状态名 + 回落链 + 别名；idle 眨眼规则；内置 aza_choi 改用原目录。
- 验收：单测 状态映射表（每种 pendingTool / 无 tool / attention / waiting / done / idle）；回落链；别名目录；idle 第 2 帧只在第 8k 个 tick 出现一个 tick；黑盒 pendingTool=Read 的卡出 read 帧、Write → write、Bash → tool、无 tool → think；旧目录名 working 仍被认。

### T31 头像填满 6 格（通过 @24cf734+38cc815+6cd04df / test_t31 12 条）
- 设计第 4 条（已改）：kitty 忽略占位符放置的源矩形，改为载入时 png.Decode 裁居中 3:2 再编码上传；坏 PNG 跳过并报。
- 验收：单测 128×128 → 128×85、200×100 → 中间 150×100、3:2 原样；坏 PNG 跳过且 Problems 一行；黑盒 上传载荷解码后是 3:2（内置帧 64×42），不带 x/y/w/h。

### T32 选择框改 5×4 格子 + 20 个内置头像（通过 @353a904+ea5df78 / test_t32 27 条）
- reviewer 核过 CC0 来源 zip（license 逐字一致、14 张逐像素 0 差异）；party-* 随 T33 删除。
- dev 定稿（照准）：格宽 9（8 时 8 字符 id 粘连）、整框 47×14；当前穿的那格 = 名字 focus 色 + 光标反白（8×3 格画不出边框）；放不下上方时少画几行；esc / q 都取消。内置 14 张来自 OpenGameArt「Wacky EGA style character portraits」（CC0，Tim Rachor，38×38 → 76×76，只有 idle），许可原文随附，CREDITS.md 记录。
- 设计第 5–6 条。CC0 来源逐个核对许可页后再收，CREDITS.md 记 URL / 作者 / 许可；README 署名。
- 验收：单测 格子布局（焦点卡下方 / 放不下换上方、5×4、格内 id 行、光标起点、滚动行、enter 绑定、消息行 id）；内置集数 = 15、sprite = 5；黑盒 120×40 按 a 抓屏：框在焦点卡下方、20 格、最后一排 5 个像素头像、l/j 移动、enter 后卡片换图、矮终端（高 20）时上方或滚动仍能选到最后一个。

### T33 内置头像换成 pi-agent-portrait 的 10 个角色（通过 @de20b1e+c919932+c2b26dd）
- reviewer 浅克隆上游比对：141 帧清单一致、抽 47 帧像素等于 2×2 均值；许可逐字一致；直通 321 个 DCS 序列最大 4153 字节、分块边界无问题。
- 数字：141 帧、PNG 803 KB、直通后 1.08 MB，载入 73 ms、写出 1.3 ms，浮窗起到看板出现 0.67 s 时头像已全在。aza_choi / cipher / default / red 上游无 success 目录 → 回落 idle（验收「每状态 ≥1」对 success 不要求）。
- 设计 design.md「内置头像换成 pi-agent-portrait 的角色」。只拿 10 个角色目录里七个状态（含别名）下不带 `_v\d+` 的 PNG，预缩 64×64 embed；删 party-*；`_v\d+` 忽略规则加到 loadSets（用户集同样）；格宽 10；CREDITS / README。
- 验收：单测 内置集数 10 且每个 idle/think/read/write/tool/hi/success 帧数 ≥1、不含 `_v2`；用户集里 `idle_v2.png` 被忽略、`idle_2.png` 仍算帧；格宽 10 的布局。黑盒 `agmux avatars` 列 10 个内置 + 5 sprite；`a` 格子 15 格、名字不贴邻格；上传帧数 = 各集基础 take 之和。

## 第五批整批回归（tester 2026-10-13，c2b26dd，套件 4bb5295）
- go vet / go test -race 全绿；e2e 574 条全过（1169s）。首轮 7 条红全是测试跟不上产品（眨眼相位、帧去重、计数），改后整套全绿。
- 没覆盖：真 kitty / ghostty 的填满与观感、CREDITS / LICENSE 内容、最近邻缩放对照片的锯齿、≥100 用户集的上传耗时、≥4096 边长图在真终端的内存。

## 第六批（用户 2026-10-13 下午：22 张卡时底部被截、没法滚；要生成头像的 prompt）

### T34 卡片区可滚动（通过 @6997705 / test_t34；历程 6bdad7a → b8c0a36 → e1a6ff0 → 656a036 → d391fb7 → 6997705；tester 黑盒退回 1 条：开板后第一个滚轮 / ctrl+d 被吞（m.focus 初始空串被当成焦点移动 → followHidden 撤销滚动），修复 @b8c0a36；审查再退 4 必改：同根因在轮询删焦点卡后复现（改比「屏上显示的焦点」）、部分可见边界 / 滚动态点击 / 滚轮 mode 守卫无测试；定稿：过滤态顶栏无 more 提示接受；轮询回落焦点也算移出 → 回到跟随；修复 @e1a6ff0 + 测试 @656a036 复审通过（5 条必改全关闭）。**tester 整套退回 1 条**：e1a6ff0 的 frameMsg 每 tick 调持久 current()，零命中过滤时把记住的焦点置空，Esc 后回第一张（test_t19 第三轮用例红，二分到 e1a6ff0）→ reviewer 查明范围更大：capture()（预览开）和所有按键路径都经持久 current() 清 focus，T19 起就有；定稿在 current() 的 len(gs)==0 分支不清 focus 一处修掉（T19 存量 bug 并入 T34 修复），修复 @d391fb7 复审通过（零命中探针预览关 / 开、8 个键全过）。已知窗口：轮询删卡到下一帧之间来滚轮，焦点回落后不跟随直到下次按键，接受）
- dev 自报：搜索过滤态顶栏被过滤框替换看不到 more 提示；滚动中焦点卡被轮询删掉时焦点悄悄换到第一张、视图停在原偏移直到下次按键；单卡高于正文高的极窄终端没处理。
- 现状：正文只跟着焦点滚（scrollOff），鼠标滚轮只滚预览，底部被截的卡没有任何提示；用户以为是 bug。
- 做法：鼠标滚轮在卡片区（x < cardsW）每格滚 3 行，不动焦点；新增动作 `page_down` / `page_up`（默认 ctrl+d / ctrl+u）滚半个正文高度；手动滚过的偏移保留，直到按键移动焦点把焦点移出可见区才回到「跟着焦点」；顶栏在被截时加提示：下方还有内容 ` · ↓ more`、上方被滚走 ` · ↑ more`（都有就两个）。`agmux config` 打印新动作，README 键位表加一行。
- 验收：单测 滚轮 3 行 / 到底停 / 到顶停；ctrl+d/u 半页；手动滚后 j 把焦点移出可见区则重新跟随；顶栏提示三态。黑盒 120×30 放 22 张卡：滚轮向下后最后一张可见、顶栏 `↑ more`；再按 j 若焦点在屏外则视图跟随。

### T35 `agmux avatars import <sheet.png> --name <id>`（通过 @4aab625+8fa4f3d / test_t35）
- dev 定稿：深色阈值三通道均值 ≤200 定边界，每格取 97% 中间正方形；--force 先移开旧集、换进失败还原；id 不能为空 / hash / 以 . 开头 / 含 /；目录 0755；退出码用法错 2、已存在 1。
- 审查退回 2 必改：--force 换入失败会删掉旧集（bak 只在成功后删、还原失败报 bak 路径）、深色阈值无测试（页边改 201 灰）；采纳：NRGBA 读像素、id 正则同上游 `[A-Za-z0-9][A-Za-z0-9._-]*`、-h usage。不做：边缘杂点加固、layout 错配检测。reviewer 用上游 2-sheet.png 对照过切块一致。修复 @8fa4f3d 复审通过（300 轮竞争 0 丢失）。
- 用户用 GPT 按 pi-agent-portrait 的 6×5 格式生成一张 sheet（prompt 由 orchestrator 给），导入成一个用户图集，文件名沿用 pi 的（idle/idle.png、idle/idle_blink.png、hi/hi1.png … 共 30 格，见 pi-agent-portrait scripts/draw-portrait 的 FRAMES + NEW_FRAMES），这样同一套在 pi 里也能用；我们只读其中七个状态。
- 切法照 draw-portrait：找深色背景的边界，等分成 6×5 格（`--layout 19` 为 5×4），每格取中间的正方形，缩到 128×128 存 PNG。写到 `$XDG_CONFIG_HOME/agmux/avatars/<id>/`，已存在则报错除非 `--force`。只用标准库（image/png + 最近邻）。
- 验收：单测 合成一张 6×5 的假 sheet（每格不同色、深色边）→ 30 个文件、名字对、每个 128²、颜色对应；5×4 布局；非 PNG / 目录已存在报错；黑盒 跑命令后 `agmux avatars` 列出该集七状态帧数，看板 `a` 里能选。

## 第七批（用户 2026-10-13 下午：agents / shell 要醒目；显示 agent 角色定义；默认头像仍是像素）

### T36 后台 agents / shell 醒目 + 图片模式默认头像只在图集里挑（通过 @c464e31+47bde68 / test_t36）
- dev 定稿（照准）：` · Bash` 这类工具名详情仍 muted；Sets 为空时池回落 sprite。
- 用户截图：`⏵ working · agents ⚇ 1` 里 `agents` 和 `⚇ 1` 是 muted 灰，看不出有东西在跑。改：状态行的 ` · agents` 详情和 `⚇ n` / `⚙ n` 计数段都用 `working` 色（和状态词同色），不新增颜色项；让位顺序不变。
- 用户以为新版没装：装了（13:02，c2b26dd），但没绑定的会话按 hash 在「5 个像素 + 10 个图集」里挑，三分之一还是像素。改：**图片模式下 hash 池只含图集**，像素头像只能按 `a` 手选；像素模式照旧只含 sprite。design.md「选择与绑定」那句同步改。
- 验收：单测 agents 详情 / 计数的转义码是 working 色；图片模式 Choices 不含 sprite、像素模式不含集。黑盒 带 subagents 的卡第 3 行 `agents` 和 `⚇` 段是 working 色；图片模式 100 个无绑定会话全是占位符头像。

### T37 卡片显示 agent 角色定义（dev @ebcc6db 审查退回 1 必改（colors.agent 未入 TestEveryColorWired）+ spec 改：来源改为尾部 64KB 最后一条 agent-setting、无则看开头（reviewer 实测 39 个 jsonl：1 个首条在 8.6MB 处、该行反复写且最后一条离末尾 ≤34KB），补丁 @47bde68 复审通过——reviewer 用本机 39 个真实 transcript 逐个比对 0 不一致；通过 / test_t37）
- dev 定稿（照准）：缓存规则——找到即缓存；文件 >64KB 仍无也缓存「无」；短文件无则下次轮询再看。无分支时第 2 行只显示右侧 agent。
- 用户问能不能拿到会话用的哪个 agent 定义（`claude --agent orchestrator` 那个）。能：Claude Code 的 transcript jsonl 里有 `{"type":"agent-setting","agentSetting":"orchestrator",...}` 行（第 1 行，resume 时再写一次）；statusline payload 和 ccmux 都没有。
- 做法：store 读 jsonl **开头 64KB**（现在读尾部 64KB 取模型的逻辑旁边再加一次读头），取第一条 agent-setting 的值存 `Session.Agent`，按 session ID 缓存（不会变）；没有就空。卡片第 2 行右侧靠右显示它（和第 1 行的时间同一规则：始终显示，分支放不下才截），新颜色项 `colors.agent` 默认 `#bd93f9`；`agmux show` 也多一列。README 颜色表加行。
- 验收：单测 jsonl 第 1 行有 agent-setting → Agent="orchestrator"；没有 → ""；只在尾部有（头 64KB 外）不要求读到（写进注释）；第 2 行布局：分支 + 右侧 agent，窄时分支截断 agent 保留。黑盒 夹具 jsonl 带 agent-setting 的卡第 2 行右侧出现 `orchestrator` 且是 colors.agent 色；无的卡第 2 行照旧。

### T38 浮窗（嵌套 tmux client）里图片模式失效（通过 @8f4217d+4d9651f+df3002b / test_t38，真 pty 当 client tty）
- dev 实测（kitty + tmux 3.8，用户的 tmux.conf 与 fl-toggle.sh 原样）：1. chooseClient 选对（外层 kitty client），错在 imageMode 用 `display-message -p -c <client>` 查 termname——**tmux 3.8 里 display-message 不带 -t 时 -c 不生效**，按最近活动 client 展开 → 拿到嵌套的 tmux-256color；2. 单层 passthrough 经嵌套 client 到不了 kitty（被 popup pty 再解析一次）；3. 包两层能到，但常驻 session 先起、popup 后 attach 时上传无处可写全丢。
- 定稿：A termname 并进 list-clients 字段；B 选中 client 是 tmux-/screen- 时改看最近活动的非 tmux client；**C tmux 里上传直写选中 client 的 tty（#{client_tty}），不再要求 allow-passthrough，打不开 tty 回落 passthrough 单层并底栏提示，写完 refresh-client，单次 write ≤ 一块**；D auto 判 off 底栏给原因；E 接受非 -h 的 AGMUX_CLIENT（-h 优先）。design.md 上传一节由 dev 改写。
- dev @8f4217d 送审 / 送测：真机（用户脚本布局、先起后 attach）图集头像全显示；旧代码单层 / 两层包装都是空占位。用户之前偶尔能看到图 = 旧探测恰好选到 kitty client + 同 id 的图早留在 kitty 窗口里。
- 审查退回 2 条补测试（tmux 外选 ToTerminal、refresh-client 失败不致命）+ 采纳 2 加固（openTTY 只认 /dev/ 前缀；O_NONBLOCK，EAGAIN 回落 passthrough），补丁 @4d9651f 复审：2 条关闭，再退 1 条缺测试（O_NONBLOCK 标志）+ 建议 n=0 并进 EAGAIN，补丁 @df3002b 送复审。reviewer 真 tmux 3.8 核过 list-clients 按 tab 拆 5 字段安全、show-environment 各形态处理正确。
- 用户现象（2026-10-13 下午）：~/go/bin/agmux 已是 c2b26dd，但在常驻浮窗里开看板，卡片和 `a` 格子全是像素头像（图集名字在、画的是 hash 出的 sprite）= 图片模式被判 off；底栏没有任何提示（auto 模式判 off 时 note 为空）。
- 用户的浮窗脚本 `~/.config/tmux/scripts/fl-toggle.sh`：程序跑在常驻 session `fl-agmux`（`new-session -d … agmux board`），浮窗是 `display-popup -E "tmux attach-session -t fl-agmux"`——**同一个 server 上多了一个 termname 为 tmux-256color 的嵌套 client**（实测 list-clients：/dev/ttys001 xterm-kitty sess=agmux；/dev/ttys042 tmux-256color sess=fl-agmux，后者活动时间更新）。脚本用 `set-environment -t fl-agmux AGMUX_CLIENT <client>`（**没有 -h**），而 chooseClient 只读 `show-environment -h`，所以 AGMUX_CLIENT 实际没生效，走「最近活动且不在自己 session 的 client」。从别的 fl-* 浮窗切过来时脚本还会 unset AGMUX_CLIENT。
- 先查后改（dev 在自己的 kitty + tmux 里用同一个脚本复现，把结论写进 design.md）：1. 这种布局下 chooseClient 选到哪个 client、为什么（TMUX_PANE → own 是否解析成功；popup client 和 agmux 进程启动的先后）；2. 单层 `\ePtmux;` 包装的上传经「嵌套 client → popup pty → 外层 tmux」能不能到 kitty（之前截图 9 在浮窗里见过图，说明某种情况下能）；3. 需不需要按嵌套层数多包一层。
- 改：a. 嵌套识别——选中的 client termname 以 tmux- / screen- 开头时，视为嵌套一层，终端类型改看同 server 上最近活动、termname 不是 tmux-/screen- 的 client，上传按实测结果决定包装层数；b. **auto 判 off 时底栏给一行原因**（哪个 client、它的 termname / passthrough），用户能自诊；c. chooseClient 同时接受 `show-environment -t <own>`（非 -h）的 AGMUX_CLIENT（优先 -h），README 两种写法都写明。
- 验收：单测 嵌套 client 的探测表、note 文案、非 -h 的 AGMUX_CLIENT；黑盒 fake tmux 给两 client（kitty + tmux-256color，后者更新且在自己 session）→ 图片模式 on、上传包装层数符合定稿；只有 tmux-256color client → off 且底栏有原因。真机：dev 用用户脚本在浮窗里看到图。

### T39 卡片更紧凑一点（通过 @3b5c86b+df3002b / 全套几何适配）
- dev 实查：同列相邻卡之间本来就没有空行（截图里的缝是字体行距），该项无可减；只改 colGap 2→1 和卡内左右边距 1→0。2 列阈值变为 ≥81 列。
- 用户 2026-10-13（浮窗 OK 后）：卡片的 margin 和 padding 都再少一点点。定稿三处各减 1：列间距 colGap 2 → 1；同列相邻卡片之间的空行 1 → 0（圆角边框相接）；边框内左右各 1 格的留白 → 0（头像紧贴左边框，头像和文字之间的 2 格不动，右侧时间 / 模型紧贴右边框）。最小列宽 40 不动（文字区因此多 2 格）；组标题行、顶栏、底栏不动。
- 牵连：rects / 鼠标命中 / flash 标签坐标 / `a` 格子定位 / 滚动的 scrollOff 都按新几何；相关单测和 e2e 的坐标断言要跟着改。README 若有画卡片示意图同步。
- 验收：单测 两张同列卡 rect.y 相差 cardH；列 x 相差 colW+1；卡片第 1 行 `│` 后紧跟头像格、末尾时间后紧跟 `│`；黑盒 抓屏对比：120 列 2 列布局下第二列起始列号、22 张卡在 30 行里比之前多显示一张。

### T40 组排布可配：`[layout] flow = "bands" | "stack"` + 组标题横线（通过 @629e877+df3002b / test_t40；bands 8 宽度 × 折叠抓屏逐字节一致（仅 ━ 段））
- dev 定稿（照准）：stack 下所有 rect band=0、只有 min(n, 组数) 列；标题被 midTrunc 后占满整列就不画线；焦点在标题时文字 focus 色。
- 用户 2026-10-13 看了对比页（artifact 8xRhqcZ7B5Nm5AVap3TUx6）：选方案一，但要保留现在的分层风格、加配置切换；组之间用标题行 + **粗实线**（`━`，U+2501）拉满列宽分隔，不要虚线、不要组边框。
- 做法：`[layout] flow`，默认 `"bands"`（现状不变），`"stack"` = 第 k 个组进第 k mod n 列、每列独立往下堆（不做瀑布流）；非法值报错一行。组标题行两种模式都改成 `▾ ~/路径 (n) ━━━━━━` ——标题后空一格、`━` 画到列的右边缘、颜色 `muted`，标题文字仍 `dir` 色；折叠的组同样画线。`agmux config` 打印 flow，README 配置示例和布局说明加一段；m1 design「放不下的组折到下一行」加 stack 的说明。
- 验收：单测 stack 分配（k mod n、列内 top 连续、无层对齐）、bands 逐位不变、标题行以 ━ 填满到列宽且 StringWidth = 列宽、折叠标题同样；配置非法值报错。黑盒 120×30 22 张卡 flow=stack 时第二列第一张卡的 y 与第一列无关、组标题行末尾是 ━；默认 flow 抓屏与之前一致（除标题行多了 ━）。
- 现状：组按「带」排——每 n 个组一层，层高 = 最高的组，矮的组下面留空。
- 方案一（推荐）：去掉层对齐，组按序轮流分到各列（第 k 个组进第 k mod n 列），每列自己往下堆，上一组结束下一组紧接着。没有空洞；组到列的分配稳定，轮询增减会话只改本列高度。缺点：列高不齐（有的列长有的短），最多差一个组。
- 方案二：瀑布流——下一个组进当前最矮的列。列高最均衡，但会话增减改变列高后，组会在列之间跳来跳去，看板每次轮询都可能重排。
- 两种都是 layout() 里分配规则的改动，h/l 的空间导航、折叠、flash、滚动、鼠标命中都按 rects 走不用动；m1 design「放不下的组折到下一行」那句改。

### 备忘：子 agent 下拉框（2026-10-13 用户问「有没有必要」；结论：暂不做）
- 想法：卡片上点 `agents` 下拉一个框，列出子 agent 的信息 / 交互。
- 数据：ccmux `subagents` 只有 agentId、status、attentionType、pendingTool、lastActivityAt、startedAt、worktreePath——没有名字和任务描述；Claude Code 自己列表里的名字和「Committing quick-add styles…」来自父会话 jsonl 的 Task tool_use（subagent_type、description），要自己解析并按 agentId 对上。
- 交互：子 agent 没有 pane，不能跳转、不能发消息，框里只能看。
- 判断：`p` 预览已经能看到 Claude Code 屏幕底部那份列表，enter 跳过去也是一键；T36 已把 `agents ⚇ n` 改醒目。做下拉框是中等工作量换一个只读视图，先不做；要做时从 jsonl 的 Task 调用取 subagent_type + description。

## 第六 + 七批审查最终结论（reviewer 2026-10-13，df3002b）
- T34–T40 全部通过，无遗留必改；各任务关键变异在 df3002b 上整套重跑全被杀。
- 不阻塞的遗留：T34 轮询删卡到下一帧之间（≤500ms）来滚轮，焦点停屏外直到下次按键；T35 页边杂点使网格漂移、`--layout 19` 用于 30 格 sheet 静默错切（照搬上游）；T38 外层终端停读 2s 后回落 passthrough 只有单测 / pipe 验证，未在真机试；T40 ruleGE / foldedRectH2 视觉等价。
- tester 整套退回：d391fb7 的「零命中不清 focus」把会话列表真空也一并保留，透传锁（T21「session is gone」）不再判丢（test_t12 红，二分到 d391fb7）→ 定稿显式锁存（reviewer：「只在 query 非空时保留」修不全，保留过滤时照样复现）：frameMsg 和按键入口处 pvFocus 为真而目标不在 Sessions() 里就置空 pvID，之后 passThrough 为假直到 tab / esc 重锁；d391fb7 不回退。reviewer 查过其它靠 focus 变空的逻辑只有透传锁。修复 @6997705（dropLostLock，帧与按键入口各一次）复审通过：relock 三探针 + 零命中两探针全过；锁存是采样式（帧 / 按键），和以前同一采样点；pane 变了又变回也保持锁住（比 T21 更严，照准）。等整套回归。

## 第六 + 七批整批回归（tester 2026-10-13，6997705，套件 42c0edf）
- go vet / go test -race 全绿；e2e 701 条全过（tmux 3.8）。两条红均为测试侧（几何适配期望列写错；眨眼换帧间隔被 capture-pane 采样停顿晚记，断言改成均值 0.4–0.6s 且不漏拍），已改。有一轮出过 2 个 error 未留输出、未复现（当时 load≈7.5），记为未复现。
- 没覆盖：真 kitty / ghostty 观感与 q=2 是否静默、?1049l 失败兜底、真 pi sheet、≥100 用户图集耗时、flash 叠在占位符上、--force 竞态稳定复现。

## 第八批（用户 2026-10-13 晚验收第六 + 七批通过后的意见）

### T41 分支名放不下时省略前缀、保留后缀（通过 @32a2435 / 16 条；dev @9730bc7 审查退回 1 必改（多字节前缀省略后高亮无测试，收穷举探针）+ 采纳建议（尾截 … 被黄底渗到，存量）；定稿：末段刚好放下但加 … 放不下时显示光秃末段，接受；修复 @64c17a1 复审再退 1 条：spread 的 reset 把名字行 … 的名字色抹掉 → 截断标记由调用方传 cutMark(c)，补丁 @32a2435 复审通过；建议 line3 的 cutMark 无测试，不改）
- dev 顺带修：flash 之前没按 agent 让出的宽度读分支。
- 用户：`feature/permission-dashboard`、`hotfix/alert-group` 放不下时现在从尾部截（`hotfix/alert-gro…`），有用的是后缀。改：放不下时先从左边一段一段去掉 `/` 前的前缀，剩余部分前面加一个 `…`（`…alert-group`、`…permission-dashboard`），直到放得下；只剩最后一段还放不下才像现在一样从尾部截。没有 `/` 的分支照旧尾部截。第 2 行右侧有 agent 角色时同样适用。搜索 / flash 的命中高亮按显示出来的字符算（前缀被省略的部分不画高亮）。
- 验收：单测 宽度刚好放下全名不省略；少 1 格 → `…alert-group`；三段 `a/b/c` 逐段去；只剩末段仍放不下 → `…alert-gr…`→ 定稿写成末段尾截 `alert-gr…`（前面不再加 …，避免两个省略号）；无 `/` 尾截。黑盒 窄列抓屏。

### T42 组标题横线颜色改成和组名一致（通过 @a2d5f83）
- 用户：横线现在 muted 灰不醒目，要和分组名同色（`dir` 色）。改：━ 用 `dir` 色（焦点在标题上时随标题用 focus 色）。design.md T40 那句同步。
- 验收：单测 标题行的 ━ 转义码 = dir 色；黑盒抓屏带色核对。

### T43 分组方式可切换：cwd / project / session / state，`g` 循环（通过 @7dd2465 / 32 条；dev @eb8759f 审查退回 2 必改：焦点在折叠标题上按 g 跳到全板第一张（改为先落到该组第一张卡）；flash 读显示标题 / g 结束手动滚动 / visibleFocus 用当前分组键三处无测试，收 4 探针；修复 @7dd2465 复审通过）
- dev 定稿（照准）：regroup 在 shown() 里换 GroupKey / GroupLabel，其余（layout / flow / 搜索 / flash / 鼠标 / `a`）不动；project 空 → "(no project)"；切换时焦点留原卡、手动滚动结束；名字标题不画路径命中；`agmux show` 分组列仍 cwd。底栏又长一段，120 列下尾部更早被截。
- 定稿：切换方式清空折叠，各方式互不影响，切回不保留。
- 用户：现在只有按 cwd 一种；参考 ccmux 加 project、tmux session，再加一个 state。`g` 切换。
- 定稿：四种 `group_by`：`cwd`（现状，默认）→ `project`（ccmux 的 `project` 字段：worktree 归到主仓库名下，标题 = 项目名，如 `shopcart (8)`）→ `session`（`tmuxTarget` 的 session 名，没有 pane 的后台会话归 `(no pane)`）→ `state`（working / waiting / done / idle 四组，按这个顺序，不按名排）。`g`（新动作 `group`，默认 g）按上面顺序循环；底栏显示 `g group:cwd`；config `[layout] group_by` 定启动时的方式；折叠状态按「方式 + 标题」记，切换方式后各组展开。组内卡片排序不变（名字序）；state 分组里卡片仍显示自己的状态行。标题的路径缩写只用于 cwd；project / session / state 标题直接写名字。`flow` 两种模式都适用。
- 验收：单测 四种分组的 key 与标题、state 的固定顺序、g 循环、配置值校验（非法报一行）、折叠记忆按方式隔离、`agmux config` 打印 group_by。黑盒 夹具里给两个 worktree + 主仓库同 project → project 模式合成一组；session 模式按 tmuxTarget；state 模式四组顺序；g 三次回到 cwd；底栏文案。

### T44 `agmux board --stay`：常驻看板，enter 跳转 / q 都只收起浮窗不退出（通过 @fabdc73 / 23 条，真 display-popup）
- dev 偏离（照准）：popup client = own session 上 termname 以 tmux/screen 开头且不是跳转目标的 client（只按 own session 会把跑在普通 pane 里的用户终端 detach 掉）；无 popup client 时退化成现状且无提示（退化即退出）。
- 审查退回（reviewer @dad5f31）3 必改：只 detach 自己 session 上的 popup 无测试（别的 fl-* 浮窗会被一起 detach）、detach 失败无测试、main.go 死代码 `case "board"`；采纳建议：list-clients 失败时报错留进程而不是退出。不做：退化时底栏仍写 q hide、不在 tmux 提示无单测。备忘：嵌套 ssh 两个 tmux-* client 同在 own session 时非目标的会被 detach（极少）。修复 @fabdc73 复审通过；等真 tmux 黑盒与整套回归。遗留不阻塞：退化时底栏仍写 q hide；不在 tmux 的提示无单测。
- 用户 2026-10-13：浮窗里 enter 跳转后看板进程退出，下次 ctrl+1 要重起；想要回车跳到对应 pane 但看板不关，下次打开还是原来的进程。用户的 fl-toggle.sh 已经是「程序跑在常驻 session fl-agmux，浮窗 = display-popup -E 里 attach 它」，进程退出 session 才没。
- 做法：`agmux board --stay`（只加旗标，不加配置项；脚本里传）。stay 模式下：`jump`（enter / 再点卡片）= 照旧 `switch-client` 把外层 client 切到目标 pane，然后 **detach 掉 attach 在自己 session 上的那个 popup client**（chooseClient 里已算出 own session；popup client = list-clients 里 session 为 own 的 client），进程不退出；`quit`（q / esc）同样只 detach；新增动作 `exit`（默认 `Q`，`agmux config` 打印）真正退出进程。不在 tmux 里、或自己 session 上没有 client（不是常驻布局）时 --stay 退化成现状（底栏一行提示 "--stay needs the board in its own tmux session"），不报错。
- 收起期间轮询照旧（500 ms tick 不停）；再 attach 时 bubbletea 收到 resize 重画，焦点留在跳走前那张卡；图片已在 kitty 里，不重传。`a` 换头像 / 搜索保留态等状态都留着。
- README：浮窗一节加 --stay 的用法和 fl-toggle 式脚本示例（new-session -d … agmux board --stay）；底栏 stay 模式下 `q hide · Q quit`。
- 验收：单测 stay 下 jump 发 switch-client + detach-client（目标 = own session 的 client）且不 Quit；quit 只 detach；exit Quit；非 stay 行为不变；own 无 client 时退化 + 提示。黑盒 真 tmux：常驻 session + popup attach，enter 后 popup 关、外层 client 到目标 pane、进程仍在、再 attach 看到同一焦点；q 同样；Q 退出进程。

### T45 在看板里看过的 done 就算已读（通过 @7cefc08 / 21 条；dev @975ae2a + 补丁 @8dc339c；审查退回 3 必改：同尺寸再 attach 不发 WINCH（reviewer 真 tmux 实测）→ hidden 期间轮询 `#{session_attached}` 清除；dwell 焦点换卡 / 非 done 两处清零无测试；MarkSeen 比较 statusChangedAt 无测试；修复 @7cefc08 复审通过——reviewer 真 tmux 实测 session_attached 在 detach 返回后立刻为 0、20 轮无窗口；等整套回归）
- dev 核实 statusChangedAt 只在状态变化时变。定稿：hideBoard 自己 detach 后 hidden=true，期间 dwell 不计，任何输入 / resize 清除；脚本从浮窗 detach 的情况不知道，接受。多看板互相误删 seen 条目接受；seen.json 复用 avatar.FileBindings 接受。
- 用户 2026-10-13：done 的卡要跳过去 pane 才变 idle（ccmux 在 pane 获焦后清 attentionState），想在看板里 focus 到它就算看过，省得反复确认。
- ccmux 没有 ack 命令，`show --json` 的 attentionGeneration 不随每次 done 递增（实测 state=unread 而 gen=0 的会话存在），所以 agmux 自己记：**已读 = (session id, statusChangedAt)**（status 变成 idle 的时刻；dev 核实该字段在「再次工作后又完成」时会变）。记录写 `$XDG_STATE_HOME/agmux/seen.json`，启动读、轮询时清掉已不存在的会话。
- 触发：焦点在这张卡上**连续 2 个 tick（1 s）**（j/k 路过不算）、或在它上面开预览 / 进直通、或 enter 跳转。标为已读后卡片按 idle 画（状态词 `⏸ idle`、头像 idle 态、边框 idle 色），`agmux show` 也按已读显示（store 里做，view 只读）。ccmux 之后真的清掉 attentionState 时记录自然失效（删掉该条）。新的 done（statusChangedAt 变了）重新显示。
- 不做：告警 / waiting 的已读（那是要你处理的，不能看一眼就算）。
- 验收：单测 2 tick 后 Done=false 且 seen.json 有条目、1 tick 不算、路过不算、预览 / 直通 / enter 立即算、statusChangedAt 变化后重新 done、ccmux 清掉后条目删除、文件坏了当空且底栏一行；黑盒 真 tmux 夹具 done 卡 focus 1 s 后变 idle，重启看板仍 idle，夹具改 statusChangedAt 后回 done。

## 第八批审查最终结论（reviewer 2026-10-13，32a2435）
- T41–T43 全部通过，无遗留必改；等 tester 整套回归。

## 第八批 + T44 + T45 整批回归（tester 2026-10-13，7cefc08，套件 f372230）
- go vet / go test -race 全绿；e2e 804 条全过（tmux 3.8，1674 s）。
- 观察（不当缺陷，待定夺）：`(no project)` 按字节序排最前而 `(no pane)` 固定最后、大小写按字节序；`agmux show` 只有 status 列，done / 已读都显示 idle；list-clients 失败的消息行以命令行开头，120 列下原因被截；底栏加 g 后约 167 列，165 列以下 `q quit` 看不到；有 attention 的 working 会话在 state 模式落 working 组。
- 没覆盖：真 kitty / ghostty 观感与 q=2、?1049l 兜底、真 pi sheet、≥100 用户图集耗时、flash 叠在占位符上、--force 竞态、嵌套 ssh 两 tmux-* client、脚本从浮窗 detach 时 hidden 靠轮询、fl-toggle.sh 真脚本未跑。

### T46 看板启动时把头像图片数据当文字刷屏（缺陷，阻塞验收；dev 修法已照准，等提交）
- 用户 2026-10-14：直接跑 `~/go/bin/agmux board`（7cefc08），看板没画出来，屏幕被几十行 base64 文本刷满（截图 images/20.png）。环境待用户补：终端、在不在 tmux 里、`$TERM $TERM_PROGRAM $TMUX`。
- 交 dev 先找根因（真 kitty 三条路复现：tmux 外 / 普通 pane / 常驻 session + popup），怀疑点：ToTerminal 路径的转义序列完整性、上传与 bubbletea 并发写同一 fd、TERM 是 kitty 而真终端不是、自定义图集某张异常。修法等拍板。
- 用户补充：kitty；popup 浮窗和普通 pane 都出，即直写 client tty（ToTTY）这条路。
- dev 根因（真 kitty + script 录字节复现 3/3）：macOS pty 输出队列只有 1024 字节，一个 4142 字节的 APC 必被拆成多次部分写，间隙里 tmux client 往同一 tty 写自己的绘制；kitty 在 APC 里遇到 BEL 或 ESC \ 就提前结束，剩下的 base64 当文字打印（CSI / CAN 等只是吞掉）。用户 tmux 开了 set-titles / automatic-rename，标题变更带 BEL。T38 时只有 10 套内置图集碰上概率低；拷进 5 套 GPT 图集后 2.7MB / 764 个 APC，每次启动截 25 个左右。不是 T39–T45 引入，T38「一次 write 一块最安全」的假设错了。
- 定稿修法：ToTTY 的 chunk 4096 → 768（整序列 <820 字节），写前 ioctl TIOCOUTQ 问队列空位，放得下整个序列才一次写完，放不下等，2 s 无进展仍 stall 回落 passthrough；ToTerminal / ToPassthrough 不变。dev 实测修后 3 × 3676 个 APC 0 截断，上传 3.1 s → 0.55 s。
- 遗留不阻塞：查队列到写之间 tmux 恰好写大块仍可能部分写（概率低几个量级）；tmux 自己 >1KB 的写被我们的序列插进去（原来就有，refresh-client 重画）；TIOCOUTQ 真 pty 只手工验过，单测用假队列。
- 备忘：每次启动全量上传 2.7MB 是隐患，可改成只传当前会话用到的集、选头像时补传；这次不做。
- 验收：单测 每次 write 是完整序列且 ≤1024、非末块负载 4 的倍数、拼起来解得出 PNG、放不下就等、满队列 stall；reviewer 变异探针；tester 跑图片相关 e2e + go test -race + 整套回归。
