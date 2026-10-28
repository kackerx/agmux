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

### T46 看板启动时把头像图片数据当文字刷屏（通过 @c3e964b / 新增 5 条；整套 809 条 OK；用户复测通过）
- 用户 2026-10-14：直接跑 `~/go/bin/agmux board`（7cefc08），看板没画出来，屏幕被几十行 base64 文本刷满（截图 images/20.png）。环境待用户补：终端、在不在 tmux 里、`$TERM $TERM_PROGRAM $TMUX`。
- 交 dev 先找根因（真 kitty 三条路复现：tmux 外 / 普通 pane / 常驻 session + popup），怀疑点：ToTerminal 路径的转义序列完整性、上传与 bubbletea 并发写同一 fd、TERM 是 kitty 而真终端不是、自定义图集某张异常。修法等拍板。
- 用户补充：kitty；popup 浮窗和普通 pane 都出，即直写 client tty（ToTTY）这条路。
- dev 根因（真 kitty + script 录字节复现 3/3）：macOS pty 输出队列只有 1024 字节，一个 4142 字节的 APC 必被拆成多次部分写，间隙里 tmux client 往同一 tty 写自己的绘制；kitty 在 APC 里遇到 BEL 或 ESC \ 就提前结束，剩下的 base64 当文字打印（CSI / CAN 等只是吞掉）。用户 tmux 开了 set-titles / automatic-rename，标题变更带 BEL。T38 时只有 10 套内置图集碰上概率低；拷进 5 套 GPT 图集后 2.7MB / 764 个 APC，每次启动截 25 个左右。不是 T39–T45 引入，T38「一次 write 一块最安全」的假设错了。
- 定稿修法：ToTTY 的 chunk 4096 → 768（整序列 <820 字节），写前 ioctl TIOCOUTQ 问队列空位，放得下整个序列才一次写完，放不下等，2 s 无进展仍 stall 回落 passthrough；ToTerminal / ToPassthrough 不变。dev 实测修后 3 × 3676 个 APC 0 截断，上传 3.1 s → 0.55 s。
- 遗留不阻塞：查队列到写之间 tmux 恰好写大块仍可能部分写（概率低几个量级）；tmux 自己 >1KB 的写被我们的序列插进去（原来就有，refresh-client 重画）；TIOCOUTQ 真 pty 只手工验过，单测用假队列。
- 备忘：每次启动全量上传 2.7MB 是隐患，可改成只传当前会话用到的集、选头像时补传；这次不做。
- 验收：单测 每次 write 是完整序列且 ≤1024、非末块负载 4 的倍数、拼起来解得出 PNG、放不下就等、满队列 stall；reviewer 变异探针；tester 跑图片相关 e2e + go test -race + 整套回归。
- tester 第一步（ba59d3b）：vet / race 全绿；图片相关 e2e 116 条只有 1 条旧断言（tty 路径与外路径序列逐块一致）按新机制改；新增 test_t46.py 5 条（假 pty 慢读 + 每 2 ms 插 BEL：修前 292/321 个 APC 被打断，修后 0，BEL 拉到每 0.1 ms 仍 0；序列 <1024、载荷 768 对齐、外路径仍 4096）。据此先装给用户复测（09:21，产品代码 ba59d3b）。
- tester 真 kitty 验证：kitty --config NONE + script 录 tty + tmux -L set-titles on 每 30 ms rename-window，15 套图集。修前 764 个 APC 截 55–59 个（全是 BEL/ST），修后 3 × 3676 个 0 截断；上传 ~3 s → ~1 s（含启动与静默判定）。没做：真 kitty 肉眼看渲染、常驻 session + popup 路径、Linux pty。
- reviewer（ba59d3b）：macOS 修法成立，真 pty 探针复核（HEAD 4392 个 APC 0 截；chunk 回 4096 → 842+/963 截；只切小块不问队列 → 2239+/4392 截，问队列是必须的）；room 用 q+n<=1024 刚好对；ioctl 出错退路照常上传；stall 后半截序列用 kitty headless 回放证实不出文字。退回 1 必改：outQueue 真 ioctl 无测试（换成别的 ioctl 号 macOS 返回 0 → room 恒真，修法失效而单测照过）→ 加 darwin 真 pty 测试。不改：room 多余判断、stall 半截、狂刷输出下上传耗时几十秒（修前就有）。
- 定稿：Linux 不在本次范围——Linux pty 的 TIOCOUTQ 恒 0、队列 8–12 KB，room 恒真，只剩小块降低概率（reviewer 实测修后仍 1–65 个被截，比修前好无退化）；design.md 遗留补一句。备忘：Linux 走阻塞写（实测原子）+ 超时兜底，以后单开。
- dev 补丁 c3e964b：images_darwin_test.go 真 pty 验 outQueue（写 500 → 500；再写 4000 只收 524、队列 1024、room(1) 假；读空后 room(1024) 真），变异 TIOCOUTQ→FIONREAD 被杀；design.md 补 Linux 不在范围。送 reviewer 复审；tester 整套回归按 ba59d3b 跑完后在 c3e964b 补跑定点。

### T47 `--stay` 在普通 pane 里也常驻：enter 跳转后进程不退出（复审通过 @a43c00e，等 tester 黑盒 + 整套回归）
- 用户 2026-10-14：在普通 pane 里跑 `agmux board --stay`，enter 跳转后进程退出回到 shell（截图 images/21.png）。想要不管在普通 pane 还是 display-popup 里，enter 跳转都只是切过去，看板进程留在原 pane 继续跑；不要只针对 popup。
- 定稿：`--stay` 下 `jump`（enter / 再点卡片）一律不退出：照旧 switch-client / select-window / select-pane 把 client 切到目标 pane；own session 上有 popup client 就 detach 它（现状），没有就什么都不 detach，看板留在自己 pane 里继续轮询。去掉「--stay needs the board in its own tmux session」的退化与提示。
- `quit`（q / esc）：有 popup client 时照旧只 detach；没有 popup client（普通 pane）时等于 `exit`，真退出。底栏按实际情况写：有 popup → `q hide · Q quit`，无 popup → `q quit`（`Q` 仍可用）。
- hidden / 已读计时：普通 pane 下看板一直可见，不进 hidden，dwell 照常计（接受）。不在 tmux 里时 --stay 仍退化成普通行为（没有 pane 可跳）。
- 验收：单测 stay + 无 popup client：jump 发 switch-client 且不 Quit、不发 detach；quit 发 Quit；底栏文案两种；stay + 有 popup 行为不变；非 stay 不变。黑盒 真 tmux 普通 pane 里 --stay：enter 后 client 到目标 pane、进程仍在、切回看板 pane 焦点仍在原卡；q 退出。原 T44 退化用例改掉。
- reviewer 复审 c3e964b 通过：TIOCOUTQ→FIONREAD 变异被新测试杀，-count=30 稳定，交叉 vet 过；仅存等价变异（ioctl 出错吞错）。
- 用户 2026-10-14 复测 ba59d3b：不刷屏了，头像正常。等整套回归结论后关闭。
- dev b3285b7：jump 一律不 Quit，有 popup 才 detach；switchAndHide 去掉回落 switchTo（AGMUX_CLIENT 精确指定时会 detach 用户自己的 client）；底栏 Init 问一次、之后每秒问 list-clients 的 client_termname 决定 `q hide · Q quit` / `q quit`，收起期间与非 stay 不问；不在 tmux 提示改成 `--stay only works inside tmux`（照准）；README 两种用法。
- reviewer（b3285b7）退回 1 必改：底栏「有无浮窗」看 own session 上任意 tmux-* client，而 q 走 chooseClient + popupClients（排除选中的），用户终端本身是 tmux-* 只有它 / 只有一个浮窗无外层 client 时底栏写 `q hide` 按 q 却退出 → askPopup 复用 q 的判断。采纳建议：list-clients 失败时保留原底栏（nil）+ tmuxFail 用例；「frame 1 not asked」加断言。不做：Run 初值与提示文案无测试。
- 定稿：嵌套 / ssh 的用户 client（termname tmux-*）挂在 own session 上、又有别的 client 在其他 session 时，enter 切错 client 并把用户终端当浮窗 detach——T44 起就有，这次不修；README 改成「本身不是 tmux 的 client 从不 detach；你的终端本身是 tmux 时若还有别的 client 在其他 session 上可能被当成浮窗」。
- 备忘：区分浮窗与嵌套用户 client 的可用特征——display-popup 里 attach 的 client 其 client_pid 的父进程是 server 的 #{pid}（reviewer 真 tmux 实测），嵌套 / ssh 的不是；以后单开任务加进 popupClients。
- dev a43c00e：askPopup = chooseClient + popupClients（errNoClient → false，其它错误 → nil 底栏不变）；TestStayFooterFollowsPopup 重写四种情况与 q 一致 + 第 1 帧不问 + 失败不变；README 措辞；TestSeenNotWhileHidden 改成只数 session_attached。每秒 2–4 次 tmux 调用，未省。

### T48 done 已读只认预览 / 跳转，不再按停留计时（复审通过 @b0e0ba3，等整套回归）
- 用户 2026-10-14：焦点在 done 的卡上停一下就被取消 done，但并没有打开预览；要改成只有预览过或跳过去 focus 了 pane 才算看过结果。
- 定稿：T45 的「焦点停 2 tick 即已读」去掉；已读只在三种动作时记：在这张卡上开预览、进直通、enter 跳转（已有）。j/k 路过和停留都不算。hidden 标志若只为 dwell 而存在就一并删，T47 收起期间不问浮窗的逻辑还用它则保留。design.md T45 那段同步。
- 验收：单测 焦点停 10 tick 仍 done、seen.json 无条目；预览 / 直通 / enter 立即已读（原用例保留）。黑盒 真 tmux 夹具 done 卡 focus 3 s 仍 done，开预览后变 idle。原 T45 dwell 用例改掉。
- reviewer 复审 a43c00e 通过：四种情况底栏与 q 一致，变异 11 杀 9；两条小建议（失败后 quit 方向不变的断言、两个非 tmux 终端无浮窗的用例）并入 T48 的 commit。

### T49 内置头像再加 10 套：GPT 做的 5 个角色 + 5 个火影角色（复审通过 @fdac3c9，等整套回归）
- 用户 2026-10-14：GPT 又做了 5 个火影主题（naruto / sasuke / sakura / kakashi / itachi，`~/Pictures/pi-agent-portraits-naruto/emotes/`，每套 30 帧 + sheet，1134×944），连同之前的 5 套（developer / researcher / operator / tester / designer，`~/Pictures/pi-agent-portraits/emotes/`）都进 agmux 默认，用户不用单独装。
- 定稿：这 10 套按现有内置格式嵌进 internal/avatar/sets/（只取 agmux 用到的 7 个状态目录 idle / think / read / write / tool / hi / success，其余表情和 sheet.png / emotes.json 不嵌）；帧预先裁成居中 3:2 并缩到高 128（和加载时一样，再大是浪费），PNG；dev 回报嵌入后体积，总量超过约 8 MB 就缩到高 96。原件只读，不动用户 ~/Pictures 和 ~/.config。
- 顺序：`a` 选择框里内置 pi 10 套在前，接 developer / researcher / operator / tester / designer，再 naruto / sasuke / sakura / kakashi / itachi，像素 5 个仍在最后；hash 池包含新 10 套。
- 用户目录同 id 覆盖内置（用户之前把 5 套拷进了 ~/.config/agmux/avatars/，内置后那份多余，告诉用户可删，不替他删）。
- 版权备注：火影角色是集英社 / Pierrot 的版权形象，GPT 生成的同人图嵌进开源发布的二进制有风险；用户已知情并决定加入，发布时 oss-maintainer 再提一次。
- 验收：单测 内置集数 = 20、每套 7 个状态都有帧、帧高 ≤128 且 3:2、id 顺序；用户目录同 id 覆盖；`agmux avatars`（或等价列表）列出 20 套。黑盒 `a` 选择框 5 行滚动、新套头像在真 kitty 可显示（tester 字节流 + 截图）。
- dev 7f7a205：dwell、hidden、asking、attachedMsg、doneMsg.hid、seenFrames 整套删（hidden 只为 dwell 存在；T47 收起期间不问浮窗也随之去掉，代价是收起期间每秒照常问、再 attach 后底栏最多晚 1 s，照准）；焦点停 10 帧 + j/k 仍 done；T47 两条小建议测试并入；README 去掉「停 1 秒」。
- tester 终结论（c3e964b）：整套 809 条 OK（在 ba59d3b 上，1697 s；c3e964b 仅多单测，定点复跑 OK）；套件 d25ea78。已装用户的 ba59d3b 产品代码与 c3e964b 相同，不重装。
- dev dd7ada1：sets 1.4 → 4.1 MB，二进制 7.82 → 10.33 MB（不用缩 96）；原帧本就 128×128，只裁居中 3:2 成 126×84（128×85 会被 cropFor 再裁一列，「上传 = 存储」不成立）；每套 7 态各 2 帧共 14 帧；转换器 tools/embedsets 可重跑；builtinOrder 固定顺序；用户同 id 覆盖内置（原规则）；CREDITS.md 注明来源与火影版权；README 提示用户可删 ~/.config 里的同名拷贝。副作用：无绑定会话的默认头像（hash 池 20 套）又会变一次；每次启动上传 20 套，dev 补数字。
- dev 补数字：20 套全内置时一次启动上传 4.4 MB / 5796 个序列（T46 时 15 套 2.76 MB / 3676 个、0.55 s），估 <1 s；备忘「只上传当前用到的集」优先级提高。
- reviewer（7f7a205）：行为正确，三种触发只在 done 卡记、停留与移动不记均有变异杀；退回 1 必改：switchAndHide 的 hidden 返回值已死 → 只返回 error（建在 dd7ada1 之上）。e2e test_t45.py 6 条 dwell / hidden 用例、lib.py 的 session_attached 分支、README 描述交 tester 改。不做：预览中变 done 再关预览记已读的用例。
- reviewer（dd7ada1）：嵌入内容无误（140 帧全 126×84、逐字节 = 上传、重跑转换器 diff 一致、embed 范围只含 sets、顺序测试锁住）；真 pty 20 套 4.41 MB / 5796 个 APC 0 截，快读 0.11 s、慢读 1.8 s。退回 1 必改：tools/embedsets 先 RemoveAll 再检查，源参数 `.` 会删光 internal/avatar/sets → 临时目录转换成功后再替换 + 拒绝 "." / ".." / "/"。采纳：CREDITS 两处措辞（第二三组适用许可、火影版权归属与非官方声明）、avatar_ext_test.go 386 溢出一行。不做：embedsets 重抄常量。升级后无绑定会话默认头像大多会换，验收报告告知用户。
- dev：T48 修复 b0e0ba3（switchAndHide 只返回 error）；T49 修复 fdac3c9（临时目录转换成功后再替换、拒绝 . / .. / 分隔符源、run() 可测变异 4 杀；CREDITS 措辞；386 常量）。本轮 T47–T49 目标 hash = fdac3c9，tester 起整套回归。
- reviewer 复审：T48 b0e0ba3、T49 fdac3c9 均通过（误删复现被拒、坏输入不动旧套、重跑 diff 一致、变异 6 杀 5；CREDITS 与 386 满足）。不补：临时目录名以点开头无测试。本轮最终 hash fdac3c9，等 tester。

### T50 `agmux config` 打印带注释的完整模板（通过 @6b683ca / 12 条；套件 3b6f716）
- 用户 2026-10-14：配置文件里把所有可配置项都列上，带默认值，注释里写可选项，全英文。我已按现有选项手写了用户的 ~/.config/agmux/config.toml（保留他的 max_columns=5 / flow=stack / renderer=image / border），模板（默认值版）存 specs/m3-config/config.example.toml。
- 定稿：`agmux config` 的输出改成 config.example.toml 这套带注释的格式，值用当前生效值（注释里的 default 不变）；每个动作 / 颜色一行加尾注释，[layout] / [avatars] 的可选项写在注释里，[avatars.bind] 的绑定照常打印在节下（注释在前）。README 配置一节说明 `agmux config > ~/.config/agmux/config.toml` 可生成完整文件。不加新子命令。
- 验收：单测 打印结果能被 Parse 回读且等价（round-trip）、每个 Actions / Colors 都有一行、注释含 default；黑盒 `agmux config` 输出与模板逐行比对（值处除外）。
- dev fe4da7d：输出 = 模板格式，值当前、default 固定；按代码改了模板措辞（f1..f20、backspace/home/end/pgup/pgdown/alt+；confirm 与 leave_preview 可共用键；选择框写的是 avatars.json 且优先于配置；hash 说明；border "" = follow the status；头注加 $XDG_CONFIG_HOME；补齐动作用途）；config.example.toml 已用 fe4da7d 的默认输出同步。测试：模板结构 / round-trip / configCmd 真跑；变异 5 杀。
- 定稿：整套回归以 fdac3c9 那轮为准（T50 只改 `agmux config` 输出），T50 在 fe4da7d 上跑 config 相关 e2e + 单测即可；装用户的是 fe4da7d。
- reviewer（fe4da7d）：注释与代码事实逐条一致（键名范围、modal 共用键、avatars.json 优先、hash 池、顺序、`#` 在字符串里安全）；退回 1 必改：border 行「terminal default」与「follow the status」自相矛盾，代码只有后者 → 改成 `default: "" (follow the status)`。采纳 3 建议：绑定名含控制字符时 `%q` 的 `\x..` / `\a` 等 TOML 读不回（T18 起就有，T50 后生成文件成推荐用法所以修）→ 改 `\u00XX`；golden 测试 Default().String() = internal/config/testdata/config.example.toml（specs 里的 config.example.toml 删除，模板以 testdata 为准）；flash 措辞、"any single character"。
- dev 6b683ca：border 注释改 follow the status；tomlQuote（\a \v \xNN → \u00XX）用于绑定名与值 + round-trip 用例；golden internal/config/testdata/config.example.toml + TestStringMatchesTheExample（UPDATE_GOLDEN=1 重写）；措辞两处。送复审；tester 目标改 6b683ca。
- reviewer 复审 6b683ca 通过：border 注释一致；tomlQuote 对 ESC / DEL / BEL / NUL / 中文 / emoji / U+2028 等全部 round-trip；golden 杀 noUse；措辞同步。

