# M3 agmux mod（备忘，未开工）

Claude Code 插件（hooks API，2.1.296 只读过类型声明，没真机验证）：
- `$.session.usage()` 直读 context / rateLimits / cost，替掉 M1 的 `agmux statusline` 透传。
- 监听 `tool.call` 识别 Bash run_in_background 和 Agent 调用，统计后台 shell 和 subagent（M1 的 Shells 固定 0）。
- `session.receive` 拦截 peer 消息并 `{consumed}`，`$.command.run({command:'handoff'})` 触发交接；上报 handoff mod 的状态（已推迟交接等）。
- 参考：用户的 handoff mod ~/.config/claude/skills/handoff/hooks/register.tsx（60% 横幅、70% 自动 /handoff force）。
- 风险：/tmp/cc-socks 的 socket 协议是 Claude Code 内部的、无文档，升级可能变；所以 M1 交接走 `ccmux send <id> -- /handoff`。
