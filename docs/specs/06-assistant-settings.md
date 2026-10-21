# 06 — 助手配置迁入系统设置（A4）

## 目标

把助手供应商、附加提示词、Skills、MCP 说明、全托管默认值、记忆与解压密码本从右侧助手窗格迁入系统「设置」对话框的 **助手** 分类，避免在对话区堆完整表单。

## 行为摘要

| 分组 | 内容 |
| --- | --- |
| 模型与密钥 | 供应商增删改与激活；保存后不再回显 API Key |
| 系统提示词 | 内置 Instruction 只读说明；可编辑「附加提示词」→ `Prefs.ExtraInstruction` |
| Skills | `files` / `memory` / `archive` 开关 → `Prefs.DisabledSkills` |
| MCP | 只读：进程内 `sunvale` 工具名；远程访问启用时显示 `/mcp` 地址 |
| 行为与记忆 | 默认全托管、启用记忆、清空记忆、历史路径 |
| 解压密码本 | 从助手抽屉迁入 |

运行时：`EffectiveInstruction(extra) = Instruction + "\n\n" + extra`（extra 空则等于内置）。`memory` 关闭时不注入记忆、A2A 卡片去掉 memory skill；`files` 关闭时去掉 files skill。

窗格保留会话历史、模型选择与全托管 chip；顶栏「助手设置」打开设置并选中助手分类。