### T51 直接切到某个分组方式的键位：`group_cwd` / `group_project` / `group_session` / `group_state`（通过 @fb91e04 / 15 条；整套 855 条）
- 用户 2026-10-14：经常要直接看 state 分组，`g` 循环要按好几下；要能配置某个键直接切到指定分组，并给他配 `G` = state。
- 定稿：[keys] 新增四个动作 `group_cwd`、`group_project`、`group_session`、`group_state`，默认都不绑键（空列表 `[]` 表示不绑；Parse 对这四个动作允许空，其它动作仍要求至少一个键）。按键 = 切到该分组方式（和 `g` 切到同一方式时行为一致：清折叠、焦点留原卡、结束手动滚动；已在该方式则不动）。底栏不加文案（`g group:<mode>` 已显示当前方式）。`agmux config` 打印 `group_state = []  # default: none — switch straight to the state grouping`，四行跟在 group 后面；golden 同步。一个键仍只能绑一个动作。
- 用户配置：我在他的 ~/.config/agmux/config.toml 里加 `group_state = ["G"]`（装好后）。
- 验收：单测 四个动作各自切到对应方式、已在该方式时无变化、默认未绑不响应、`[]` 可解析、绑定后 KeyMap 包含、`agmux config` 四行与 golden；黑盒 配 `group_state = ["G"]` 后按 G 直接到 state 分组、再按 g 从 state 往下循环到 cwd。
- dev fb91e04：Actions 在 group 后加四个，默认空；Parse 只对这四个允许 `[]`；String() 默认无键的动作打印 `= []  # default: none — …`；do("group"|"group_<mode>") 共用路径，已在该方式早返回；README 键位表 + 示例 group_state = ["G"]；变异 4 杀。已有语义提醒：文件里绑的键会从默认持有者拿走（配 ["g"] 则 g 循环失键）。
- reviewer（fb91e04）通过无必改：`[]` 豁免只限四个、一键一动作仍生效、String() 两条路径不混、KeyMap 无空项、直切与 g 在四种状态下完全一致（直通 / flash / 搜索中两者都不切）；变异 7 杀。不做：头注释补「group_<mode> 可为 []」。

## T47 + T48 + T49 整批回归（tester 2026-10-14，fdac3c9，套件 e65a8c6）
- vet / race 全绿；整套 817 条 1865 s，16 条红全是 T49 预期变化（内置 10 → 20 套顺序、选择框 25 格要滚动），已按新语义改，受影响文件 300+ 条复跑全绿；改后整套未重跑，留给 T51 那轮做最终回归。
- T47 真 tmux 9 条（普通 pane enter 不退不 detach、焦点留原卡、q/esc/Q 退出、底栏随 popup 2.5 s 内变化、不在 tmux 提示）；T48 t45 重写（停 3 s / 路过 / popup 里停仍 done，预览 / 直通 / enter 立即，waiting 不记，statusChangedAt 变了回 done）；T49 11 条（20 套顺序、每套 7 态 × 2、用户同 id 替换位置不变、选择框滚动、281 帧全经 tty 且 <1024、126×84、hash 落到新套）；真 kitty 3 次 5796 个 APC 0 损坏；反向在旧版均红。
- 一次未复现：t18 选择框宽字符用例在连跑里红过一次（105 列「网格外一行变了」），单跑 11 次全绿，整套再跑留意。
- 没覆盖：真 kitty 渲染截图（另起实例截图全白，改为从字节流解帧看了 naruto / developer idle 正常）；真 kitty 读速；新套各态画面未逐帧看。
- tester（6b683ca）：默认输出与 golden 逐字一致；合法 TOML；9 个非默认值只改值行、default 注释不变；写回再打印两轮相同；绑定名含控制字符 / 中文 / emoji 读回无损且输出只有 \u00XX；坏配置仍退出非 0；11 条旧用例改用 lib.plain_config() 读值后全绿。没覆盖：整套（留 T51 轮）、覆盖用户已有文件的用法。决定：先装 6b683ca 给用户验收 T46–T50，T51 过后再装。
- 2026-10-14 12:08 已装 6b683ca（干净 clone 构建，不含 T51）；用户配置用新版重新生成（值不变、round-trip 一致）。
- tester（fb91e04，套件 e4189b5 + 1 修测试）：T47–T51 最终整套 855 条 2061 s，唯一 error 是 t51 新用例读到重绘截断帧（测试问题，expect() 遇截断帧重读后 t43 + t51 复跑绿；改后整套未再全跑）；t18 宽字符用例未复现。T51 15 条：G 直达 state、g 继续循环、折叠清 / 焦点留 / 滚动结束、已在该方式折叠保留、四键 12 组合、默认无反应、搜索框 / 直通里 G 是输入、`["g"]` 拿走 g、config 四行与 golden、`[]` 只对四个合法；反向 13 红。没覆盖：flash 里按 G、用户真实 config、真机 kitty 渲染截图。
- 2026-10-14 12:59 已装 fb91e04（干净 clone 构建）；用户配置重新生成并加 group_state = ["G"]（round-trip 一致）。T46–T51 待用户验收。

### T52 头像选择框高度随内容自适应（通过 @87a809e）
- 用户 2026-10-14：4×5 固定 4 行，25 套时第 5 行（像素）看不见，又没有滚动条，用户不知道还能滚（截图 images/23.png）。
- 定稿：选择框行数 = ceil(集数 / 5)，全部显示（25 套 → 5 行）；只有当卡片下方放不下全部行时才滚动，且滚动时在框底一行显示 `↓ n more`（上方被滚掉时首行显示 `↑ n more`），用 muted 色；不滚动时不显示。框的上边仍贴着卡片；卡片下方空间不够时允许框向上盖过卡片下面的其它卡（现状如何就保持）。列数仍 5、格宽 10 不变。
- 验收：单测 10 套 2 行、25 套 5 行、26 套 6 行；空间够时无 more 行；空间不够时 more 行文案与计数、滚到底后换成 ↑；黑盒 80×24 与 80×40 两种高度下 25 套的截图。

### T53 同组内卡片置顶（通过 @87a809e）
- 用户 2026-10-14：经常要把某个 session 放到分组前排；想要一键置顶，后置顶的排在先置顶的后面，再按一次取消回落默认排序，像 nvim bufferline 的 pinned。
- 定稿：新动作 `pin`，默认键 `t`（top）；按下切换焦点卡的置顶。置顶的卡在它所在的组里排最前，多个置顶按置顶时间先后排（先顶的在前）；取消后回到组内默认（名字序）。置顶按 session id 记，和分组方式无关（切到 project / session / state 分组仍在各自组内最前）；记录写 `$XDG_STATE_HOME/agmux/pins.json`（复用 seen.json 的 FileBindings 机制，会话消失时清掉）。卡片名字前加一个 `▲`（focus 色；焦点在卡上时随焦点色），表示置顶。搜索过滤、flash、折叠不受影响；`agmux show` 不改。`agmux config` 键位表加 `pin = ["t"]  # default: t — pin the focused card to the top of its group`。
- 验收：单测 置顶后组内最前、两张按先后、取消回落、切分组仍最前、重启仍在、会话消失条目清掉、文件坏了当空且底栏一行；黑盒 真 tmux 夹具 t 置顶 / 再 t 取消 / 两张顺序 / g 切分组后仍最前 / 重启看板仍置顶。
- dev aa0b9be：行数 = ceil(n/5)，gridPlace 先试全部行（卡下 → 卡上）放不下才减行；取舍（照准）：n 数被滚掉的头像数；滚动时首尾各留一行（无则空行）使框高不随光标跳；一行都放不下的退化场景保持原 5 行无 more；放置策略不变。测试 10/25/26 行数、h=20 下顶中底文案与计数、框高恒 10；变异 2 杀。
- dev 9a7a3bb：动作 pin 默认 t；Poller.Pins（pins.json，值 = 置顶时间）、TogglePin、消失清掉、文件坏了当空 + 底栏；groups() 置顶在前按时间再名字序；名字前 `▲ `（focus 色）、flash 命中偏 2 列；底栏 `pinned x` / `unpinned x`；变异 4 杀。定夺：按 session id 记会在 r 重启 / handoff 后掉 → 改按名字（AvatarKey）记，Prune = 无会话用该名字；底栏不加 t pin。
- reviewer（aa0b9be）：穷举探针 34390 个状态证明框高恒定 / 方向 / 计数守恒 / 光标可见；退回 3 必改：带提示要 7 行使 5–6 行空间时退化到屏顶盖住自己的卡（测试阈值 5→7 掩盖了回退）、最后一行不满的 ↓ 计数无测试、README 过时。定夺：采纳建议 1 把 more 写进上下边框线不占行（删 +2 / hints），矮屏也有提示；阈值回 5；采纳建议 2；居中测试缺口不补。design.md 选择框段已同步；e2e 过时清单转 tester。
- dev 1647154：more 进上下边框（muted），删 +2 / hints，框高 2+3*rows，一行放得下不再退到屏顶；阈值回 5、Scrolls 表恢复；RowsAndMore 表驱动（25 / 23 个、h=12 矮屏一行有提示），顺带修了最后一行不满时 ↓ 出负数的真 bug；README 改；探针 34390 例全过。本轮 T52+T53 目标 hash 1647154。
- reviewer（0f3c9d9）：四种分组 × 两种 flow 排序正确、t 在搜索 / 选择框 / 直通 / flash 里仍是输入、三个状态文件互不干扰、坏文件不覆盖。退回 2 必改：快速连按 t t 第二下再置顶（按键快照）→ TogglePin 在锁内 Reload 后看文件当前状态并返回新状态；flash 对截断名字不标 label、▲ focus 色两处无测试。spec 定夺：r 重启空档（ccmux SessionEnd 删 marker 后 removeSession，1.5–4 s 看不到会话）和 daemon 刚起空列表会让 Prune 清掉 pin → 按名字记连续缺席起始，超过 60 s 才清；无 summary 会话按 project 一起置顶照现状接受。采纳：unpinNoReload / onlyThisID 用例、注释。
- reviewer 复审 1647154：3 必改 + 建议 2 满足；新探针 77520 个状态全过（宽度 ≤ 屏宽、↑↓ 只在边框、计数守恒、光标可见、框高恒定、≥5 行空间不盖卡为 0）；鼠标不受影响。剩 1 条测试必改：muted 断言看整行被框外文字撑成恒真 → 只看框内 ansi.Cut 段并加无 note 用例。随 T53 修复一起提交。
- dev 87a809e（T52 muted 断言 + T53 全部退回）：TogglePin 改 (pinned, err) 锁内 Reload 按文件状态切换并返回；连按 / 跨看板用例；flash 截断名字、▲ 颜色用例；60 s 缺席宽限（Now 注入、pinGone、prunePins；[a1]→[]→[a2] 仍置顶，两次 50 s 不累加，>60 s 清）；无 summary 按 project 的 README 一句；同名一起用例；变异 7 杀。本轮 T52+T53 目标 87a809e。
- reviewer 复审 87a809e 通过：连按探针现为 unpinned；60 s 宽限在注入时钟下 10 s 空档仍置顶、永久消失 60 s 后清；pinGone 不重计时、不泄漏、多看板各自计时只会更晚清；Now 注入不漏到产品路径；变异 13 杀 12（存活 noCleanup 仅内存条目，不补）。
- tester（87a809e，套件 05bd73f）：整套 894 条跑一次 6 红全查清（t17/t50 键位表加 pin；t18/t30/t44 是用例或采样毛病），改后受影响 9 文件 211 条只剩 t30 一条重绘半截帧，再读后两遍绿（两处改动合起来未再整套跑）。T52 新 10 条：80×40 画 5 行无提示、25/26/30/31 套行数、80×24 边框 `↑/↓ n more` muted 且计数按屏上格子反推、16 种高度 × 卡位与 gridPlace 一致且不盖自己的卡、一行放得下贴卡、截图 scratchpad/shot52.txt；反向 fb91e04 8/10 红。T53 新 29 条：t 置顶 `▲ ` focus 色 + `pinned x`、多张按先后、`t t` 终态未置顶、四种分组仍最前、重启不变、改 id 模拟 r 仍置顶、空列表 / 缺席 4 s 不丢、30 s 在 60 s 后清（实测 55–85 s，单跑 70 s+）、同名一起、另一看板几秒同步、折叠 / 搜索 / flash 不受影响、坏文件当空不覆盖、超长中文名、config pin 行 / 改键 / 占键 / `[]` 报错；反向 25/29 红。没做：真 claude 会话按 r（tester 禁区，留用户验收）、两看板同时按 t 竞态、真 kitty 截图、真 tmux popup 选择框、Linux。观察（备忘）：无 AGMUX_CLIENT 时板子按 client_activity（秒级）挑 client，往 popup 敲键不刷新用户 client 的 activity，两个 client 同秒活跃可能挑错；真实使用 launcher 设 AGMUX_CLIENT，不致命。harness 修了 parse_screen 颜色跨行延续（tmux capture -e 颜色不变不重写 SGR）。
- 2026-10-14 17:42 已装 87a809e（干净 clone，vcs.modified=false）。T46–T53 待用户验收。备忘：`agmux --version` 不存在，只能 `go version -m ~/go/bin/agmux` 看；以后单开。

### T54 选择框里 `avatar` 键（默认 a）也关闭选择框（通过 @ad287e8 / 10 条）
- 用户 2026-10-15：头像选框里除了 esc 外，a 也能关，即 a 是开关键。
- 定稿：选择框打开时，按 `avatar` 动作绑定的键（默认 a）关闭选择框，等同 esc / quit：不改头像、不写文件、光标位置不保留（下次打开仍停在当前穿戴的那个）。其它键行为不变。README 键位一句 / `agmux config` 的 avatar 注释改成 "open or close the avatar picker"。
- 验收：单测 a 开再 a 关回到 browsing 且 SetAvatar 未被调用；改键（avatar = ["A"]）后 A 开 A 关、a 在选择框里无反应；黑盒 真 tmux 夹具 a a 框消失、绑定文件不变。

