# M2 预览 + sidebar + 详情（备忘，未开工）

- 预览窗口：选中卡片后在旁边 / 下方看最近对话（`ccmux last` / `ccmux screen`），替代 M1 去掉的 prompt 行。用户验收 M1 时提出。
- sidebar：替换 `ccmux sidebar`，用卡片的紧凑变体（3 行）；M1 的卡片渲染函数已预留「高度」参数。
- 详情页：大头像、mission（lastPrompt / backgroundDetail）、心跳曲线、子 agent 小头像阵列、`.handoff/` 历史。
- 顶栏右侧放 5 小时限额（statusline payload 里有 rate_limits）。

## 预览窗口方案（2026-10-12 提议，等用户定）
- 位置：**右侧**（和 ccmux 的 P preview 一致）。理由：会话内容是竖长的，吃高度不吃宽度；卡片区本来就按宽度自适应，预览一开列数自动从 4 降到 2–3，不用另写布局；放下侧会压掉卡片的行数，看板没有竖向滚动，组一多就看不全。
- 尺寸：预览占终端宽度 45%，最少 60 列；`P` 开关，开着时卡片区按剩余宽度重排（现有列数算法直接用）。
- 内容：`ccmux screen <id>` 的 pane 抓屏（所见即所得、不分 agent 类型），跟着 2s 轮询刷新；background 会话没有 pane，用 `ccmux last <id>` 的最后一条回复。焦点换卡片，预览跟着换。
- 回复：预览开着时按 `s`（和看板同一个键）在预览底部出一行输入框，enter → `ccmux send <id> -- <text>`，发完预览继续刷新就能看到回复；esc 关输入框。不做多行编辑，多行走 `ccmux send --stdin` 以后再说。
- 键位：`P` 开关预览；预览里 `J`/`K` 滚动抓屏历史（`ccmux screen` 支持多少行就滚多少）；其它键和看板一样作用于焦点卡片。
