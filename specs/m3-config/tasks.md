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

### T19 `/` 模糊搜索过滤（修复 d27522d 复审中 / 回归中）
- 按 design.md「模糊搜索过滤」：fzf algo 封装在 internal/fuzzy（固定版本）、扩展语法、三字段高亮、实时过滤、`3/14` 计数、enter 保留 / esc 清空、no match、ctrl+h 退格。
- 验收：单测 `orch` 命中 orchestrator、`^sql` 只命中前缀、`!test` 排除、`'main` 精确、smartcase；高亮位置按字素簇；过滤后布局只含命中卡片、组数变化；黑盒 fixture 输入 `orch` 只剩 orchestrator 卡，esc 恢复 9 张。
- 定稿（2026-10-12 tester 退回「折叠 + 空模式计数 9/9 但屏上 6 张」）：计数是命中数/会话总数，不是屏上卡数；空模式不过滤、折叠照旧，9/9 正确，不改代码，tester 改断言。

### T20 `s` 全局定位 flash + 回复改 `i`（复审通过 @00fcdab+b6b75e2；dev 收 3 条补测；回归中）
- 按 design.md「全局定位」：标签按离焦点距离分配、小写后大写、背景 dim、按标签移焦点、esc 取消、非标签字符忽略；`reply` 默认键改 `i`，底栏与 README 同步。
- 验收：单测 9 张卡的标签顺序按距离（焦点卡自己是 a）、30 张卡进入大写；黑盒 按 s 后每张卡左上角有标签，按 `d` 焦点到第 3 张（粉边框），esc 取消。

## 遗留（不阻塞验收）
- 宽度 1 的终端按 j / s 会 panic（card() 里 strings.Repeat 负数）；终端本来画不下卡片，M3 审查发现，未修。
- 高度 ≤ 7 时焦点卡比窗口高，flash 进入后零标签只能 esc；无害，未修。

## 后续迭代（用户 2026-10-12 验收前提出，M2+M3 整批收口后再开）

### T21 预览进出都用 `tab`（待做）
- 用户原话：ctrl+] 退出预览焦点不顺手。改成：`tab` = 打开预览（若没开）并进入直通；直通里 `tab` = 回到卡片，预览**不关**；`p` 仍是开/关预览。
- 落地：默认键 `focus_preview = tab`（没开预览时先开再进）、`leave_preview = tab`（原 ctrl+] 不再默认绑，用户可在 config.toml 绑回来）；底栏文案、README、m2-preview/design.md「按键直通」一节同步改（design.md 里「否掉 tab」的理由作废：用户实测后拍板）。
- 代价（已知，用户决定）：直通里 `tab` 到不了 pane——Claude Code 的路径补全、shell 补全要在真终端里按；`shift+tab` 照常直通。
- 验收：黑盒 预览关着按 tab → 预览开 + 边框粉 + 底栏 "keys go to the pane · tab back to the cards"；再按 tab → 边框普通色、预览还在、底栏回卡片提示；p 关预览；直通里 shift+tab 仍 send-keys 到 pane；config `leave_preview=["ctrl+]"]` 时 ctrl+] 也能退。