### T55 一键已读：动作 `mark_read`，默认键 `m`（通过 @ad287e8 / 26 条；整套 930 条）
- 用户 2026-10-15：除了现有的 tab / 预览打开 / enter 会变已读，加一个 m 直接把 done 变 idle，不用打开预览。
- 定稿：新动作 `mark_read`，默认键 `m`。焦点卡是 done 时按下，走和预览 / enter 完全相同的已读路径（MarkSeen，同一个 seen.json），卡立即变 idle，底栏一行 `read x`（muted）。焦点卡不是 done、或焦点在折叠的组标题上：无反应、无提示。搜索框 / 直通 / flash / 选择框里 m 仍是输入或无反应。底栏不加提示（同 pin）。`agmux config` 键位表加 `mark_read = ["m"]  # default: m — mark the focused card's done as seen (it turns idle)`，golden 同步；README 键位一句。
- 验收：单测 done 卡按 m → MarkSeen 被调一次、卡 Done=false、底栏 read x；非 done 按 m 无调用无提示；折叠标题无调用；改键 / `[]` 报错（普通动作，同 pin；只有 group_* 允许空）/ 占键报错；黑盒 真 tmux 夹具 done 卡按 m 立即 idle、重启看板仍 idle、状态再变后重新 done。
- dev dbc068f（T54）：pickAvatar 的 quit case 加 "avatar"；README / config 描述 "open or close"；TestAvatarKeyToggles 默认与 ["A"] 两遍、改键后 a 无反应；探针 1 杀。
- dev 7e22c45（T55）：Actions 加 mark_read 默认 m；case 先判 !s.Done 返回，再 say read x + markSeen；TestMarkReadKey / TestMarkReadRebound / config TestMarkReadKeys；探针 4 杀。定夺：`[]` 报错不禁用（同 pin）。本轮目标 7e22c45。
- reviewer（7e22c45）通过无必改：真 Poller 探针证实按 m 后 Done=false、seen.json 写入、重启仍 idle、状态变更后重新 done；m 在搜索 / 直通 / 选择框 / flash / kill 确认里都不触发；avatar 与 quit 不可能同键，avatar=["q"] 开关不退出；变异 12 杀 11。采纳建议 1 后半：markSeen 的 Cmd 返回 nil 不触发重绘，按 m 后卡要等下一帧（≤500 ms）才变 idle → mark_read 改成 Cmd 里 MarkSeen 后返回 doneMsg{ok: "read "+name}，底栏与 idle 同帧出现。采纳建议 2：断言 msg 颜色 muted。不做：ccmux 没给 statusChangedAt 的 done 卡按 m 仍写 read x 而不变 idle（用户 10 张 done 卡都带时间，备忘）。
- dev ad287e8（T55 建议）：mark_read 改 Cmd 里 MarkSeen 后返回 doneMsg{ok: "read "+name, err}；msg 颜色 muted 断言；新增 TestMarkReadFails（失败底栏红、不写 read x）；探针 1 杀。本轮 T54+T55 目标 ad287e8，送 tester。
- tester（ad287e8，套件 31d19d8）：整套 930 条 2428 s，1 error 是 t18 sweep 读到重绘截断（用例问题，改重读后 38 条三遍绿，改后未再整套跑）。T54 10 条：a 关框不写 avatars.json / 不改头像 / 不发命令、光标回到穿戴的、a a a、esc q 仍关其它键无反应、滚动态也关、改键 A / q、config 注释；反向 9 红。T55 26 条：卡变 idle 与底栏 read x 同一 capture（几十 ms）、muted、seen.json 同一条、只动焦点卡、二次 m 无反应、working / waiting / idle / 折叠无反应、重启仍 idle、statusChangedAt 变了重新 done、坏文件不覆盖不假报、只读目录报错、搜索 / flash / 选择框 / kill 确认 / 直通 / 真 tmux --stay 弹窗、config 行 / 改键 / [] / 占键 / pin=["m"]；反向 87a809e 14 红、7e22c45 仅「同一帧」1 红（证实建议 1 有效）。备忘：无 statusChangedAt 的 done 卡按 m 底栏写 read x 但不变 idle。没覆盖：真 kitty、Linux、用户真实 config、真 claude 会话。
- 2026-10-15 15:45 已装 ad287e8（干净 clone，vcs.modified=false）。T46–T55 待用户验收。

### T56 预览窗口长行自动换行（通过 @24a88f4 / 22 条）
- 用户 2026-10-15（截图 images/24.png）：预览里内容不换行，pane 比预览宽时行尾看不到。现状：previewLines 把每行 ansi.Truncate 到框内宽度，是 M2 的设计取舍，不是 bug，但要改。
- 定稿：预览框内每一行按框内宽度硬换行（x/ansi v0.10.1 的 Hardwrap，保留 pane 自己的颜色），显示的是换行后的最后 N 行；滚动（滚轮）按换行后的行数算；光标（T24）映射到换行后的行列：行 = 该逻辑行换行后的起始行 + col / inner，列 = col % inner；光标所在逻辑行以及之后的空行逻辑（trimTail / keep）照旧按逻辑行算再换行。宽字符不能被切成半个（Hardwrap 本身处理）。tab 仍先按 8 列展开再换行。不加配置项。
- 验收：单测 一行 2×inner+3 列的内容在框里占 3 行、尾部可见；颜色跨换行行延续不漏到边框；光标在第 2 段时反色落在第 2 行正确列；宽字符行尾不被切半；滚动按换行后的行数；黑盒 真 tmux 夹具 pane 150 列、预览 60 列，长行尾部文字在框里看得见；截图。

### T57 直通态把真光标放到预览里的光标格（输入法候选框跟着走）（通过 @24a88f4 / 17 条；整套 969 条）
- 用户 2026-10-15（截图 images/24.png）：输入法候选框固定出现在左下角，不跟预览里的输入焦点走；底栏开头还被输入法的预编辑文字（d'fys）盖住。根因（我读 bubbletea v1.3.10 standard_renderer.go flush 得出）：每帧画完后渲染器固定 `CursorPosition(0, len(newLines))` 把终端光标放到最后一行开头，kitty 的输入法候选框和预编辑文字跟终端光标走，所以落在底栏左下角；View 里塞 CUP 序列会被这句覆盖，v1 没有设光标位置的 API。
- 定稿：直通态（pass-through 有效，预览画着 pane 光标）时，把终端真光标放到预览里光标那一格，并显示（tea.ShowCursor）；离开直通、光标不可画（flag=0 / 滚动 / 超宽）、预览关闭时隐藏（tea.HideCursor）回现状。做法：给 tea.NewProgram 传 tea.WithOutput(w)，w 是嵌入 *os.File（stdout）的包装（必须仍满足 term.File：io.ReadWriter + Fd()，否则 bubbletea 不查窗口尺寸、不收 resize），Write 原样写出后若有待放位置就追加 `\x1b[row;colH`（1 起）；位置由 View 时算出（预览框内光标行列 → 屏幕绝对行列）存进包装（原子 / 锁），渲染器只在画面变化时 flush，光标变了画面也变（反色块在动），所以每次位置变化都会被写出。直通态有真光标时不再画反色块 / 下划线（真光标顶上），非直通态照旧画。实测要求：dev 在真 kitty + tmux（popup 和普通 pane 各一次）开中文输入法在直通里打字，候选框贴着预览里的光标；用 `tmux display-message -p -t <看板 pane> '#{cursor_x} #{cursor_y}'` 核对看板 pane 的光标就是预览里那一格。design.md（m2-preview 光标段）补一句。
- 验收：单测 直通态 View 后包装收到的位置 = 预览光标格的绝对行列（含卡片区宽度偏移、标题行、边框）；离开直通 → HideCursor 且不再追加；光标不可画 → 不追加；包装 Write 对无位置时字节原样；黑盒 真 tmux 夹具里看板 pane 的 cursor_x/y 等于预览格，tab 回卡片后 cursor_flag=0。
- dev 164ce6f（T56）：wrapRows 用 Hardwrap(preserveSpace) 逐逻辑行换行取尾 N 行，续行开头补当前 SGR、行末 reset 仍在；滚动 / pvLines 按换行后行数；光标按每行实际宽度累加定位（宽字符挤行也对），光标停在整行之后补一空行；删「cursor_x 超宽不画」旧用例；wrap_test.go；探针 6 杀。
- dev 451a2c9（T57）：cursor.go 的 cursorOut 嵌入 *os.File，Write 原样写后追加 CUP 一次发出；Update 包一层只在状态变化时发 Show/HideCursor；pvBox 算绝对坐标；WithOutput。偏差（dev 定）：直通态光标格不画反色块但放一个无效果的 `\x1b[27m` 标记，否则光标单独移动画面不变渲染器不 flush；副作用该格丢 pane 原色（真光标盖着）。真 kitty 0.49 + tmux 实测：普通 pane 看板 pane 的 cursor_x/y/flag = 预览格（abc → +3、你好 → +4、换行后换行也对、tab 回卡 flag=0）；popup 用 kitty @ get-text --add-cursor 核对一致；截图 scratchpad t57-*.png。没验：输入法候选框贴光标（要人开输入法，留用户验收）、ghostty、Linux、光标形状不跟 pane 的 DECSCUSR。风险：review（ExecProcess）返回后 bubbletea 重新隐藏光标，curOn 未同步（review 只能从卡片态触发）。本轮目标 451a2c9。
- reviewer（451a2c9）退回 2 必改：(1) T57 回归——cursorOut 不是 *os.File，bubbletea 的 exec 给 `d` review 的子进程填的 stdout 变成 pipe（真 hunk 退回 80 列上下排版）→ review() 里 `c.Stdout = os.Stdout` + 单测；(2) previewBox 末尾 `2+curRow >= h` 死代码删掉。采纳建议 1（框内宽度只从一处取，不再 previewW(m.w)-5 复制）和建议 2（注释说明 execute 走提升的 WriteString 不追加 CUP，退出不落 CUP 到主屏；探针收成测试）。spec 定夺：tmux capture -e 的 SGR 跨 pane 行延续（行末带色不 reset、下一行不重写）是 T24 起的老问题，并入 T56 修：activeSGR 状态跨逻辑行延续，且要从整段 capture 开头算起（不是只从显示的尾部算），否则被 trimTail 丢掉的行里开的颜色会丢。已核无问题：续行 SGR 逐格比对 11 种样式一致、光标定位、滚动 clamp、性能（3000 行 9 ms）、Write 合约、竞态最坏一帧、标记只影响一格、6 种显隐转换、popup 路径 kitty 回放光标 (134,9) 与 pane cursor_x 吻合；变异 22 杀 19。
- dev a74e6c1（T56/T57 退回 5 条）：review() 显式 c.Stdout = os.Stdout + TestReviewChildStdoutIsTheTerminal；死条件删；previewInner(w) 单一来源 + 91/92 列用例；cursorOut 注释 + TestQuitInPassThroughLeavesNoPosition（真 Program 直通后 Quit，退出 alt screen 后无 CUP）；SGR 从整段 capture 首行延续（TestPreviewWrapColorCarriesAcrossLines：行末 41m 续行红底、reset 清掉、被截掉的前 10 行里开的 44m 到首个显示行）；ponytail 限制：从不 reset 的几千行 capture 前缀会变长，无上限。没重跑真 kitty（光标计算未动）；真 hunk 未在真终端复验。本轮目标 a74e6c1，送复审。
- reviewer 复审 a74e6c1 通过无必改：bb 黑盒 review 的 stdout 回到 TTY；carry2 场景真 tmux 里预览与 pane 一致；ponytail 限制在用户 22 个真 claude pane 的 capture 上实测前缀最长 618 B、换行后总量不膨胀，接受；变异 25 全杀。备注采纳（老问题，ad287e8 也有）：行尾带底色未 reset 的行，预览补齐空格会把底色一直铺到边框 → box 行里 content 之后先 `\x1b[0m` 再 pad（每行开头重新带 sgr 前缀，不影响跨行延续），dev 补一个 commit。
- dev 24a88f4（T56 备注）：box 行 `pad(l+"\x1b[0m", inner)`，底色止于文字；TestPreviewWrapBackgroundStopsAtTheText。本轮 T56+T57 目标 24a88f4，送 tester。
- tester（24a88f4，套件 ee28934）：整套 969 条 2566 s 零红零重跑。T56 22 条：232 列框内 87 文字格、150 列长行占 2 行拼回原行、100 列看板 3 行、整宽不多空行、宽字符不切半、tab 展开、最后 N 行逐行比对、颜色跨行 / 未 reset 红底续行 / 底色止于文字 / 截掉行里的蓝底延续、光标第 2 段 +13 / 下划线 / 满行后下一行首 / 宽字符挤行 / 超宽改画在换行行、滚轮 ±3 行到顶不动；反向 ad287e8 16 红、164ce6f 2 红。T57 17 条：进直通即在格上、打字 / 宽字符 +4 / 换行 / 折行 +13 / 超宽跟走、块不画、Tab 回卡 / p 关 / 99 列 / session is gone / pane 隐藏 / 滚轮上滚 → flag 0、退出后无 CUP 末序列 ?25h（q / Q）、d 起的 review 子进程 stdin / stdout 都是 TTY 且往返后直通仍工作；反向 451a2c9 2 红、ad287e8 14 红。t24 两条旧用例改。备忘：滚轮一格 = 3 个换行后的行（board 一直 ±3，不是需求变更）；滚动后窗口末行少一行（光标空行不显示）。没覆盖：输入法候选框、真 kitty 渲染、popup 路径光标（dev 自测过）、Linux、ghostty、DECSCUSR、真 hunk 排版。
- 2026-10-15 18:29 已装 24a88f4（干净 clone，vcs.modified=false）。T46–T57 待用户验收。

## 第十一批 后台会话（agent-builder 2026-10-16 转达，称用户已确认；用户可否决）
背景：编排者以后用 `claude --bg --agent <角色> -n <项目>-<角色> --model <模型> "<任务>"` 起后台会话（无 tmux pane），看板上会越来越多。实测（ccmux 1.4.3、Claude Code 2.1.300）：`ccmux show --json` 里 `trackingMode:"background"`、`tmuxPane:null`、`summary:null`；`claude agents --json` 有 `kind:"background"` 和 `name`；名字写在会话 jsonl 里（`{"type":"custom-title","customTitle":"bg-probe2"}`，重复写了几次）；`claude attach <id|name>` 把后台会话接到当前终端（← 回 agent 视图，Ctrl+Z 回 shell，会话照跑）；`claude logs / stop / rm <id>`。

### T58 后台会话的卡片名用 `-n` 给的名字（通过 @3f8d72b / 17 条）
- 现状：store.go `Name: r.Summary`，空就退回 Project，后台会话显示成项目名。
- 定稿：Name = Summary；Summary 空时取 jsonl 里最后一条 `custom-title` 的 customTitle；再空才退回 Project。读法：先在已经读的末尾 64KB tail 里找；找不到就整文件扫一次（流式、取最后一条），按 session id 缓存结果（名字不会变，每个会话最多整扫一次）；没有 logPath 或文件读不到就退回 Project。不额外起 `claude agents` 进程。AvatarKey / pin / seen 的名字口径随 Name 一起变（后台会话有了真名就按真名记）。`agmux show` 的名字列同步。
- 验收：单测 tail 有 custom-title → 用它；tail 没有、文件前面有 → 整扫一次拿到且第二次 poll 不再读整文件（计数）；都没有 → Project；summary 有 → 不看 jsonl；黑盒 假 ccmux 返回 summary null + 假 jsonl → 卡片名是 -n 的名字。

### T59 卡片标明后台会话（通过 @3f8d72b / 15 条）
- 定稿：Session 加 `Background bool`（trackingMode == "background"）。卡片第二行右端（分支那行的右边空位）加 muted 的 `bg` 标签；预览标题里 pane id 的位置写 `bg`（有了 T60 的 attach pane 后标题仍写 `bg · %id`）。没有 pane 的消息行文案改为 `background session · enter or i attaches it`（T60 之前先改文案，键仍灰）。`agmux show` 加一列 `bg`/空。
- 验收：单测 后台卡有 `bg` 标签、普通卡没有；预览标题；show 列；黑盒 假 ccmux trackingMode background → 卡片与标题可见 `bg`。

### T60 在预览里和后台会话交互：按需 attach 到一个 tmux pane（通过 @3f8d72b / 35 条；整套 1036 条）
- 用户要的：不另开终端，在 agmux 预览窗口里就能和后台会话对话。
- 定稿（最少新代码：让后台会话也有 pane，T24 / T57 的预览、直通、光标、跳转全部复用）：
  - 后台卡没有 pane 时，`enter`（jump）/ `i`（reply）/ `tab`（focus_preview）先给它起一个 pane：在专用 tmux session `agmux-bg` 里开窗口 `new-window -d -t agmux-bg: -n <卡片名> -c <cwd> -P -F '#{pane_id}' -- claude attach <id>`（session 不存在就 `new-session -d -s agmux-bg … -- claude attach <id>`），然后照常：enter 切过去、i / tab 开预览进直通。`p`（预览开关）不 attach：焦点扫过后台卡时预览区显示 `background session · enter or i attaches it`。
  - pane 的发现不存状态文件：每次 poll 跑 `tmux list-panes -a -F '#{pane_id}\t#{pane_start_command}'`，start command 匹配 `claude attach <id>` 的 pane 就是它的 TmuxPane（看板重启、多块看板都自然一致；attach 退出窗口关掉，卡片回到无 pane，再按再起）。放在 store 里跑（execx），tmux 不可用（不在 tmux / 命令失败）时静默当没有。
  - 同一会话已有 pane 时不重复起；两块看板同时按的竞态接受（最多多开一个窗口，备忘）。
  - `x`（kill）/ `r`（restart）对后台会话仍是现状（ccmux kill / restart 对 bg 是否有效不在本批；备忘：以后可能要走 `claude stop / rm`）。
  - 不依赖 `claude attach` 的退出键；用户在直通里按 ← 回 agent 视图、Ctrl+Z 退出 attach 都是 claude 自己的行为，窗口随进程关。
