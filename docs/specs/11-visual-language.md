# 11 视觉语言 v2（VISUAL2）

目标：在保持 Explorer 密度（行高、字号、内边距不变）的前提下，提供可切换的 v2 视觉风格：Kero 式 mist 底色、浮起内容卡、侧栏选中 chip、蓝色强调；**v2 为默认**，经典（Windows 11 扁平）仍可在设置中切换。

## 令牌（`visual.go`）

| 常量 | 值 | 用途 |
| --- | --- | --- |
| `v2CardRadius` | **8** | 内容卡、任务卡 |
| `v2RowRadius` | **4** | 侧栏选中行、主题 `appTheme.Radius`（对话框组件加倍 → **8**） |
| `v2InputRadius` | **4** | 地址栏 / 搜索框（`fieldRadius()`） |
| `v2Gutter` | 8 | 内容卡外侧 wash 间距 |

经典：`appTheme.Radius = 6`（对话框 12），地址/搜索圆角 6。

## 行为

- `appSettings.VisualStyle`：`"v2"`（`defaultSettings` 默认）、`""` 经典。
- `syncVisual` → `resolveV2` 覆盖色板；根节点 mist 渐变；`contentPanes` 将文件区 / 预览 / 助手包进一张 `v2Card`。
- 设置 → 外观 → 视觉风格：`经典` / `v2`。

## 纵向对齐

图标、文字、徽章、按钮在各自行高内垂直居中；v2 下侧栏列表顶距收为 4，与内容卡顶齐；搜索图标与 24 DIP 输入同高居中；状态栏 / 任务卡进度条固定高度并 `AlignSelf(Center)`。

## 截图

`docs/assets/shots/VISUAL2/`：`files|tasks` × `classic|v2` × `light|dark`。
