# agmux 黑盒 e2e

在真实终端（tmux）里跑 `agmux board` / `agmux show` / `agmux avatars`，按键、鼠标、resize、宽字符、配置加载都走真的；`ccmux` 用 PATH 上的假脚本代替，**绝不会碰真的 ccmux 或你的 tmux**。

## 跑法

```sh
go build -o /tmp/agmux . && cd test/e2e
AGMUX_BIN=/tmp/agmux python3 -m unittest -v                 # 全部，约 2 分钟
AGMUX_BIN=/tmp/agmux python3 -m unittest -v test_t4.Keys    # 只跑一组
```

- 需要 `tmux`（3.x）和 `python3`（3.10+，只用标准库）。`AGMUX_BIN` 缺省为 `~/go/bin/agmux`。
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
| `test_t7.py` | 固定排序（假 ccmux 每次轮询换数组顺序，卡片不动；同名按 id；组按 cwd 字符串）、`f` 折叠 / 展开（标题 ▾ ↔ ▸、其它组位置不动、折叠标题可获焦点且不响应 H/r/d/s/x、enter 展开、轮询不展开、不跨运行记忆） |

`fixtures/catv.py` 是假 pane 里用的原始模式回显（BSD `cat -v` 会把被拆开的 UTF-8 弄乱码）；`fixtures/paste_probe.py` 是要求 bracketed paste 并回显收到字节的 pane。

`internal/store/store_ext_test.go`、`internal/avatar/avatar_ext_test.go` 是同一轮测试写的补充 Go 测试（模型缓存、64KB 边界、并发刷新、avatars.json 读写与坏文件、sprite 渲染）。

## 夹具

`lib.build_fixture()` 造 9 个会话 / 5 个分组（按 cwd 排序）：a1 working + HUD 41% + 2 个子 agent、a2 名字回退到 project、a3 attentionType 非空 + 超长中文名、b1 原始 idle 但有子 agent 在跑、b2 background（无 pane）、c1 / c2 HUD 30% / 59% 和 done、d1 waiting + HUD 60%、e1 超长 cwd + attentionState read。改夹具要同时看 `NAMES` 和各用例里写死的期望。