- 验收：单测 后台卡 enter / i / tab 在无 pane 时先发 new-window（参数含 -n 名字、-c cwd、claude attach id），有 pane 时不发；p 不发；list-panes 输出解析（多 pane、带制表符、无匹配）；store 把匹配的 pane 填进 TmuxPane 且 Background 仍为 true。黑盒 真 tmux：假 ccmux 给一个 background 会话 + 假 `claude` 脚本（attach 子命令打印并 cat），按 i → agmux-bg session 出现窗口、预览显示其输出、直通打字到达；退出 → 卡片回无 pane。dev 真机：用 `claude --bg -n agmux-bg-probe --model haiku "只回复 OK"` 起一个一次性后台会话，在看板里 i 进去对话一次，完后 `claude stop` + `claude rm`；不动用户的 25 个会话。
- dev 8e69c38（T58）：store/title.go，tail → 缓存 → 整文件流式扫；缓存记已扫偏移（只算完整行），没标题的会话后续只读新增部分（偏差，dev 定，照准：刚起的会话标题可能还没写，整扫一次就永久缓存会一直显示项目名）；探针 6 杀。
- dev fbffe54（T59）：Background；卡片第二行右端 `bg`（有 agent 时 `<agent> bg`，分支让位）；预览标题 `名 · bg [· %id] · 状态`；文案 `background session · enter or i attaches it`，非后台无 pane 改 `no tmux pane: can't jump or send`；show 第 8 列；探针 5 杀。
- dev aa662cc（T60）：Poller.Panes（只在有无 pane 的后台会话时跑 list-panes，失败静默，匹配容忍引号 / 路径，多个取第一个）；board enter / i / tab 先 has-session → new-window / new-session 再做原动作；p 不 attach；attach.go 的 attachedSrc 把刚开的 pane 叠在 store 结果上直到 store 列出或 10 s（dev 定，照准）；attach 后卡没拿到 pane 不重做原动作（防栈溢出）。真机：一次性 bg 会话（haiku）名字 agmux-bg-probe、i 进直通对话收到 PONG、Ctrl+Z 窗口关卡片回无 pane、enter 重建并跳过去 253x65；已 stop + rm。定夺 dev 的 4 点：(1) 窗口 80x24 让预览里分隔线折行 → new-session 加 `-x <预览框内宽> -y <预览行数>`（只影响无 client 时的初始尺寸，不是 manual，client 接上仍跟 client），new-window 继承 session 尺寸即可，不用 resize-window；(2) list-panes 格式加 `#{session_name}`，attach 后 TmuxSession = agmux-bg，按 session 分组进 agmux-bg 组，tester 的假 tmux 随之更新；(3) H（handoff）在后台无 pane 卡上也走先 attach 再发，和 enter / i / tab 同一套；(4) 10 s 叠加层照准。
- dev dba7fed（T60 四点）：new-session `-x previewInner -y previewLinesN`（真机 200 列看板窗口 75x44，分隔线不折行，enter 后跟 client 253x65）；list-panes 加 session_name，attach 的卡 TmuxSession = agmux-bg（叠加层也填）；H 先 attach 再发，发前 capture-pane 轮询等窗口有内容（上限 5 s、100 ms 一次、超时仍发；实测 0.2 s 即有），i / tab / enter 不等；README 两句。本轮第十一批目标 dba7fed，送审。
- reviewer（dba7fed）退回 4 必改：(1) T58 改名后退回旧名——tail 命中不更新缓存、有缓存就不再看新增 → 改成有缓存也从 scanned 增量扫新增部分、新标题覆盖；(2) T58 补两个用例：文件变短 scanned 归零、整扫取最后一条；(3) T59 line2Right 的 agent 先截到 w 再拼 " bg" 超宽 → Background 时 agent 截到 w-3（不足只留 bg）+ 超长 agent 用例；(4) README:22 show 列说明补 bg。采纳建议 1（titleState 记 path，不同从 0 扫）、2（bg muted 断言）、3（`/` 下焦点在后台卡的 footer 用例）、4（attached=nil 的 Model 测不再发 attach，锁住防递归）、6（list-panes 加 `#{pane_dead}`，死 pane 不认领）。备忘（建议 5）：两块看板同时按，B 的卡会换成 A 的 pane 而 pvPane 仍是 B 的，B 直通掉一次要按 tab，B 开的窗口成孤儿需手关。备忘：不在 tmux 时 new-session -d 会顺带起默认 server。已核：T60 黑盒 i → agmux-bg 窗口 87×44、标题 `proj · bg · %2`、打字到达、叠加层过期不闪、id 整词匹配、名字 / cwd 带引号空格走 argv、kill-window 后回无 pane、用户手工 new-window 'claude attach id' 的 pane 会被认领（好事）、H 轮询在 Cmd 里、has-session 精确；56 MB jsonl 整扫 59 ms；用户当前 0 个后台会话、置顶 / 头像不受影响；show 第 8 列无外部调用方；变异 38 杀 31。
- dev 3f8d72b（第十一批退回 9 条）：title() 有缓存也增量扫、tail 命中写回缓存并把 scanned 设为当前大小（取舍：读 tail 与 stat 之间的几微秒新字节跳过，tail 每次都读不会丢改名）；TestTitleRename（A→B→滚出仍 B→盖过 tail 得 C）、TestTitleOtherFile（变短清空、换无标题文件退回项目名、整扫取最后一条）；path 变了从 0 扫；line2Right Background 时 agent 截到 w-3 + 兜底 Truncate，8 种宽度用例且 muted；README 列说明；过滤分支 footer 用例；attached=nil 防递归用例；list-panes 加 pane_dead（四列 `%id\tsession\tdead\tcmd`，三列旧行忽略）；探针 9 杀。本轮目标 3f8d72b，送复审。
- reviewer 复审 3f8d72b 通过无必改：改名滚出 tail 仍是新名；两个缺口补上；8 种宽度 bg 完整；README 对齐；5 建议都采纳并各有变异杀；真 tmux 复验 remain-on-exit 下 dead pane 3 s 后不认领、bb/t60.sh 全部一致。dev 的取舍接受（要同时满足改名落在 tail 与 Stat 之间的微秒窗口、且 2 s 内再写 64KB 才会漏；可选：首次 tail 命中把 scanned 设成 size-64KB，不阻塞）。变异 44 杀 41（存活 statErrEmpty / noFinalTrunc / noTypeCheck 均可不补）。第十一批目标 3f8d72b，送 tester。
- tester（3f8d72b，套件 38f92a2）：整套 1036 条 2997 s，1 红是 t10 150 ms 用例被 poll 的 list-panes 插进 send-keys 之间（用例问题，tmux_calls 默认过滤 poll 的 list-panes），修后 t10 三遍、新 67 条两遍绿，整套未再跑。T58 17 条（tail / 整扫 / 多条取最后 / 无标题 / 读不到 / summary 优先 / 别的行 / 空标题 / 断行 / 中文引号 / 20 MB / 改名 / 滚出不倒退 / 半行 / 晚出现 / 变短 / 换 path / 两会话 / show / pins 按标题）。T59 15 条（bg 位置 / agent bg / 让位 / 超长 agent 4 宽度 / 标题两式 / 两种无 pane 文案 / show 第 8 列）。T60 35 条：Shape 层假 tmux 记 argv（四键先 attach、-x/-y 取屏上量值、H 等内容 5 s、p 不建、不重复、引号空格、两卡两窗、叠加层 10 s、解析 8 种、按需跑 list-panes、失败路径、non-tmux）+ Real 层私有 tmux 假 claude（窗口尺寸 / 名 / -c / 预览 / 直通 / H 等 1.5 s 后 ^M 到达 / enter 切 client / kill / 退出 / 死 pane 回无 pane 再重建 / 重启认领 / 手工窗口认领 / 前缀 id）。反向 24a88f4 67 红、dba7fed 各修复点红。旧用例 t4 / t10 / t11 / t12 / t37 改。tester 失误已记：早期几个手工窗口 PATH 走到真 claude，只报错退出未动用户会话，已改走假脚本绝对路径。没覆盖：真 bg 会话、两看板竞态、非 tmux 真环境、真 kitty、Linux、claude 退出键。
- 2026-10-16 13:38 已装 3f8d72b（干净 clone，vcs.modified=false）。T46–T60 待用户验收。

### T61 预览不再换行（回到截断），预览宽度可配 `[layout] preview_width`（通过 @9b178a0 / 14 条 + t56 重写 21 条；整套 1048 条）
- 用户 2026-10-16（截图 images/27.png、28.png）：T56 的硬换行太生硬，Claude Code 的表格、输入框整个乱掉；「不好实现的话按原来的来，把预览框加宽点」。判断：pane 是 TUI，按框宽换行必然破坏盒线和对齐，判别「哪些行能换」不可靠 → 回到截断，用宽度解决。实测（只读 list-panes）用户的 claude pane 宽度多为 61–71，另有 117 / 130；253 列终端 40% 预览框内 96 列，比大多数 pane 宽，只有 117 / 130 的 pane 会被截。
- 定稿：
  - 预览内容回到 T56 之前的截断（每行 ansi.Truncate 到框内宽度，不换行；光标 cursor_x 超出框宽不画、不定位真光标（T57 的 HideCursor 路径））。保留 T56 一起修的两条：SGR 跨 pane 行延续（从整段 capture 首行算）、行尾底色止于文字不铺到边框。滚动回到按 capture 行数。previewInner 单一来源保留。
  - 新配置 `[layout] preview_width = 40`（整数，百分比，范围 30–70，默认 40 = 现状），预览宽 = max(60, w*pct/100)；卡片区按剩余宽度照旧算列数。`agmux config` 模板加一行 `preview_width = 40  # default: 40 — the preview's share of the terminal width, in percent (30–70; never under 60 columns)`，golden 同步；越界报错 `layout.preview_width: must be 30–70`。previewMinTerm 100 不变。
  - README / design.md（m2-preview 预览段「40%」处）同步。
- 验收：单测 150 列长行在框里截断且无第二行；SGR 跨行与底色止于文字的既有用例仍过；cursor_x 超宽不画不定位；preview_width 50 时 previewW(253)=126、30 / 70 边界、29 / 71 报错、未写时 40；golden。黑盒 真 tmux pane 150 列的表格（盒线）在预览里左侧完整不乱；config 行；改 55 后卡片列数变化。
- 用户配置：我在装好后把 `~/.config/agmux/config.toml` 的 preview_width 设为 55（253 列下框内约 134 列，用户所有 claude pane 都放得下；卡片区 114 列约 2 列），用户可改。
- dev 3ebcdef（T61）：wrapRows → carrySGR 逐行 Truncate；滚动 / pvLines 回按 capture 行数；col ≥ 框宽不画、pvBox -1 不定位；保留 SGR 跨行 + 底色止于文字；wrap_test 删、cut_test 新（150 列截断、宽字符边缘丢、滚动按行、91/92）；config PreviewWidth 30–70 默认 40，模板行 + golden，previewPct 走 useConfig；README / m2 design.md 同步（T56 段改写为曾换行 T61 改回）；探针 6 杀。本轮目标 3ebcdef。
- reviewer（3ebcdef）通过无必改：回退干净；超宽光标 HideCursor 不追加 CUP；carrySGR 整行先算再截，7 种样式逐格一致、截掉的右半段开的色延续到下一行；preview_width 路径同 maxCols、30/40/55/70 × 6 种宽度无越界、校验文案与 golden 对；真 tmux 150 列盒线表格在 40% / 55% 下左侧逐行一致；变异 13 杀 12（存活等价）。采纳建议 1（文案统一成 `71 is out of range (30 to 70 percent)` 回显值）和 2（README preview_width 行补「终端窄时卡片可能窄于 40 列」），dev 补一个 commit 后直接送 tester。
- dev 9b178a0（T61 建议）：越界文案 `layout.preview_width: 71 is out of range (30 to 70 percent)`；README 半句。本轮目标 9b178a0，送 tester。
- tester（9b178a0，套件 74533ad）：整套 1048 条 3021 s，3 红是 config [layout] 行序变化（max_columns → preview_width → flow）的旧断言，改后 90 条复跑绿。t61 14 条（253 列 40/55/30/70 → 101/139/75/177 与卡片 3/2/4/1 列、100 列 30%/40% 下限 60、120 列 70% 卡片 36 列不崩、框内 = 预览宽−5、<100 列仍拒绝、150 列盒线表格 40%/55% 左侧一致、config 行 / 回读 / 30 70 合法 / 71 29 0 100 −5 两路径文案 / 类型错）；t56 重写截断口径 21 条（左 87 格无第二行、整宽、宽字符边缘丢、tab、最后 N 行、颜色 7 种含截掉右半段开色延续、光标 86 画 87 不画、滚轮 3 行）；t57 改超宽不定位；t24 回不画。反向 3f8d72b 34 红。没覆盖：真 kitty、popup 路径、Linux、preview_width 与 --stay popup 宽度交互。
- 2026-10-17 14:01 已装 9b178a0（干净 clone，vcs.modified=false）；用户配置 preview_width = 55（round-trip 一致）。T46–T61 待用户验收。

## 第十二批 分发：GitHub Release + brew tap + npm（用户 2026-10-17）
- 用户要的：agmux 像 sqlmux 一样用 npm 和 brew 装；token 复用 sqlmux 的。
- 已查（只读）：agmux 本地仓库没有 remote，GitHub 上没有 kackerx/agmux；sqlmux 的链路在 ~/proj/sqlmux（`.goreleaser.yaml`、`.github/workflows/{ci,release}.yml`、`.github/scripts/tap-formula.sh`、`npm/build.mjs` + `npm/sqlmux/`），tap 是 `kackerx/homebrew-tap`（公开，现只有 Formula/sqlmux.rb），两个 secret `NPM_TOKEN` / `TAP_GITHUB_TOKEN` 是 sqlmux 仓库级的，值读不出来，要用户往 agmux 仓库再加一次；npm 裸名 `agmux` 已被别人占（0.0.7，另一个项目），壳包用 `@kackerx/agmux`，平台包 `@kackerx/agmux-{darwin,linux}-{arm64,x64}`。sqlmux-orchestrator 的坑：一次 push 别超过 3 个 tag（不触发 workflow）；源码 tar.gz 没 .git，commit/date 由 release.yml 算好传给 formula；`npm publish ./out/x` 要带 `./`；npm latest 约 2 min 后才变；不做签名 / notarize。
- 阻塞在用户的（见给用户的问题）：建公开仓库 kackerx/agmux（内置的 5 套火影同人头像随之公开）；往新仓库加两个 secret；第一个 tag。

### T62 `agmux --version`
- 定稿：`agmux --version` / `agmux version` 打印 `agmux v0.1.0 (3f2a1b9, 2026-10-17T06:01:34Z)`。main.go 加 `version/commit/date` 三个变量（默认 dev/none/unknown）供 ldflags -X；没注入时退回 `debug.ReadBuildInfo()`：Main.Version（go install @latest 得到 v0.1.0 / (devel)）+ vcs.revision 前 7 位 + vcs.time。usage 第一行补 `agmux [--version] <command>`。照 sqlmux cmd/sqlmux/main.go 的 versionLine 写，别多。
- 验收：单测 注入三值 → 原样；未注入 + BuildInfo 有 vcs → 取 vcs；都没有 → `agmux dev (none, unknown)`；`agmux --version` 退出码 0 且只有这一行。

### T63 发布流水线（照抄 sqlmux，改名字）
- 定稿：
  - `.goreleaser.yaml`：project_name agmux，`main: .`，binary agmux，darwin/linux × amd64/arm64，CGO 关，-trimpath，ldflags 注 main.version/commit/date（版本带 v），archives tar.gz `agmux_{{.Version}}_{{.Os}}_{{.Arch}}`，checksums.txt，changelog 排掉 `^e2e:` / `^m3:` / `^specs:` / `^docs:` / `^Merge `（我们的 spec 提交前缀是里程碑名，如 `m3:`，都排）。不要 brews 段。
  - `.github/workflows/ci.yml`：push main / PR / workflow_call；go vet、go test ./...（test/e2e 是 Python + 真 tmux，不进 CI）、gofmt -l 为空、go mod tidy 无 diff。不加 golangci-lint（项目没在用）。
  - `.github/workflows/release.yml`：`v*` tag → ci → goreleaser（GITHUB_TOKEN）→ tap job（checkout kackerx/homebrew-tap 用 `secrets.TAP_GITHUB_TOKEN`，`.github/scripts/tap-formula.sh` 生成 Formula/agmux.rb：desc 一句、homepage、源码 tar.gz 从源编 `depends_on "go" => :build`，`system "go", "build", *std_go_args(ldflags:), "."`，test 断言 `agmux v#{version}`）→ npm job（gh release download → `node npm/build.mjs` → 四个平台包再壳包，`NODE_AUTH_TOKEN: secrets.NPM_TOKEN`，发前跑 `out/agmux-linux-x64/bin/agmux --version | grep -F "agmux $TAG "`）。
  - `npm/build.mjs` + `npm/agmux/{package.json,bin/agmux.js}`：壳包名 `@kackerx/agmux`，bin 名 `agmux`，optionalDependencies 四个 `@kackerx/agmux-<os>-<cpu>`，require.resolve 找二进制 spawn，找不到的提示里 `go install github.com/kackerx/agmux@latest`。
  - README 安装段改成三种：`brew install kackerx/tap/agmux`、`npm i -g @kackerx/agmux`、`go install github.com/kackerx/agmux@latest`；加一句二进制未签名。
  - 不做：trusted publishing / OIDC（先沿用 NPM_TOKEN，和 sqlmux 一致；以后再说）、签名 / notarize、Windows、GoReleaser 的 brews / casks。
