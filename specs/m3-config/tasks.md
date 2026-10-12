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

## 第二批（用户 2026-10-12 验收意见；2026-10-12 晚开工，顺序 T21→T22→T23→T24）

### T21 预览进出都用 `tab`（审查通过 @2bb3fb4；测试中，e2e 旧断言 9 条待改）
- 用户原话：ctrl+] 退出预览焦点不顺手。改成：`tab` = 打开预览（若没开）并进入直通；直通里 `tab` = 回到卡片，预览**不关**；`p` 仍是开/关预览。
- 落地：默认键 `focus_preview = tab`（没开预览时先开再进）、`leave_preview = tab`（原 ctrl+] 不再默认绑，用户可在 config.toml 绑回来）；底栏文案、README、m2-preview/design.md「按键直通」一节同步改（design.md 里「否掉 tab」的理由作废：用户实测后拍板）。
- 代价（已知，用户决定）：直通里 `tab` 到不了 pane——Claude Code 的路径补全、shell 补全要在真终端里按；`shift+tab` 照常直通。
- 验收：黑盒 预览关着按 tab → 预览开 + 边框粉 + 底栏 "keys go to the pane · tab back to the cards"；再按 tab → 边框普通色、预览还在、底栏回卡片提示；p 关预览；直通里 shift+tab 仍 send-keys 到 pane；config `leave_preview=["ctrl+]"]` 时 ctrl+] 也能退。

### T22 搜索高亮改黄底 + 保留过滤后 n/N 上下跳（审查通过 @2069326；测试中）
- 用户 2026-10-12 验收：命中字符不要粉色前景，要像 nvim 的 Search——**黄底深字**（截图：浅黄底 #f2d06b 左右、字 #2d2a2e）。新增颜色项 `colors.match`（默认那个黄），命中字符画成 match 底 + 深字；design.md「命中字符用 focus 色高亮」那句改掉。
- enter 保留过滤后，`n` / `N` 在命中的卡里按布局顺序跳下一张 / 上一张，到头回绕；新增动作 `next_match` / `prev_match`（默认 n / N，和现有默认键不冲突）；没保留过滤时 n/N 不做事；底栏在保留过滤态显示 `n/N next/prev · esc clear`。
- 定稿（dev 提议照准）：保留过滤态底栏是在原整条底栏前面加 `n/N next/prev · esc clear ·`，不整行替换（enter jump 等提示还在）；n/N 和 j/k 一样从 m.current()（屏幕上画着焦点的那张）出发走下一张 / 上一张，不看原始 m.focus（审查退回：焦点卡被轮询改名后不再命中时，屏上焦点已退到第一张，按 n 却原地不动）。
- 审查退回（reviewer @60c2217，探针 cr-t22-60c2217/probes/）：1. n/N 改走 m.current()，删「隐藏卡」分支（TestProbeNFromDisplayedFocus）；2. 补测布局序 ≠ 得分序时 n 按布局序（收 TestProbeNLayoutOrder）；3. 底栏 n/N 提示有哪个键显示哪个；README 配置示例 `down = ["n", "down"]` 换成不撞默认键的键。
- 验收：黑盒 `/ a` enter 后抓屏带色，命中字符格子是黄底；n 三次焦点按布局序循环；N 反向；esc 后 n 无效。

### T23 `s` flash 改成 flash.nvim 式：先打子串再按标签（修复 93cecf7 复审 / 测试中）
- 用户 2026-10-12 验收：现在「一个字母跳一张卡」不对，要和 flash.nvim 一致——按 `s` 后输入字符，卡片的 name/branch/dir 里**子串命中**（大小写不敏感，不用 fzf 模糊）的位置黄底高亮（同 T22 的 match 色），每张命中卡在命中子串后一格盖一个标签字母（focus 底深字）；标签字母**避开**所有命中后紧跟的下一个字符（这样继续打字和按标签不会撞）；按标签跳过去并退出；继续打字缩小范围；退格；esc 取消；无命中时标签全无、继续打字或 esc。按 sqlmux 的 internal/app/jump.go 做（字母表 asdfghjklqwertyuiopzxcvbnm → 大写、按离焦点距离分配、跳过匹配后紧跟字符）；design.md「全局定位」一节按此重写（T20 的「直接标签」作废：用户实测后拍板）。
- 折叠标题也参与（标题文字里命中）。dev 定稿（照准）：dir 只在组标题里找（卡片上看不到 cwd）；组标题命中的标签放在标题行，展开的组按标签跳到它的第一张卡、折叠的落到标题；消息行显示已打的内容；再次按 s 清空重来；改窗口大小重扫。打一个字符就出标签（flash.nvim 默认）。底栏 `flash: type to narrow · label to jump · esc`。
- 审查退回（reviewer @b4b5351，探针 cr-t23-b4b5351/probes/）：1. 轮询后命中和标签仍画在旧坐标、压到别的卡上——改为轮询后按新布局重扫，「卡 → 标签」保留（design.md 已写）；探针 TestProbeFlashStaleAfterPoll。2. 收 probe_t23b_test.go 三条：TestProbeFlashNameCutByTime（名字宽度扣右侧时间）、TestProbeFlashOpenTitleAfterHit（前卡命中后下一组标题仍搜）、TestProbeFlashTitleJustAbove（标题在窗口上一行不画到顶栏）。3. spec 定稿：每张卡名字 + 分支合起来一处命中一个标签（按现有代码），组标题单独一处。
- 验收：单测 标签避开后续字符（卡名 "alpha" "alps"，打 "al" 后标签不能是 p / s）；黑盒 `s` 打 `br` → 只有 bravo-* 卡有黄底 + 标签，按标签焦点到位；打到无命中时无标签、esc 退出。

