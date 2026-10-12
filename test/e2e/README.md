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
| `test_t3.py` | 列数与列宽（最小列宽 40：81 / 82 / 124 / 200 / 208 / 232 列等）、resize、卡片三行字段与让位顺序、agent 图标、边框色 = 状态色 / 焦点 #ff8ebe 粗线、6 格上下文条与阈值、模型缓存、`agmux statusline` 抓到的 model / 百分比显示到卡片、多行 stderr |
| `test_t4.py` | 每个键的命令行（send 正文在 `--` 之后）、x 确认、s 输入（含中文、`-` 开头）、粘贴、d 的 ExecProcess（回来后鼠标仍可用、失败时底栏显示 stderr 最后一行）、后台会话置灰（提示只出现一次，和错误行同时可见）、连按、鼠标、2s 轮询、会话消失的竞态、popup 里的真鼠标 |
| `test_t6.py` | `agmux statusline`（逐字节透传、退出码、sh -c、写失败不影响透传、session_id 白名单、原子写）和 `agmux setup`（文案、`--write` 保留其它键、`.bak`、损坏 / 无权限、软链接、端到端）；只用临时 HOME，收尾校验真 `~/.claude/settings.json` 哈希没变 |
| `test_t9.py` | 空间导航：120 列（2 列 3 带）下 j 走遍整列（跨带）再停住、k 原路回来、j/k 不退让；h/l 取纵向重叠最多的卡，没有重叠时退到最近一列里纵向最近的卡；方向键等价；折叠标题作为一行高的卡参与；焦点从屏幕上读（#ff8ebe 粗线边框），不用按 enter |
| `test_t7.py` | 固定排序（假 ccmux 每次轮询换数组顺序，卡片不动；同名按 id；组按 cwd 字符串）、`f` 折叠 / 展开（标题 ▾ ↔ ▸、其它组位置不动、折叠标题可获焦点且不响应 H/r/d/s/x、enter 展开、轮询不展开、不跨运行记忆） |

`internal/store/store_ext_test.go`、`internal/avatar/avatar_ext_test.go` 是同一轮测试写的补充 Go 测试（模型缓存、64KB 边界、并发刷新、avatars.json 读写与坏文件、sprite 渲染）。

## 夹具

`lib.build_fixture()` 造 9 个会话 / 5 个分组（按 cwd 排序）：a1 working + HUD 41% + 2 个子 agent、a2 名字回退到 project、a3 attentionType 非空 + 超长中文名、b1 原始 idle 但有子 agent 在跑、b2 background（无 pane）、c1 / c2 HUD 30% / 59% 和 done、d1 waiting + HUD 60%、e1 超长 cwd + attentionState read。改夹具要同时看 `NAMES` 和各用例里写死的期望。