- 验收（本地能做的；GitHub 上的要等仓库建好第一个 tag）：`go build -trimpath -ldflags "-X main.version=v0.0.0-test -X main.commit=abc1234 -X main.date=2026-10-17T00:00:00Z" -o /tmp/x .` 后 `--version` 一致；用这个二进制手工打 4 个假 tar.gz（同名规则，可用同一个二进制）喂 `node npm/build.mjs v0.0.0-test <dir> <out>`，`out/agmux/bin/agmux.js --version` 走到平台包；`npm pack --dry-run ./out/agmux` 列出只有 bin + package.json；`sh .github/scripts/tap-formula.sh v0.0.0-test <sha> abc1234 <date> | ruby -c`（或至少肉眼）；`actionlint` 若装了就跑；goreleaser 未装，dev 可 `go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=publish` 验一次（能跑就跑，跑不了写明）。
- dev 883951a（T62）：version/commit/date + versionLine，退回 BuildInfo（Main.Version / vcs.revision 7 位 / vcs.time）；`agmux version` 也行；TestVersionLine 7 例；ldflags 二进制 `agmux v0.0.0-test (abc1234, …)` 一行退出 0。
- dev d75e7e3（T63）：goreleaser（无 ignore_tags / Features 分组，agmux 没有 m tag）、ci.yml（vet / test / gofmt / tidy）、release.yml、tap-formula.sh（desc "Card board for the coding agents in tmux, in a popup"）、npm/build.mjs + npm/agmux、README 三种安装、.gitignore /dist/。本地：假 tar.gz → build.mjs 五包版本一致、壳包 --version 走到平台包、缺平台包退出 1 带 go install 提示；npm pack 只有 bin + package.json；formula ruby 4 Syntax OK（系统 ruby 2.6 不认 `ldflags:` 简写，brew 的 ruby 够）；goreleaser --snapshot 跑通出 4 包（GOPATH 要换到可写处）；actionlint 没装。说明：仓库没有 LICENSE，license 字段没写。
- 我定（可否决）：MIT，和 sqlmux 一致，版权行 `Copyright (c) 2026 kackerx`；dev 补 LICENSE + formula `license "MIT"` + 两个 package.json `"license": "MIT"`。第十二批目标 = 这个 commit，送 reviewer。
- dev 26d7836（LICENSE）：MIT；formula `license "MIT"`、壳包 + 平台包 license（build.mjs 补回 `license: shell.license`）。第十二批目标 26d7836，送 reviewer（883951a / d75e7e3 / 26d7836）。
- reviewer（26d7836）退回 2 必改：(1) changelog 只排 `^m3:`，仓库还有 m1 / m2 的 spec 提交，首发无上一 tag 会列全部 205 条 → `^m\d+:`；(2) T62 CLI 路由无测试（去掉 version / --version / 不读 BuildInfo 三个变异全活）→ 抽 `run(args, stdout) int` 或子进程重入，两个词各测退出 0 / 恰一行 / 以 "agmux " 开头。采纳建议 1（再排 `tasks:` `memory:` `test:` `design:` `orchestrator memory:` 等非用户可见前缀）、2（build.mjs 给壳包和平台包各 cpSync LICENSE，MIT 要附副本；sqlmux 同漏，已告知）、3（formula ldflags 加 `-s -w`，注释说 std_go_args 加 -s -w 不对，Cellar 二进制 11.3 MB vs 8.4 MB）、4（补 `git: ignore_tags: ["m*"]`）。备忘（建议 5）：预发布 tag 发 npm 没带 `--tag next` 会成 latest。已核：CI 在无 tmux / kitty 的 Linux 容器非 root 下 11 包 386 例全绿；名字替换干净；actionlint 0 问题；goreleaser snapshot、build.mjs、npm pack、brew style / audit --strict / install --build-from-source / test 全过。
- reviewer 提的发布前用户决定（代码之外）：466 个提交作者邮箱全是公司邮箱（sqlmux 用的是 kackerx 的 noreply + `github.com-personal` 别名）；`.claude/agent-memory/orchestrator/` 7 个文件被跟踪（user_env.md 有公司机路径）；testdata/show.json、board_test.go、view.go、m1 design.md、e2e 里有公司项目名 / 真实路径（shopcart、rename-flag、ProjectsFolder）。见给用户的问题。
- dev f9e2f17（第十二批退回）：changelog exclude `^m\d+:` + specs / tasks / memory / orchestrator memory / design / test / e2e / docs / Merge（内部前缀全排，剩 118 条用户可见），`git: ignore_tags: ["m*"]`；main 抽 `run(args, stdout, stderr) int`，`readBuildInfo` 可替换，TestVersionCommand 两词各测退出 0 / 恰一行 / stderr 空，`--version x` 退出 2，四变异全杀；build.mjs 各包 cpSync LICENSE（npm pack 3 文件）；formula `-s -w` + 删错注释。本轮目标 f9e2f17，送复审。
- reviewer 复审 f9e2f17 通过无必改：T62 11 变异全杀，run() 各分支退出码 / 输出不变；真跑 goreleaser 的 CHANGELOG 469 → 119 条；npm pack 各包 3 文件、壳包走到平台包；formula ruby -c OK。新建议：(1) 漏网 2 条（`m2/m3:` 与无冒号的 `docs 改名…`）→ 正则 `^m\d+[:/]` + `^docs `，下个 commit 顺手改；(2) 首版 changelog 119 条偏内部 → 我定（可否决）：首版照自动生成，发完用户在 GitHub Release 页手改一段摘要，不加 --release-notes 机关。第十二批目标 f9e2f17，送 tester。
- tester（f9e2f17，套件 07e5045）：整套 1084 条 3071 s 零红（t62 13 条、t63 23 条）。run() 抽出前后 22 条命令逐字节对比只差 usage 的 `[--version]`；go install 出伪版本 + 7 位 commit（注意 git worktree 里拿不到 vcs，要 clone）；goreleaser v2.18.3 snapshot 4 包 + checksums、架构对、归档含 LICENSE；build.mjs 五包 3 文件、壳包透传参数 / stdin / 退出码 / 信号；formula 用 brew portable ruby 4.0.7 Syntax OK（`/opt/homebrew/opt/ruby` 不存在）。没覆盖：GitHub Actions、真 npm publish / npm i -g、Linux 真跑、签名。**第十二批通过，目标 f9e2f17；推送前等用户定六件**（邮箱改写、公司项目名、memory 去跟踪、火影、secret、建仓库）。
- 2026-10-17 18:46 已装 f9e2f17（干净 clone，vcs.modified=false）；`agmux --version` 在无 tag 的本地装出伪版本 `v0.0.0-20261017094136-f9e2f175122e (f9e2f17, …)`，预期。dev 24ad6af：changelog 正则 `^m\d+[:/]` + `^docs `（不送审，纯配置）。主干现在 = 24ad6af，推送前等用户六件决定。
- 2026-10-17 用户拍板：提交身份全部改成 kackerx、用 kackerx 建远程仓库推送；记忆文件去跟踪；火影留着；npm 新 token 由用户提供（已设为仓库 secret）；brew 的 TAP_GITHUB_TOKEN 用户自己找。公司名的来源：testdata/show.json 是 M1 时从用户机器上真 `ccmux show --json` 抓的夹具，测试和 M1 设计稿里的名字都是从它来的。
- 历史已用 git filter-repo 重写（备份 mirror 在 orchestrator scratchpad rewrite/agmux-before-rewrite.git）：作者全部 kackerx noreply；`.claude/agent-memory/` 整段从历史删除并 .gitignore；同长度替换掉公司机用户名、IDE 项目目录名、两个项目名、一个分支名（对照表只在 orchestrator 本地记忆文件里，不进仓库）；提交消息里的短 hash 已被 filter-repo 自动映射。重写后 475 → 460 个提交（纯记忆文件提交消失）。
- 2026-10-17 20:00 GitHub 上首次 ci.yml（run 39030657720，fe0a681）绿：test job 成功。警告两条：actions/checkout@v4、setup-go@v5 还是 Node 20（runner 已强制 Node 24，先能跑）；ubuntu-latest 10-19 起迁 Ubuntu 26。让 dev 把三个 action 升到当前大版本。
- dev b2416bc：checkout / setup-go / setup-node / goreleaser-action 升 v7（Node 24）。已推，CI run 39030789651 绿，无 Node 弃用警告。打 v0.1.0 前只差 tester 整套 + 用户 TAP_GITHUB_TOKEN。
- 注意：本文件在「历史已用 git filter-repo 重写」那行以上记的 commit hash 都是重写前的旧 hash，对照查备份 mirror。2026-10-17 20:20 两个 secret 齐（TAP_GITHUB_TOKEN 的范围比需要的宽：能 push sqlmux、admin tap，用户可自行收窄），公开历史最终扫描无公司信息，用户拍板直接发：打 v0.1.0。
- v0.1.0 release run 39031950151：ci ✓、release ✓（4 个 tar.gz + checksums）、tap ✓（Formula/agmux.rb 已推到 kackerx/homebrew-tap）、**npm ✗**：第一个平台包就 403「Two-factor authentication or granular access token with bypass 2fa enabled is required to publish」——用户新开的 granular token 没勾「Bypass two-factor authentication」。npm 上什么都没发出去。修法：用户重开 token（Packages and scopes = Read and write、范围要能建新包（All packages）、勾 Bypass 2FA）→ 我更新 secret → `gh run rerun --failed` 只重跑 npm job。
- tester 重写后整套（c9366a5 内容，套件 15aa6b0）：1084 条 3075 s，2 红均非替换所致（t43 老竞态 helper 漏接 AttributeError；t63 断言跟 changelog 新正则），修后 57 条复跑绿；替换涉及的分组 / 路径缩写 / 宽度用例全绿。tester 已清掉主仓库里全部旧 worktree。npm 发布：两个 granular token 都 403（npm 不认 bypass 2FA，已知问题 npm/cli#9268），改用户本地 `npm login` 后跑脚本手发五包（包已用 Release 真归档打好在 orchestrator scratchpad npm-v0.1.0/），之后排 Trusted Publishing。

## 第十三批 后台会话归到开它的会话组（用户 2026-10-20，截图 images/31.png）
- 现象（现状即如此，是 T60 决定 (2) 的后果）：按 tmux 会话分组时，后台会话 attach 前没有 pane → 进「(no pane)」组；attach 后 pane 在 agmux-bg 里 → 进 agmux-bg 组。用户要的是跟开它的那个会话（blessmuse）同组。
- 已查（只读）：bg 进程都是 `claude daemon` 的子进程（daemon 父进程是 1），进程树找不到开它的 pane；ccmux show 对 bg 只有 cwd / project / mainRepoRoot，originInvocationId 为 null。可靠的只有 cwd：开 bg 的会话几乎总在同一个项目目录里。

### T64 按 session 分组时后台会话跟同 cwd 的前台会话走
- 定稿：只改 session 分组的键。后台会话（Background）的 session 组键 = 和它同 cwd（先 cwd 相等，再退 mainRepoRoot 相等）的**前台**会话所在的 tmux session；多个候选时取前台会话最多的那个 tmux session，再并列取 LastActivity 最近的。找不到同目录前台会话时保持现状（有 pane 用 pane 的 session，没有就 (no pane)）。attach 前后组键一致，卡片不再搬家。cwd / project / state 分组不变。卡片上的 `bg` 标签不变；预览标题仍 `bg · %id`。不改 attach 窗口放在 agmux-bg 的做法。
- 验收：单测 一个 bg + 两个前台（blessmuse 组），attach 前后组键都是 blessmuse；两个 tmux session 都有同 cwd 前台会话时按数量、再按最近；只有 mainRepoRoot 相同（worktree）也算；没有同目录前台会话且无 pane → (no pane)，有 pane → 其 pane 的 session；非 bg 会话键不变。黑盒 假 ccmux 两前台一后台同 cwd，按 session 分组后台卡在前台组里，i 之后仍在，agmux-bg 组不出现（没有别的 bg）。

### T65 不把 claude daemon 的备用进程当后台会话（用户 2026-10-20 截图 images/32.png：点了 bg 卡冒出和 shopify-orch 一模一样的会话）
- 已查（只读，用户机器）：`claude --bg` 的 daemon 会预热「备用」进程（ps 里 `--bg-spare …claim.sock`），ccmux show 把它们也列成 trackingMode background：id 有、pid 有、logPath null、lastPrompt ""、prompts []（本例 dce23d2f、03c4fd88）。看板按项目名把它们显示成「blessmuse · bg · working」的空卡。用户对这种空卡按 i → agmux 跑 `claude attach dce23d2f` → daemon 把这个备用进程认领成一个**新会话**，内容是 fork 当前目录里正在跑的那个会话（63498847 = `--fork-session --resume a3f41046.jsonl --name "shopify-orch (2)"`，updatedAt 与 dce23d2f 只差 9 s，新备用 03c4fd88 的 pid 紧挨着它），于是出现和 shopify-orch 完全一样的副本；再点一个空卡再来一份。agmux 自己只跑过 `claude attach <id>`，fork 是 daemon 的行为。注意：被认领的进程 ps 里仍带 `--bg-spare`（701ce329 是真会话也带），所以不能按进程参数判。
- 定稿：store 里过滤：Background 且 logPath 为空且 lastPrompt / prompts 都为空的记录是备用进程，不进 Sessions（show 列表也不列）。其余不动。README 后台会话段加一句：daemon 的备用进程不显示。
- 验收：单测 ccmux 记录 background + 无 logPath + 无 prompt → 不出现；有 prompt 无 logPath（刚起的真后台会话）→ 出现；有 logPath 无 prompt → 出现；native 记录不受影响。黑盒 假 ccmux 混一条备用记录 → 看板无此卡、show 无此行、不会对它跑 attach。
- 备忘：这是 ccmux / Claude Code 侧的问题（备用进程不该当会话列出来；attach 备用进程会 fork 当前会话），可以给 ccmux 提 issue。用户现在机器上多出的副本：63498847（waiting）和被认领的 dce23d2f，`claude stop` + `claude rm` 清掉即可，agmux 不代做。
- dev 646ae71（T64）：view.go bgHomes 只在 session 分组时用，候选 = 同 cwd 有 pane 的前台会话，退同 mainRepoRoot；数量 → 最近活动 → 名字排序（确定性）；Session 加 MainRepoRoot；README g 行补规则；TestBgGroupFollowsForeground 全覆盖验收 + 三种其他分组键不变；变异 10 杀 10（1 等价）。边界（照准）：候选必须有 pane。T65 进行中。
- 2026-10-20 npm 首版发出：用户开 2FA 后重新 `npm login`，手跑脚本把五个包发到 registry（@kackerx/agmux 0.1.0 + 4 平台包；脚本中途 403「不能覆盖已发布版本」是因为前一次已成功，已改成跳过已发布的）。验证：临时 prefix `npm install -g @kackerx/agmux` → `agmux v0.1.0 (7b1b625, 2026-10-17T12:17:27Z)`，只装了 darwin-arm64 平台包。三条安装路径（brew / npm / go install）全部可用。

### T66 npm 改 Trusted Publishing（OIDC），不再用 token
- 为什么：bypass-2FA 的 granular token 在 npm 侧已不可用（三次 403），且 2027-01 停用；包现在存在了，可以配信任发布。
- 定稿：release.yml 的 npm job 加 `permissions: id-token: write`（contents: read 保留给 gh release download），删掉 `NODE_AUTH_TOKEN` / `secrets.NPM_TOKEN`；setup-node 用 node 24（自带 npm ≥ 11.5，OIDC 需要）；`npm publish` 加 `--provenance`（可选但顺手）。其它不动。README 不用改。
- 用户要做的（代码之外，发下一个版本前）：npmjs.com 上五个包各自 Settings → Trusted Publisher → GitHub Actions：Organization or user `kackerx`、Repository `agmux`、Workflow filename `release.yml`、Environment 留空。配完可以删掉所有 npm token 和仓库 secret NPM_TOKEN。
- 验收：actionlint；yaml 解析；本地无法验 OIDC，真验收 = 下一个 tag 的 npm job 绿。reviewer 核对字段与 npm 官方文档一致。
- dev 11fd25f（T65）：Refresh 里 spare() 过滤（background + 无 logPath + 无 lastPrompt + 无 prompts），show 同路径；ccmux.Session 加 LastPrompt / Prompts；旧夹具补 lastPrompt；TestSpareProcessesAreNoSessions 变异 5 杀 5；README 一句。
- dev 12591e6（T66）：npm job permissions contents: read + id-token: write；node 24；删 NODE_AUTH_TOKEN / NPM_TOKEN；--provenance（官方说 OIDC 默认带，多余无害）。官方文档要点：npm ≥ 11.5.1；repository.url 须与仓库一致（我们是简写 github:kackerx/agmux，首次 OIDC 发布最可能的失败点，报错就改完整 URL）；Trusted Publisher 配置 **2 天内不发布会过期**，用户要在打下一个 tag 前再配；只支持 GitHub 托管 runner。第十三批目标 12591e6，送 reviewer。
- reviewer（12591e6）通过无必改：T64 变异 10 杀 9（存活等价）、全序确定、attach 前后一致、regroup 微秒级不缓存；T65 5 杀 5，过滤在 seen / convert 之前，真机只读对比 ccmux 30 条 → agmux 29 条（少的正是备用 03c4fd88，真后台 701ce329 在）；T66 对照官方文档 node 24 / id-token / 无 token 引用 / actionlint 0；repository 简写无风险（registry 上已发版本存的就是规范形式，publish 自己规范化）。补进用户配置步骤：Trusted Publisher **2 天内要发一次**；「Allowed actions」要勾 `npm publish`；字段大小写敏感。建议 1（repository 改对象形式去 warning）顺手；建议 2（TTYWriter 两个 100 ms 阈值用例并行时偶红一次，复现不出）记备忘。第十三批目标 12591e6，送 tester。
- dev c597ae6：npm 壳包 repository 改对象形式（平台包从壳包读），npm pack 无 warn。随第十三批一起装。

