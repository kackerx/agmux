# M6 找回会话（recall）+ 键位全可配 + 中英文（设计，2026-10-28 用户点头 MVP）

设计稿（有界面示意图）：https://claude.ai/artifact/4gZTbgirVFDqivKBkNXUos 第 2 版。本文是给 dev / tester 的定稿，与设计稿冲突以本文为准。

## 1. 要解决的事
用户经常关掉会话，过段时间想找回，只记得聊过什么，不记得哪个 worktree / 哪个 session。现有 tv `ccs` channel（~/.config/television/cable/ccs.toml + ~/.config/bin/cc-tv）一行一句、只搜用户消息、不知活没活着、和看板割裂。

## 2. 体验（已定）
- 一行 = 一个会话；片段 = 最匹配的那句 + 命中数；预览展开全部命中。
- 你说的和 agent 说的都搜（agent 只取每段回复首 200 字）；你说的同分优先。
- 两个入口一套界面：`agmux -r [关键词]`（= `agmux recall`，任何终端，不要 tmux）；看板里动作 `recall`（默认 `?`）。
- resume 原地起：`agmux -r` 里 enter → agmux 退出，同一终端 `cd <cwd> && exec claude --resume <id>` / `pi --session <文件>`。在跑的会话 enter 不动，底栏说它在跑（看板里 enter 跳 pane）。目录没了 enter 不动，底栏说原因。
- 看板里 enter 关了的会话：看板跑在普通 pane → 同上原地 exec（看板退出）；popup / --stay → 在会话 cwd 开 tmux 新窗口跑 resume 命令（T60 的 new-window 代码）再跳过去。
- 空查询 = 最近的会话（按 transcript mtime），片段是最后一句。
- 筛选面板（`recall_filter`，默认 ctrl+f）：来源多选（只列本机有会话的来源，默认全选）+ 范围单选（当前项目 / 全部历史，默认全部）。面板键：上下 = `[keys] down/up`，勾选 `recall_toggle`（space），全选 `recall_all`（a），清空 `recall_none`（A），esc 收起。`recall_scope`（ctrl+t）不开面板直接切范围。勾选不记住（每次打开全选 + 全部）。
- `recall_copy`（ctrl+y）复制 `cd '<cwd>' && claude --resume <id>` 整条；`recall_open`（ctrl+o）`$EDITOR` 开 transcript。
- 窄于 100 列不画预览。fork（alt+enter）v1 不做。

## 3. 来源接口
`internal/recall`：
```go
type Source interface {
    Name() string                      // "claude" | "pi"
    Files() ([]string, error)          // 本机全部 transcript 路径
    Read(path string, from int64) (Head, []Line, int64, error) // 从偏移读到末尾：会话头（可能部分更新）、新句子、新偏移
    Resume(h Head) []string            // argv：claude --resume <id> / pi --session <file>
}
type Head struct{ ID, CWD, Name, Branch, Agent string; Live bool }
type Line struct{ Role byte /* '>' 用户 '<' agent */; At time.Time; Text string }
```
- Claude：`~/.claude/projects/*/*.jsonl`（`$CLAUDE_CONFIG_DIR` 优先）；头：cwd、customTitle / aiTitle 最后一次、agent-name、gitBranch（store 现成读法）；句：type=user 非 isMeta 的文本块、type=assistant 的 text 块首 200 字。
- pi：`~/.pi/agent/sessions/*/*.jsonl`（`$PI_CODING_AGENT_DIR`）；头：首行 type=session 的 cwd / id，名字 = 最后一条 session_info.name，没有用第一句；句：type=message 的 role user / assistant 的 text 块。
- codex / opencode：不实现，接口即扩展点。
- 在跑的：和看板同一份 store 会话表按 id 合并（Live、pane、状态）。

## 4. 索引
- `$XDG_CACHE_HOME/agmux/recall/<来源>-<id>.json`：来源、id、path、cwd、名字、分支、mtime、size、offset、lines[]（≤300 句你的 + ≤300 句 agent 的，每句 ≤200 字，去重），`v` 版本号（格式变了整体重建）。原子写。
- 打开：stat 全部文件；mtime / size 变了从 offset 续读；size < offset 整读；文件没了删索引。
- 首次：后台协程按 mtime 新→旧建，边建边出，顶栏 `索引中 n / N`；`agmux -r --reindex` 清缓存重建。
- 噪音：`This session is being continued`、`<local-command`、`<command-name`、`<task-notification`、`<system-reminder`、`[Request interrupted`、工具结果、<4 字。`[Image #n]` 留。
- 匹配：全内存，`internal/fuzzy`；每句匹配串 = `来源 项目 分支 会话名 句子`；会话分 = 最高句分，用户句优先，再 mtime。筛选先过滤再匹配。实测每次按键 > 50 ms 才加防抖。

## 5. 键位全可配（用户 2026-10-28）
看板和找回里每个按键都在 `[keys]` 下，`agmux config` 模板列全。找回新增动作：`recall`（?）、`recall_filter`（ctrl+f）、`recall_scope`（ctrl+t）、`recall_copy`（ctrl+y）、`recall_open`（ctrl+o）、`recall_toggle`（space）、`recall_all`（a）、`recall_none`（A）；上下 / 翻预览 / esc 复用现有 `down` `up` `preview_down` `preview_up` `quit`。输入框打字状态下只有 ctrl / alt 组合和方向键算动作，字母是字符（同 `/`）。核：看板现有键是否有硬编码没走 `[keys]` 的（如选择框里的 a、预览滚轮、ctrl+d/u、n/N、g 循环），一并收进 `[keys]`。

## 6. 中英文（用户 2026-10-28）
`[ui] language = "en"`（`en` | `zh`，默认 en；`$AGMUX_LANG` 可覆盖，便于测试）。`internal/i18n`：`T(key string, args ...any) string`，两张表（en / zh）同一组 key，缺 zh 回落 en，缺 key 返回 key 本身并在测试里断言两表 key 集合相等。范围：看板和找回里用户看到的全部文案（底栏、消息行、预览标题、选择框、提示、找回的列头 / 顶栏 / 面板 / 底栏），`agmux setup` 的输出。不翻：`agmux config` 模板注释、`agmux show` 列、日志 / stderr 错误、README。宽度：zh 文案按显示宽度算（现有 ansi.StringWidth），底栏截断逻辑不变。

## 7. 不做
会话链合并、assistant 全文 / thinking / 工具输出、删会话 / 改名 / 时间范围、窄屏全屏预览、codex / opencode、fork、记住筛选。tv channel 先留。