### T24 预览里画出 pane 的光标（修复 29aa7ef 复审 / 测试中）
- 审查退回（reviewer @4e0985d，探针 cr-t24-4e0985d/probes/）：1. 必改：内容填满框、光标停在被裁掉的末尾空行（seq / tail -f 常态）时不画——有光标时末尾空行只裁到光标行为止（取 max(非空行数, cur.row+1) 再取最后 n 行，框少显示最上一行）；探针 TestProbeCursorBlankRowFullBox。2. tab 展开改按 8 列制表位（现在固定 4 格，光标列错位）；探针 TestProbeCursorAfterTab。3. 信息行认不出（旧 tmux 无 cursor_shape）也丢掉第一行、只是不画光标，不当屏幕内容。
- 用户 2026-10-12 验收：预览里看不到光标。能解决：抓屏时顺带 `tmux display -p -t <pane> '#{cursor_x} #{cursor_y} #{cursor_flag} #{cursor_shape}'`（tmux 3.7 有这些变量，已查 man），在预览对应格子上画光标：cursor_flag=0 不画；shape 块状 → 该格反色；竖线/下划线 → 该格加 focus 色下划线（终端里画不出细竖线，用下划线替代）。滚动（scroll 偏移非 0）时不画。预览和直通态都画，直通态随每次抓屏刷新。
- **先实测再写死**：dev 先用一个真 pane 验证 cursor_shape 的取值（0/1/2 还是文字）和 cursor_flag 在 vim 插入态 / Claude Code 输入框里的值，把结果写进 design.md 再实现。
- 验收：黑盒 真 pane 里跑 `printf 'abc'` 后预览第一行第 4 格反色；`tput civis` 后不画；滚动后不画。

### T25 最大列数可配置（f9f4ecf 审查 / 测试中）
- 用户 2026-10-12 验收：内容显示绰绰有余，想收窄到 5 列。现在 view.go `maxCols = 4` 写死。
- 做法：config.toml 新增 `[layout] max_columns = 4`（1–8，默认 4 不变；最小列宽 40 不动，终端够宽才会真到 5 列：5 列约需 ≥ 200 列）；`agmux config` 打印它；README 配置示例加一行、说明「宽度够时最多几列」。用户自己的 config 写 5。
- 验收：单测 232 列 max_columns=5 → 5 列、=4 → 4 列、180 列 =5 → 4 列（受最小列宽限制）；config 超范围 / 非整数报错一行；黑盒 232×50 夹具配 5 → 抓屏数到 5 张卡一行。

### T26 非焦点卡边框统一灰色（5c7ac6c 审查 / 测试中；只有 border 允许空串）
- 用户 2026-10-12 验收：边框色跟状态太花哨分散注意力，想全部灰色、只留 focus 粉边框，状态靠卡片里的状态行 / 头像神态区分。
- 做法：新增颜色项 `colors.border`，默认空 = 沿用现在「跟状态」；设了就所有非焦点卡的边框都用它（focus 边框仍是 `colors.focus`）。状态行文字（⏵ working / ● done / 告警）和头像神态不受影响，仍按状态着色。不动 working/idle/done/attention 四项的语义（它们还管状态行和头像）。README 颜色表加一行，示例给 `border = "#585b70"`。
- 验收：单测 border 设了 → working / done / alert 卡的边框转义码都是 border 色、focus 卡仍 focus 色、状态行仍是状态色；没设 → 和现在一样；黑盒 配置后抓屏带色核对。