## 第十四批 M5 自家发现层（用户 2026-10-20 点头：去 ccmux、codex 不要、pi 保留、来源策略抽象）
设计：specs/m5-own-discovery/design.md（§4 替换表、§7 done 来源、§9 pi）。顺序：T67 → T68 → T69 → T70（tester 的夹具 T70 可与 T67 并行起步）→ T71。

### T67 Source 接口 + Claude 来源（替换 ccmux show）
- 定稿：store 里 `type Source interface { Sessions() ([]Raw, error) }`（Raw = 与 agent 无关的中间结构：ID、AgentType、PID、SessionID、CWD、Pane、TmuxSession、Name、Status(working|idle|waiting)、Attention(permission|question|plan_approval|"")、StatusChangedAt、Transcript、Background、BgJobID）；Poller 持有 `[]Source`，合并后走现有 convert。Claude 来源读 `~/.claude/sessions/*.json`（`$CLAUDE_CONFIG_DIR` 优先，与 statusline 同一套路径逻辑）：`kill(pid,0)` 活着才算，`procStart` 与 ps 的进程起始时间不一致（pid 复用）丢；`spare:true` 丢（T65 的 spare() 及 ccmux.Session.LastPrompt/Prompts 删）；字段映射见 design §4：tmux 字段 `agmux # main:@14.%37` → TmuxSession = `agmux # main`、Pane = `%37`；status busy→working、waiting→waiting、idle|shell→idle；waitingFor `permission prompt`→permission，其它非空→question；尾部最后一个 tool_use 是 ExitPlanMode 且 waiting → plan_approval；StatusChangedAt = statusUpdatedAt；kind bg → Background，id = jobId，状态 / transcript / 名字从 `~/.claude/jobs/<jobId>/state.json`（照 ccmux deriveBackgroundState：tempo blocked → waiting（有 questions → question，needs 非空 → permission）；done|stopped|failed → idle；其它 working；logPath = linkScanPath；name = state.name）。Name = 登记 name（nameSource 任意），空则 T58 的 custom-title，再空项目名。Transcript = statusline 存的 transcript_path，否则 `projects/<cwd 非 [A-Za-z0-9] 转 ->/<sessionId>.jsonl`。pendingTool = 尾部最后一个未配对 tool_use 的 name（有 tool_result 配对则空）。Branch = 尾部最后一条带 gitBranch 的条目；Project / MainRepoRoot：`git -C cwd rev-parse --show-toplevel --git-dir --git-common-dir` 一次按 cwd 缓存（git-dir ≠ common-dir 即 worktree，MainRepoRoot = common-dir 的父目录；project = MainRepoRoot 的 basename，无 git 则 cwd basename）。Subagents：先真机只读验「只有子代理在跑时登记是否 busy」，是就不读 subagents 目录；否则 `subagents/agent-*.jsonl` 10 s 内有写入算 agents 在跑。
- done：Status idle 且 StatusChangedAt 新于 seen.json 的记录 → Done；seen key 改 id + StatusChangedAt（现有 MarkSeen 路径不变，后台会话同样适用）。
- `agmux show` 同源；列不变（第 8 列 bg）。
- 不动：board、avatar、statusline、T60 attach 认领（list-panes 仍跑）、config。internal/ccmux 包先留着不删（T71），但 Poller 默认只装 Claude 来源，`ccmux` 二进制不再被调用。
- 验收：单测 登记文件夹具（interactive / bg / spare / 死 pid / procStart 不符 / 坏 JSON 只丢那条）→ Sessions 正确；tmux 字段解析（含 session 名里带 ` # ` 和 `:`）；status / attention 映射表每格一例；jobs state 四种 tempo/state；done 三步（working→idle 变 done；MarkSeen 后 idle；再变又 done）；Name 三级回退；Transcript 两级回退；pendingTool 配对 / 未配对；worktree 的 MainRepoRoot；show 输出。真机只读：dev 用自己的 Claude 会话对比 ccmux show 的 29 条与新来源的列表（id 集合、pane、状态一致；差异逐条解释）。

### T68 pi 扩展 + pi 来源 + setup 安装
- 定稿见 design §9。扩展源码放 internal/pi/agmux.js（go:embed），`agmux setup` 多一段：dry-run 打印将写入 `~/.pi/agent/extensions/agmux.js` 的差异，`--write` 写入（已有且内容一致跳过；用户改过的不覆盖，提示）。pi Source 读 `~/.pi/agent/agmux-sessions/*.json`，pane 爬 ppid；AgentType = "pi"。无 statusline → ContextPct -1、Model 空。r / x 见 §9。
- 验收：单测 扩展文件内容包含 7 个事件名；setup dry-run / write / 不覆盖；pi 文件夹具 → Sessions；ppid 爬 pane（三层、找不到 → 无 pane）；底栏提示条件。真机：dev 装扩展后起一个一次性 pi 会话（`pi` 在临时目录，问一句退出），看板出现 / 状态变化 / 退出消失；装的扩展文件事后留着（用户要用），一次性会话的 jsonl 删掉。

### T69 r / x / d 不走 ccmux
- r：Claude 前台 = SIGTERM pid，等 ≤5 s 退出，`tmux send-keys -t <pane> 'claude --resume <sessionId>' Enter`；Claude 后台 = `claude respawn <jobId>`；pi = SIGTERM 后在 pane 里 `pi --session <transcript 路径>`（pi 1.1.0 支持 --session <path|id>；-c 不指定哪个、-r 弹选择器，都不用）。x：前台 SIGTERM（等 ≤2 s，不行 SIGKILL）；Claude 后台 `claude stop <jobId>`（输出含 No job matching 视为成功）。d：`git -C <cwd> rev-parse --show-toplevel` 到仓库根 exec `hunk diff --watch`；没有 hunk → `git diff` 走 `$PAGER`/less。pin 跨重启靠名字（T53）不变。
- 验收：单测 假 runner 记 argv / 信号；三类会话三种动作；hunk 缺失回退。真机 dev 用自己的一次性会话各做一遍 r / x。

### T70 e2e 夹具换登记目录（tester，与 T67 并行起步）
- 假 ccmux 二进制 → 假 `$HOME`（或 `$CLAUDE_CONFIG_DIR`）下的 `sessions/*.json` + `jobs/*/state.json` + `projects/<slug>/<id>.jsonl`，pi 用 `~/.pi/agent/agmux-sessions/*.json`；pid 用 harness 自己起的长命进程（`sleep`）保证活着，procStart 由 harness 按 ps 填。lib.py 的 fixture 表保留语义，改生成器。23 个文件逐个过。

### T71 删 internal/ccmux、README / CREDITS
- README 前置条件去掉 ccmux，加 `agmux setup --write` 装 pi 扩展；CREDITS 致谢 ccmux 的实现思路（MIT）；`agmux show` 文档。用户自己卸 ccmux 的三个 hook。
- dev 真机验（T67 前置）：只有子代理在跑时登记 status = busy（65 s 连续，statusUpdatedAt 不变）→ 不读 subagents 目录；AgentsWorking 恒 false（`working · agents` 细分没了，照准）。`shell` = 后台 shell 在跑、主会话空闲 → 按设计映射 idle（子代理再开后台 shell 这种情况会显示 idle，接受）。细节：sessions 目录还有 `<pid>.<hash>.key` 文件，只 glob `*.json`；procStart 与 `ps -o lstart=` 同格式，一次 `ps -o pid=,lstart= -p …` 校验全部；时间字段是毫秒时间戳。

### T72 agent 角色优先读 claude-agent 状态文件（agent-builder 2026-10-20 转达，称用户已同意；用户可否决）
- 背景（agent-builder 实测 2.1.301）：/clear、裸 resume 之后新 jsonl 没有 agent-setting 行，进程参数也看不到角色，handoff 每次交接都 /clear，所以常驻角色交接后按 transcript 判不出来。用户机器上 SessionStart hook `~/.config/bin/claude-agent-record` 把 hook 输入的 agent_type 写到 `${XDG_STATE_HOME:-~/.local/state}/claude-agent/<session_id>`（一行：角色名或 `-`），只增不删。我核过：48 个文件，内容如 `storefront-qa`；settings.json 第 75 行挂着；`~/.config/bin/claude-hud` 已这么读。
- 定稿：Session.Agent 先读该文件（按 sessionId；trim；`-` 或不存在 → 退回现有 agent-setting 尾部读法）。文件小，每次 poll 直接读，不缓存。README agent 列说明加一句这个来源（hook 不是 agmux 的，不装）。排在 T67 之后（用 T67 的 SessionID）。
- 验收：单测 文件有名 → 用它；`-` → 退回 tail；不存在 → 退回 tail；XDG_STATE_HOME 生效；空白 trim。

### T73 底栏补 pin / mark_read 提示（用户 2026-10-20 问「置顶键是什么来着」）
- 现状：footerFor 没列 pin 和 mark_read（T53 / T55 时我定的不加，错了）。
- 定稿：footerFor 在 avatar 之后加 `%s pin`（pin）和 `%s read`（mark_read），都不要 pane；用配置里的第一个键（用户是 t / q）。底栏放不下时现有的截断逻辑照旧。
- 验收：单测 默认配置底栏含 `t pin` 与 `m read`；配置 mark_read = ["q"] 时显示 `q read`；e2e 现有底栏断言跟着改。
- dev 157146f（T67）/ 58dc5bd（T72）/ d4e6387（T73）：Raw + Source（raw.go）、Claude 来源 claude.go（kill(pid,0) + ps lstart 校 procStart，**ps 必须 LC_ALL=C TZ=UTC** 否则全被当复用丢掉；spare 丢）、状态 / attention 映射、bg 走 jobs state、Transcript 两级、分支 / 待决工具 / plan_approval 在 convert 里从尾部取、git-common-dir 一次按 cwd 缓存、done = idle 且 seen 无同时刻记录；testdata/sessions.json 替 show.json；Subagents / AgentsWorking 恒空。真机只读 34 vs 32（差两个 pi）、32 条 id / 状态一致；分支 / 待决工具 / 后台名比 ccmux 准。T72 / T73 照定稿；变异 24 / 6 / 2 全杀。
- 我定三件（dev 的 2 / 3 / 8）：(a) tmux session 名**保持原来按第一个 ` # ` 截短**（用户一直看到的是 `agmux`、`blessmuse`，完整名又长又吵；跳转用 pane id 不受影响），组键 / 组标题 / T64 的候选统一用短名，e2e 不用改；(b) 名字优先级：registry 的 nameSource = user 的 name → transcript custom-title → **pane 标题摘要**（照 ccmux summaryFromPaneTitle：去掉 ✳ / 盲文 spinner 前缀，`Claude Code` 当空；list-panes 加 `#{pane_title}` 一列，tester 的假 tmux 同步）→ registry 的 derived slug → 项目名。否则用户现在看到的「PAP与A票assignee解析」这类摘要名会变成 `cp-paptask-52`；(c) done 基线：看板**首次 poll 时把当时已 idle 的会话全部写成已读**（之后的 working→idle 才算 done），否则升级后一打开二十多张 done。坏登记文件静默丢弃先接受（备忘：底栏提示）。T67 补一个 commit 后整批送审。
- dev 2bb69b3（T67 abc）：shortSession 三处统一；五级名字 + 摘要剥 ✳ / 盲文 spinner / `Claude Code`；list-panes 五列（旧四列兼容）；Baseline 只在 runBoard 置，首次成功轮询把 idle 且 seen 无记录的写已读；真机名字全部与 ccmux 一致（后台更好）；变异 18 杀 18。备忘：pane 默认标题（主机名）不过滤（ccmux 同）；第二块看板启动会把刚 done 的标已读。第十四批前半目标 2bb69b3（T67 157146f+2bb69b3、T72 58dc5bd、T73 d4e6387），送 reviewer。
- reviewer（2bb69b3）退回 2 必改 + 1 spec 问题：(1) 真机复现：用户 seen.json 里 4 条 ccmux 时代的记录 key 比登记的 statusUpdatedAt 晚 300–350 ms，baseline 对「有旧记录」的不补，升级首开这 4 张读过的卡变 done；(2) 三个分支无测：alive 的 EPERM（alive(1) 应为真）、procStart 空白归一化（夹具两边都写了双空格）、git 结果按 cwd 缓存（删掉缓存没用例挂）。spec 问题：新会话一出现若登记就是 idle（新开未发消息 / clear 后新 id / T69 重启后新进程）会被判 done。建议 1：尾部 gitBranch = "HEAD" 显示成 HEAD（ccmux 显示 main）；2：submodule 里 project 会成 "modules"（备忘）；3：psStarts 退出码 1 分支走不到（注释改准）；4：后台会话 StatusChangedAt 取登记的 statusUpdatedAt 而状态来自 jobs/state.json，若 job 状态变时登记时间不动，MarkSeen 后第二次 done 测不到（dev 真机看一次）。已核：真机 show 31 条状态 / 名字全一致（少 2 pi）、pane 30/31 一致（差的那条是 attach 出来的更对）、撕裂读 48019 次 0 失败、映射 / 名字 / 短名 / T72 / T73 变异全杀；45 变异 41 杀。
- 我定：**done 改成「本 Poller 这次运行里见过它非 idle」+ idle + seen 无该时刻记录**（内存 set），`agmux show` 不涉及；这同时解决必改 1 和 spec 问题，**Baseline 整个删掉**（看板重启后旧 done 回 idle，与 ccmux 的内存态一致，接受），不做 ±1 s 容差。必改 2 三个用例补上。采纳建议 1（"HEAD" 当没有分支，往前找上一条非 HEAD 的，没有就空）、3（注释改准）；建议 4 dev 真机看一次：登记时间不随 job 状态动就改用 state.json 的 updatedAt 作后台的 StatusChangedAt；建议 2 备忘。
- dev 89b6f5e（T67 退回）：done = 本次运行见过非 idle（worked 集合，消失的 id 清）+ idle + seen 无同时刻；Baseline 全删；TestDoneNeedsAWorkingSession 四例；EPERM / procStart 单双空格 / gitCommonDir 计数三用例；HEAD 当无分支往前找；psStarts 注释。真机：后台会话登记 statusUpdatedAt 只在状态变化时动且与 state.json 一致，state.json 的 updatedAt 在 working 期间随进度动 → 后台 StatusChangedAt 不改（建议 4 关闭）。第十四批前半目标 89b6f5e，送复审。
- dev 0d68f51（T68 扩展 + setup）/ 8b84479（T68 来源）：internal/pi/agmux.js 9 事件，写 `<pi 目录>/agmux-sessions/<pid>.json`（多一个 statusAt 作 done 时间键，updatedAt 改名换工具也动）；setup 装扩展（dry-run / 一致跳过 / 改过不覆盖 / 临时文件不以 .js 结尾）；Pi 来源（存活 + 启动时间不晚于文件写入防复用）；list-panes 加 `#{pane_pid}` 六列、`ps -axo pid=,ppid=,comm=` 一轮一次且只在有 pid 无 pane 非后台会话时跑；ppid 爬 pane ≤64 层防环；pi 名字 = 登记 name 否则项目名（pi 标题不是摘要）；pi 分支 `git branch --show-current` 每 10 s；提示 `pi: run agmux setup --write`（有 pi 进程、零 pi 会话、扩展未装；30 s 查一次）在消息行。真机：扩展已装到用户 ~/.pi/agent/extensions/agmux.js（留着），一次性 pi 会话 idle→working+bash→idle、/name 生效、/quit 文件消失、pane %424 / session agmuxpiprobe；已清理，用户两个 pi 会话没碰（要重启才会出现）。变异 14 + 21 全杀。小瑕疵：setup --write 打两行（would install + installed）→ T69 顺手合成一行。第十四批后半目标 8b84479 送 reviewer（与前半复审并行）。
- reviewer 复审 89b6f5e 通过无必改：三存活变异已杀；worked 新变异全杀；真机副本 3 轮 poll done 为 0，4 张旧记录卡不再 done；建议 4 理由复核成立（登记 statusUpdatedAt = state.json lastTerminalAt + 0.3 s）。采纳建议：**worked 之外记第一次见到该 id 时的 StatusChangedAt，idle 且 StatusChangedAt 比它新也算干过活**（2 s 内 idle→busy→idle 的短回答 / compact 不漏 done；新开会话首见时间即启动时刻，不假 done）→ 并进 T69 的 commit。
- reviewer（8b84479）退回 1 必改（真机复现）：提示条件写成「已装就不提示」，用户现在「已装扩展但两个老 pi 没重启」既无卡也无提示 → 改成「有 pi 进程 && 无已知 pi 会话」就提示，文案分两种：未装 `pi: run agmux setup --write`；已装 `pi: restart the running pi sessions to show them`。采纳建议 1（只对 AgentType pi 爬进程树，Claude 登记自带 tmux）、3（消息行顺序 err → noPane → hint，hint 最后先被截）、4（Alive 判死时删掉 agmux-sessions 里自己的死文件和 .tmp）；建议 2（Claude 尾部无分支时也走 git branch 兜底）接受，README 一句。已核：扩展与 types.d.ts 对得上、shutdown 顺序安全（读了 pi 源码）、node 测试实跑、setup 各况、pid 复用边界、爬树防环、变异 20 全杀。
- dev 47211e3（T69）：control.go，Runner = Restart / Kill / Review + tmux 两方法，board / store 不再 import internal/ccmux；r：前台 SIGTERM ≤5 s → SIGKILL → pane 里 `claude --resume <sid>`；pi `pi --session '<transcript>'`；后台 `claude respawn`；x：SIGTERM ≤2 s → SIGKILL；后台 `claude stop`（No job matching 算成功）；pid ≤1 / 本进程拒绝；d：rev-parse 根 → hunk diff --watch，无 hunk → git diff 走 $GIT_PAGER / $PAGER / less -R；变异 22 全杀；真机一次性 Claude（haiku）与 pi 各 r / x 一次，pid 换、sid 与 pane 不变，x 消失；未验：后台 stop / respawn、真机 d。setup 两行合一。副作用：dev 临时目录的信任记录留在用户 ~/.claude.json。
- dev 38d2718（T67 复审建议）：首见 StatusChangedAt，idle 且时间变新 → worked；四用例。
- dev 3ab2204（T68 退回）：提示两种文案、30 s 一查；只 pi 爬进程树；消息行 err → noPane → hint；判死删 .json / .tmp；README 分支来源 + pi 两种提示。第十四批后半目标 3ab2204（T69 + 两补丁）送 reviewer；T71 派 dev。
- dev 4a0e4c9（T71）：删 internal/ccmux；注释 / 测试名 / 假报错改准；review 键说明改（config 模板 + golden 同步）；README 前置条件（tmux、Claude Code ≥ 2.1.296、可选 pi ≥ 1.1、可选 hunk）、show 文档；根目录新建 CREDITS.md 致谢 ccmux（MIT，epilande），头像的 CREDITS 保留原处（照准）。第十四批全部代码到位：目标 4a0e4c9；reviewer 审 3ab2204 中，4a0e4c9 追加送审；tester 等 T70 夹具后整套。
- reviewer（4a0e4c9）退回 3 必改（都在 T69 的 r，真 tmux 复现）：(1) 进程已不在时 r 照样往 pane 敲 `claude --resume` → 连按 r 把这行当 prompt 发给刚重启好的 agent；改成 end 前先 alive，不在就报错 "<name> is not running (restarted already?)"，x 不变；(2) SIGKILL 后仍在（僵尸 / D 态）end 返回 nil → 等待后再查 alive，还在就报错，不敲命令；(3) AgentType 非 claude / pi 走到 `claude --resume` → case 加 `== "claude"`，default 报 "r can't start a <type> session again"。spec 问题（38d2718 带出）：r 一个 idle 会话后 ID 不变、PID 变、StatusChangedAt 变新 → 被判 done；采纳建议：firstAt 同时记 PID，PID 变了当首见并清 worked。采纳建议 1（pi.go 判死：ps 解析失败不删文件，`else if ok`）；建议 2（GIT_PAGER 盖 core.pager；用户 LESS 带 --quit-if-one-screen 会一闪）→ 改成不设 GIT_PAGER，用 `git -c core.pager='less -R' diff` 仅当用户没配 core.pager 时？过度；**定：无 hunk 时直接 `git --no-pager diff | less -R`**（不碰用户 pager 配置、不受 LESS 影响）；建议 3（rev-parse 报错文案与顺序）顺手。T71 通过（版本门槛 2.1.296 为实测下限；CREDITS 署名一致；扩展算重写不附 MIT 全文）。reviewer 自报：一次探针 PATH 没设对在私有 server 真起过一个 claude 约 2 s，已核无登记 / 无信任记录 / 无新目录。
- dev 18565b6（第十四批退回）：r 先 alive 不在报错；SIGKILL 后仍在报错不敲命令（immortal 用例）；分支按 claude+sid / claude 无 sid / default 报错；firstAt 记 PID；pi 判死只删 reused 的；d 无 hunk `sh -c 'git --no-pager -C "$1" diff --color=always | less -R'` 且 LESS 清空（--color=always 照准，d 本来就是看彩色 diff）；rev-parse 文案；TestReviewFallbackRuns 真跑；README d / r 两行。变异 12 全杀。第十四批目标 18565b6，送复审。
- reviewer 复审 18565b6 通过无必改：连按 r 真 tmux 探针第二次报错、新 agent 收到 ""；SIGKILL 不死报错无 tmux；codex 报错无信号；firstAt 记 PID；pi 判死；d 的 sh -c 在含空格 / 引号 / $ 的根路径下实跑正确、--no-pager 盖掉 core.pager；变异 13 全杀；真机副本 hint 出现。备忘：同一 ID 两个活进程时 worked 每轮被清永远不 done（老问题，ID 重复本就出两张卡）；d 管道退出码是 less 的，git 失败一闪而过。**第十四批 reviewer 通过，目标 18565b6（89b6f5e + 18565b6）；等 tester T70 夹具 + 整套。**
- tester（12591e6，套件 42c2817）：整套 1116 条 3454 s 零红（首跑 94 红全是预期变化：T65 让 group 夹具里无 logPath / lastPrompt 的后台会话被当备用过滤 → 夹具补 lastPrompt；t62 伪版本在 v0.1.0 之后变 v0.1.1-0.… 放宽；t63 改 id-token；t30 / t11 负载抖动复跑绿）。T64 15 条、T65 8 条（挂 pane 的空 background 记录也过滤，照字面；第十四批后 ccmux 路径已不存在，无所谓）、T66 9 条。**第十三批通过，目标 12591e6，装 c597ae6**（12591e6 + npm repository 字段）。T70 做法：保留 `env.write_show(S)` 入口，在 lib 里翻译成登记目录，23 个文件不逐个重写。
- 2026-10-20 18:25 已装 c597ae6（干净 clone，vcs.modified=false）。T64–T66 待用户验收（后台卡跟 blessmuse 组、备用进程不显示）；--stay 常驻看板要 Q 退了重开才用上新版。
- tester（18565b6，套件 f36add1）：整套 1183 条 6374 s 零红。T70：`Env.write_show` 入口不变，lib 的 sync_registry 翻成 sessions / jobs / projects / statusline + 每会话一个活 sleep 进程（procStart 按 LC_ALL=C TZ=UTC ps）；假 claude / hunk / ps（藏本机真 pi）；fake tmux 日志一次调用一文件（修 t44 偶红）；done 旧语义用 rearm 表达。删 / 改的旧用例均为预期（子代理计数、ccmux 错误、spare、无名卡、空项目）。新 t67 42 / t68 24 / t69 20。没覆盖：真会话、真 bg、pi 多层爬树、hunk 真程序、Linux、kitty。**第十四批通过，目标 18565b6，装。M5 完成。**
- 2026-10-21 03:26 已装 18565b6（干净 clone，vcs.modified=false）。只读核：登记里活 pid 非 spare 26 个 = `agmux show` 26 行，id 一一对应；pi 0（用户两个 pi 未重启）。T64–T73 待用户验收。

