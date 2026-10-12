# M2 预览 + sidebar + 详情（备忘，未开工）

- 预览窗口：选中卡片后在旁边 / 下方看最近对话（`ccmux last` / `ccmux screen`），替代 M1 去掉的 prompt 行。用户验收 M1 时提出。
- sidebar：替换 `ccmux sidebar`，用卡片的紧凑变体（3 行）；M1 的卡片渲染函数已预留「高度」参数。
- 详情页：大头像、mission（lastPrompt / backgroundDetail）、心跳曲线、子 agent 小头像阵列、`.handoff/` 历史。
- 顶栏右侧放 5 小时限额（statusline payload 里有 rate_limits）。