## 第十五批 M6 找回会话 + 键位全可配 + 中英文（用户 2026-10-28「先来个 MVP」）
设计 specs/m6-recall/design.md。顺序：T74（i18n + 键位）→ T75（索引 + 来源）→ T76（`agmux -r` 界面）→ T77（看板 `?` 入口）；tester T78 与 T75 起并行备夹具。做完由 dev 自己实测 fzf 耗时。

### T74 `[ui] language` 中英文 + 键位全部进 `[keys]`
- 为的是：用户要默认英文、可切中文；每个键可配。
- 做法：design §5 §6。先 `git grep` 列出看板里全部用户可见字符串和硬编码按键，贴进 tasks.md 这一项下面让我过目后再改（清单一条一行）；`internal/i18n` 两张表；`[keys]` 补齐缺的动作并进模板 + golden。
- 验收：单测两表 key 集合相等、缺 zh 回落、`$AGMUX_LANG=zh` 下底栏是中文；golden 更新；`agmux config` 回读；`go test ./...`。黑盒（tester）：zh / en 底栏、消息行、选择框各一例；新收进来的键改绑后生效、原键失效。

### T75 recall 索引 + Claude / pi 来源
- 为的是：按内容找回会话的数据底座，首建一次后每次打开毫秒级。
- 做法：design §3 §4。`internal/recall`：Source 接口 + claude.go + pi.go + index.go（缓存、增量、首建协程、噪音过滤、去重、上限）。`agmux recall --dump [关键词]` 先出一个纯文本子命令（每行 `来源\tid\t名字\t项目\t分支\tmtime\t命中数\t片段`）给 tester 和自己验，T76 的界面在它之上。
- 验收：单测 两来源各读一个真格式样本（夹具从真机匿名化抄）、增量续读（追加后只读新字节）、变短整读、文件消失删索引、噪音过滤表、去重、上限、缓存版本不符重建、原子写；`--dump` 在用户机器上首建耗时与第二次打开耗时各一个数字写进 tasks.md；fzf 对全量句子一次匹配的耗时写进 tasks.md。
- 真机只读：不改任何 transcript；缓存目录只写 `$XDG_CACHE_HOME/agmux/recall`。

### T76 `agmux -r` 界面
- 为的是：主入口；design §2 的列表 / 预览 / 筛选面板 / 底栏 / enter / copy / open。
- 做法：bubbletea 新 Model（internal/recall/ui 或 board 包内，取小的），复用 board 的预览框绘制、fuzzy 高亮、footer 风格；筛选面板照 T52 选择框的画法。enter 原地 exec：`syscall.Exec` 前恢复终端（tea 退出后），cd 用 `os.Chdir`。在跑的会话靠 store 的 Poller 一次 Refresh 合并（不轮询）。
- 验收：单测 Model 的键 → 状态（筛选、范围、焦点、enter 三种情况、esc 三级）；黑盒（tester 真 pty）：打字筛选、高亮、预览命中、面板勾选、ctrl+t、ctrl+y（假 pbcopy）、ctrl+o（假 $EDITOR）、enter 对关了的会话 exec 假 claude / 假 pi 且 cwd 正确、在跑的不 exec、目录没了不 exec、窄屏无预览、首建进度行、`--reindex`。
- dev 真机：用自己的历史会话搜几个词，截图贴 tasks.md。

### T77 看板 `?` 入口
- 做法：board 里按 `recall` 切到 T76 的 Model（同进程），esc / `?` 回看板；enter 关了的会话：普通 pane 原地 exec；popup / --stay（现有 stayFooter / popup 判定）→ 会话 cwd 下 `new-window -c <cwd> -n <名字> -- <resume argv>` 再 switch；在跑的 → jump。
- 验收：单测三种 enter 路径；黑盒 假 tmux 下 popup / --stay 走 new-window，普通 pane 走 exec。

### T78 e2e（tester，与 T75 并行起步）
- 夹具：匿名化的 Claude / pi transcript 样本各几份（不同 cwd、worktree、有无名字、有噪音行）、`$XDG_CACHE_HOME` 指到临时目录、假 claude / pi / pbcopy / $EDITOR 脚本；真 pty 跑 `agmux -r`。
- dev T74 清单（18 组字符串 + 7 处硬编码键）已过目，照准 5 点：(1) 新动作 `clear_filter`（esc，仅有过滤时）、`preview_up` / `preview_down`（pgup / pgdown，3 行）；(2) 打字态 / 锁死态的接受 = `jump` 的键、取消 = `quit` 里非单个可打印字符的键；backspace / ctrl+h / ctrl+c 固定不进 [keys]，模板头注一句；(3) recall_* 八个动作随 T76 / T77 再加；(4) control / store / tmux 的错误文案留英文；(5) `[ui] language` 校验 en|zh，`$AGMUX_LANG` 优先且非法值忽略，i18n 包内全局、测试默认 en。另：bgNote 里写死的 "enter or i" 改成 First(jump) / First(reply)；setup 的输出也翻，config 读不出时按 en。
- dev f3b37d5（T74）：internal/i18n（en / zh，T(key,args)，Resolve env > config > en）、三种一致性测试（key 集合、参数个数、源码 ↔ 表）、[ui] language 校验、新动作 clear_filter / preview_up / preview_down、打字态接受 / 取消取 jump / quit 的非字符键、写死键名全部改取配置、golden + README；变异 41 杀 38（3 等价）。zh 表我过了一遍，照准（闪跳 / 直通 / 看 diff / 置顶 / 已读）。dev 提的边界 (1)：quit 只配字符键时打字态 esc 不再取消 → 我定加兜底：打字态 / 锁死态里 esc 恒取消、enter 恒接受，配置的非字符键是额外的（一个小 commit）；(2) 包级状态照旧。没覆盖：zh 状态词比英文宽 1–2 格，窄卡片截断留 tester 黑盒看。
- dev ad77f81（T74 兜底：打字态 / 锁死态 esc 恒取消、enter 恒接受）。reviewer（ad77f81）退回 4 必改（小）：(1) 参数一致性测试排序后比较抓不到类型换位 → 按顺序比或 Sprintf 断无 "%!"；(2) preview_up/down 的「预览没开」守卫无测试 → 加关闭后 pgup 再开 scroll==0；(3) hintLine 过滤空串成死代码 → 改 strings.Join 删掉；(4) control.go 两条错误写死键名 x / r → 改成动作名（kill / restart）。采纳建议 a（zh 卡片状态词断言）、b（bg_plain 分支用例）、c（searchKey 先判 esc 再判 jump，底栏 FirstCtl 跳过对方固定键）、e（README 提 [ui]）；d（<44 列顶栏 / 底栏不截断，老问题）备忘；f 不补。已核：zh 状态词不比英文宽（运行中 / 等待中 6 格 vs 7）、20–240 列 × 13 状态溢出差 0、112 屏 en View 逐字节与 91444cd 一致、真机 zh 两尺寸无溢出。变异 46 杀 36（3 等价）。
- dev f21ff8e（T74 退回 4 必改 + a b c e）；7c3334c（T75）：internal/recall（Source / Head{+Path,+Custom} / Line / Clean / eachLine、claude.go、pi.go、index.go、match.go）、xdg.CacheDir、`agmux recall --dump [--reindex] [--] [query...]`；缓存 `<来源>-<文件名>.json` 原子写、新→旧并发、续读 / 整读 / 删除规则齐全；噪音表加 `<command-message` `<bash-`；custom-title 优先于 ai-title（真机 8 例会被盖）。真机只读：独立 Python 实现对 261 Claude + 143 pi 全部 404 个缓存逐个一致。**耗时（热页缓存）**：首建 wall 1.5–4.3 s（3.8 GB + 174 MB，12,799 句，缓存 4.4 MB）；二次打开 26–77 ms（Update 22 ms）；fzf 单线程全量一次：单词 23–56 ms，两词 64–71 ms。变异 107 杀 100（7 等价 / 不可测）。
- 我定（T76 一起做）：(1) 匹配超 50 ms → fuzzy.Filter 按 NumCPU 切块并行再按分数稳定合并（dev 估 10 ms 级），仍超再加 30 ms 防抖；(2) 命中数只算句子正文命中（对已命中的句子再按正文过滤一遍），列表排序仍用含前缀的匹配串；(3) Head.Path / Custom、pi 缓存按文件名、噪音两条照准；(4) 同 id 跨项目共用缓存名、原地改写续读、来源目录为空清缓存三条备忘。
- tester 会话重起（-w 独立 worktree），派 T74 / T75 黑盒 + T76 / T77 用例骨架。
- reviewer 复审 f21ff8e 通过无必改（4 必改 + a b c e 变异 15 杀 13，2 可接受）。**T74 reviewer 通过，目标 f21ff8e**；tester 黑盒进行中。T75 7c3334c 审查中。
- reviewer（7c3334c）退回 2 必改 + 1 spec：(1) Update 末尾按 mtime 排序无测试（并发 worker 完成序乱，真机空查询 151 / 169 行乱序）→ Workers>1 假 Source 越新睡越久，断言降序；(2) match.go 同分用户句优先的条件是死代码（Lines 用户在前 + 稳定排序）→ 删条件、注释说明；(3) 会话间同分「用户句优先」我定按字面：分数 → 最佳句是不是你说的 → mtime，补测试。采纳建议 a（`<pasted_content…>…</pasted_content…>` 块整段去掉、块后的话才是句子，真机 35 例）、b（Progress 用独立 mutex 保序）、c（首建期间 Entries() 有序）、d（project() 与 store 的 repo() 抽共用）、e（--dump Update 出错退出 1）、f（Update 顺手清 .tmp-*）、g（空查询片段 = 你最后一句，没有才 agent 的，确认即设计本意）、h（会话名去控制字符）。已核：compact 不重写文件（40 例 size 只增）；405 缓存增量 vs 全量 0 差；噪音前缀真机统计全覆盖；custom-title 4 例；pi 名字 3 例；耗时 首建 1.65 s / 二次 20 ms / 两词最坏 96 ms（T76 并行 + 防抖照定）。变异 59 杀 54。
- dev b75736d（T76）：board 包内 recall.go / recall_view.go，`agmux -r` ≡ `agmux recall`；列表 / 预览 / 面板 / 三级 esc / enter 三种 / ctrl+y（pbcopy → wl-copy → xclip）/ ctrl+o；`fuzzy.FilterParallel`（与 Filter 逐条一致）；命中数只算正文；8 个 recall_* 动作进 [keys] + golden；文案 i18n 28 条；缓存版本 2（+Worktree）。真机：每键全量搜索 3–24 ms（并行前 23–71），**未加防抖**；截图 /tmp/t76-shots/01–09（我看了 02：列对齐、片段开窗、预览折叠正常）。变异 129 杀 125（4 等价）。未修（照准）：锚定 `^` 只能命中来源前缀（备忘，v1 接受）；cwd 取不到时「当前项目」等于全部；`recall` 动作 T77 接。
- dev 56fd337（T75 退回）：按序插入 + 建索引期间 Entries() 有序；三级排序；pasted_content 整块删；Progress progMu；`internal/gitrepo` 统一 project / 主仓库根 / worktree 判定（store 与 recall 共用，顺带修子模块 / separate-git-dir 项目名）；--dump 不可读文件退出 1；清 >1 min 的 .tmp-*；会话名去控制字符。变异 31 全杀。送 reviewer：T75 复审 56fd337 + T76 审 b75736d，目标 56fd337。T77 派 dev。
- reviewer：T75 56fd337 复审通过（gitrepo 与旧 repo() 在真机 76 个目录 0 差）；采纳建议 a（Clean 变了 → 缓存 Version 升 3）、b（保序测试改 channel 放行而非 sleep）、c（CI 的 go test 加 -race）。T76 b75736d 退回 3 必改：(1) 底栏用 cfg.First 把字母键当提示 → 改 FirstCtl（jump 无非字符键兜底 enter，其它没有就不列）；(2) window() 用 rune 下标比 cell 宽度，中文长句命中字被截出屏外（用户主要搜中文）→ 按累计显示宽度算起点 + 中文长句用例；(3) 窄屏先丢分支再丢项目无测试 → 补刚好丢一列的用例。建议 a：真机负载 41 下最慢一键 171 ms，search 在 Update 里同步跑会卡输入 → 我定：**search 放进 tea.Cmd 异步跑，带查询代号丢弃过期结果，不加防抖**；b：relTime ≥ 365d 显示 `1y` / `2y`（4 格内）；c 两条夹具提醒转 tester（Claude 行预过滤要求紧凑 JSON；pty 要回应 OSC 11 背景色查询否则首帧等数秒）。已核：enter exec 终端状态（粘贴 / 光标 / 鼠标 / 备用屏退净、fd 非阻塞清、信号未忽略）、FilterParallel 240 组一致、命中数 / 预览 / 键 / 在跑判定变异全杀；真机 160 列 391 会话、zh、90 列正常。变异 61 杀 57。
- dev 换 session（/handoff）：T77 WIP 在分支 worktree-agmux-dev-t77（c9f8c46，基于 e392eb1，变异 68 跑到一半，不算交付）；主检出里同内容的未提交改动经逐文件比对与 c9f8c46 相同，已按 dev 要求清掉（checkout + 删两个未跟踪文件）。新 session 先收 T77 尾巴，再做 T76 退回；交付带分支 + hash，由我合回 main。
- tester（分支 worktree-agmux-tester，ac557b0）：t74 40 / t75 49 / t76 42（骨架在 b75736d+56fd337 上已过）/ t77 12（skip 等接线）；t17 / t50 因新键与 [ui] 段改断言；未跑整套（忙机器数小时），只跑受影响子集。已合回 main。观察：(1) 空查询片段 = 你最后一句（design §2 措辞已改一致）；(2) quit 只配 `z` 时选择框 esc 关不掉（正常用法不碰，备忘）；(3) 缓存目录建不出来时 --dump 不列（备忘，可退回内存索引）；(4) 空数组存 null 无害；(5) 占用默认键不报错是 T17 既有规则。没覆盖：真 kitty 目视、面板里 down/up 改绑、<100 列逐宽度、pi 的在跑合并。
- dev 分支 worktree-agmux-dev-t77：c9f8c46（T77 代码 + 测试）、dc199ac（T77 补 7 用例，变异 68 杀 65，3 等价 / 需终端）、640ae75（T76 退回 3 必改 + a b c：底栏 FirstCtl、片段按显示宽度开窗、丢列顺序用例、search 异步带全局查询代号、relTime 1y、缓存 v3、channel 保序、CI -race；变异 41 全杀）。超出清单的自查改动（照准）：答案未回时 enter / 复制 / 打开不动；tick 不压未回的 search；代号全进程唯一。已合回 main（merge 4d91819）。待办：看板底栏加 `? recall`（同 T73 的理由）。送 reviewer：T77 + T76 补丁，目标 4d91819。
- dev cfa6913（底栏 `? recall`，i18n「找回」；三个旧断言跟改；底栏 "q quit" 可见门槛从约 190 列提到约 200 列，用户 253 列无影响）。合回 main（merge e242a8b）。tester 的 t74 EN_FOOT 字面串要加 " · ? recall"。**第十五批目标 e242a8b**。
- reviewer（e242a8b）：T76 复审通过（底栏 / 中文开窗 / 丢列 / 异步 search 竞态与代号 / relTime 变异全杀；中文命中尾巴在片段列仅 15 格时被截可接受）。T77 1 必改：popup / --stay 下 new-window 未返回前再按 enter 会建第二个窗口 → 两个 `claude --resume` 并发写同一 transcript → 加「窗口在建」标记，pick 到 resumeWindowMsg 之间忽略 enter + 用例（保留为必改，数据风险）。采纳建议 a：tmux 对 new-window 的 -c / -n 做格式展开（`#S` 等会被替换、目录含 `#` 会悄悄在 $HOME 起窗口），-c 和 -n 把 `#` 换成 `##`，**T60 的 attach new-window 同样处理**；b 不补。已核：三种 enter × 宿主（真 pty exec 终端状态、popup new-window 参数与 switch-client、在跑走 jump）、popup 判定 = TMUX 非空且（--stay 或 TMUX_PANE 空）、同进程切换状态不丢、真 popup 里 indexing 进度与搜索。变异 34 杀 33。T74 / T75 / T76 已通过，T77 修完整体通过，等 tester。
- dev 8e2ac46（T77 退回）：Model.resuming + recallModel.building（离屏重开也拦）、enter / jump 在建期间忽略；tmuxText() 把 # 换 ##，recall 与 T60 attach 的 -c / -n 共用；变异 17 杀 16（1 等价）。合回 main（merge 487dd5a）。**第十五批目标 487dd5a**，送 reviewer 复审 + tester。
- reviewer 复审 487dd5a 通过（probe_double / 离屏重开探针、# 转义四处 + 真 tmux 3.8 实测；变异 11 全杀）。**第十五批 reviewer 整体通过：T74 f21ff8e / T75 56fd337 / T76 e242a8b / T77 487dd5a。** 等 tester 整套。
- tester（分支 worktree-agmux-tester，1620ec1；合回 main 957749a）：487dd5a 上整套 1329 条（t74 40 / t75 49 / t76 42 / t77 15，本批新增 146）。5 红全是测试没跟上产品：t3 两条（底栏加 `? recall` 后 200 列放不下 `q quit`，改 ≥210 列才要求 / 232 列跑）、t7（`kill .*\?` 正则误命中底栏，改 `kill \S+\?`）、t63（ci.yml 改 -race）、t44 轮询用例负载 10–20 时红、单跑绿（时序抖动）；t74 / t17 底栏字面串与宽度跟改。改后受影响 63 条复跑全绿，**整套没有连续一次全绿的记录**（已派 tester 在 957749a 上再跑一次整套补证据，不挡安装）。T76 复跑全绿；T77 15 条：普通 pane 原地 exec、popup / --stay 走 new-window（`-d -P -F #{pane_id} -t =<sess>: -c -n`）+ switch-client、在跑走 jump、目录没了不动、esc / `?` 回看板过滤不丢、tmux 不回 pane id 红字停留、键位可配、zh；487dd5a 的两条（连按 enter 只建一个窗口；`#`→`##`）在 e242a8b 二进制上红、487dd5a 上绿。假 tmux 新增 new-window 应答 + 延迟。没覆盖：真 kitty 目视、面板里改绑方向键、pi 在跑会话合并、recall 屏逐宽度扫。**第十五批 tester 通过，目标 487dd5a（产品）/ 957749a（含测试）**；派 dev 安装。备忘：t44 整套下时序抖动（第一次），再红一次就给它加机制（放宽等待或独立跑）。
- dev 安装 957749a 到 ~/go/bin/agmux（vcs.modified=false，`agmux v0.1.1-0.20261028092037-957749a4e333`）。注意：从 git worktree 检出 go install 不带 vcs 信息（--version 显示 dev），以后安装用 git clone。**第十五批交付，待用户验收。**

## 第十六批：找回验收返修（用户 2026-10-28 用 957749a 验收后提的 5 条）

目标：找回界面的输入和选择像 nvim / television：光标能动、能删词、回车进锁定态再 n/N/j/k 选、ctrl+n/p 滚预览、三个框各有粗边框。细节已写进 m6-recall/design.md §2 的四条新规则。

### T79 输入框（看板 `/` 和找回共用）
- 要做：一个输入框组件两处共用：真光标；left / right、home / ctrl+a、end / ctrl+e、alt+backspace / ctrl+w 删前词、字符插在光标处；光标按显示宽度放（中文 2 格）。编辑键固定不进 `[keys]`，`agmux config` 模板头注列出。
- 为的是：用户在 kitty 里 option+backspace 删词、左右改错字，现在都没反应。
- 做法：`charmbracelet/bubbles/textinput` 可以用（bubbletea 的配套库，光标 / 删词 / 宽度都有），不想加依赖就自己写但行为对齐它；kitty 把 option+backspace 发成 ESC DEL，bubbletea 收到的是 `alt+backspace`，真 pty 里要核一次。
- 验收：单测 光标移动 / 插入 / 删词（含中文、词边界是空格和标点）；看板 `/` 和找回各一条「光标中间插字」用例；tester 真 pty 里发 ESC DEL。

### T80 找回两段式
- 要做：design §2「两段式」整条：enter 进锁定态不 resume；锁定态 n / N 回绕、j / k 不回绕、enter resume、`/` 回输入框改查询、esc 清空回打字态；打字态空查询再 esc 退出；`agmux -r <关键词>` 直接进锁定态；底栏两态分别列键。
- 为的是：用户习惯 nvim 的 `/foo<enter>` 再 n/N；现在打字态 enter 直接 resume，容易误开会话。
- 做法：仿看板 `/` 的 typing / kept 两态（board.go 里现成），recall 模型加一个状态位；打字态上下键照旧。
- 验收：单测 键 → 状态表（两态 × enter / esc / n / N / j / k / `/`）；embedded（看板里）同样走两态；黑盒 真 pty 打字 → enter → j → enter 才 exec 假 claude。

### T81 ctrl+n / ctrl+p 滚预览
- 默认 `preview_down = ["pgdown","ctrl+n"]`、`preview_up = ["pgup","ctrl+p"]`；golden 模板跟改；两态都能滚；看板也生效。
- 验收：单测 + 黑盒各一条。

### T82 三个粗边框
- 输入框 / 列表 / 预览各一个粗边框（lipgloss ThickBorder），颜色 `[colors] recall_input` / `recall_list` / `recall_preview`（默认 `#ff8ebe` / `#6272a4` / `#bd93f9`），模板 + 校验 + golden；标题嵌在上边框：列表 `n/total`、预览会话名。窄屏（<100 列）只画前两个框。高度：三个框加起来不超过终端行数，列表行数相应减。
- 验收：单测 三色从配置读到、默认值；黑盒 frame 里边框字符和颜色、100 / 99 列、24 行下不溢出；dev 真机截图贴这里。

### T83 e2e（tester，与 T79 并行起步）
- t79：真 pty 发 ESC DEL / 方向键 / ctrl+a / ctrl+e，看板 `/` 和找回各一组；t80：两态键表 + 只有锁定态 enter 才 exec；t81：ctrl+n/p；t82：边框字符与颜色、窄屏、矮屏；t74 / t76 / t77 里「打字态 enter 即 resume」的旧断言跟改。
- tester（worktree-agmux-tester 3d2bcfe，先不合：t76 / t77 的 enter 已改成两次，合进去会在 957749a 上红）：t79 13×2 宿主（真光标读 tmux cursor_x/y、ESC DEL / M-BSpace / ctrl+w、中文宽度）、t80 两态各约 17 条 × 两宿主、t81、t82、夹具 lib_recall；二进制 `agmux config` 没有 `recall_input` 前整体 skip（88 条）。三点照准：(1) 打字态有查询时 esc 先清查询不退出（即 design §2「空查询再 esc 才退出」）；无命中列表标题 `0/0`；删词后光标在词原位置、前面空格留不留都接受。(2) 看板 `?` 里空查询 esc 回卡片不退 agmux。dev 做的如与此不同，以这三条为准。

### T84 找回鼠标（用户 2026-10-28 追加，排在 T82 后）
- 要做：design §2「鼠标」整条：单击选中、同行 400 ms 内双击确认、滚轮列表挪选中行 / 预览滚 3 行；`agmux -r` 的 Program 加 WithMouseCellMotion，看板嵌入时 recallUpdate 把 MouseMsg 交给 recall 模型而不是吞掉；ExecProcess 回来后鼠标重开（现有 recallEditedMsg 的处理照旧）。
- 为的是：用户看板已经靠鼠标点卡片，找回也要能点。
- 做法：bubbletea v1 没有双击事件，自己记上次点击的行和时间；行号 → 会话由 view 的布局反推（列表框内第几行 + 滚动偏移），T82 加边框后坐标要算上边框和标题行；看板 board.go 的 mouse() 已有滚轮 / 点击的写法。
- 验收：单测 点击 → 选中、双击 → pick、隔行两次点不算双击、超时不算、滚轮两区各自生效、面板开着忽略；黑盒 真 pty 写 SGR 鼠标序列（`\x1b[<0;x;yM` / `m`、滚轮 64 / 65），`-r` 和看板 `?` 各一组；T83 的 tester 补 t84。
- tester t84 写好（9e776c4，32 条，同样等 `recall_input` 出现再接线）。照准：双击在打字态也算确认（design §2 已写明）；双击 resume 的是被点的那行。
- **用户 2026-10-28 改 T80**：列表已经筛过，n / N 没意义，去掉；enter 进「选择态」，j / k / 方向键挪选中，enter resume，其余（`/` 改查询、esc 清空、带参数启动即选择态）不变。docs 已改（design §2），tester t80 里 n / N 回绕的用例删掉，n / N 在选择态不做事。
- dev 第十六批交付（worktree-agmux-dev-t77 b15f64d，合回 main a60cff2）：4bd541b T79 输入框 input.go 共用（变异 45 杀 44，1 等价）；f9e88fe T81（3 全杀）；4c19fdb + 965aa5e T80 两段式、去 n/N（37 全杀）；12362e5 T82 三个粗框（46 全杀）；b15f64d T84 鼠标（44 杀 40，4 等价）。dev 定的 8 条细节里 7 条照准：空查询 enter 也进选择态；选择态 quit 的非 esc 键结束本屏、esc 清空回输入态；选择态字母键按 [keys]，输入态只有方向键 / ctrl 键；看板 `?` 在选择态或空输入时回看板；双击两态都确认且清计时；矮屏 <7 行不画消息行、<6 行不画框；底栏两态分列。**改一条**：列表标题 `n/total` 的 n = 命中的会话数（像看板 `/` 的 3/14 和 fzf），不是光标位置。**未交付**：T79 的「真光标」（IME 候选框跟随）dev 用反显格代替，另立 T85。真机核过 option+backspace / Left 插字 / enter 进选择态；截图 /tmp/t82-shot-140x30.txt。
### T85 找回输入框真光标（T79 遗留）
- 要做：`agmux -r` 和看板 `?` 里输入态时终端真光标停在输入框光标处（IME 候选框跟着），选择态隐藏；复用看板 cursorOut 的机制，RunRecall 也包一层输出，坐标按 T82 版面算（输入框在第 2 行，列 = 框左边 + 2 + 光标前文本的显示宽度）。
- 为的是：用户主要搜中文，候选框要贴着输入位置；tester 的 t79 已按真光标写（读 tmux cursor_x / y）。
- 验收：单测 坐标函数（含中文 / 滚动的查询）；黑盒 tester t79 现成。
- dev b4c7354（T85 真光标 + 标题 n = 命中数；合回 main 34f3e34）：坐标 = 边框 1 + " / " 3 + 光标前文字显示宽度，长查询按可见段算、indexing 占位扣掉；输入态显示、选择态 / 面板开着隐藏；standalone 用 cursorOut 包输出，看板 `?` 里走看板的 wantCursor；真 tmux 核过 `a优b` x=8、左移 7、选择态隐藏。变异 33 杀 32（1 需真 TTY）。标题 = 命中数 / 同筛选下可搜会话数，无查询时 n = total。**改**：有查询无命中显示 `0/total`（像 fzf），不是 `0/0`——撤回前面给 tester 的 0/0，dev 一行改掉。
