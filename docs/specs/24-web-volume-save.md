# 24 图片快存：音量键 / 方向键保存与取消（VOLSAVE）

看图时一键把当前图片复制到一个固定的「快存目录」，再按一次反向键取消。网页文件浏览器（远程访问 `/`）与桌面快速预览共用同一套规则。

## 1. 可行性调研

| 平台 | 网页能否收到音量键 | 结论 |
| --- | --- | --- |
| Android Chrome（标签页 / 已安装 PWA） | 否。Chromium 在 Android 上不把 `KEYCODE_VOLUME_UP/DOWN` 交给网页，`keyboard_code_conversion_android.cc` 只映射了静音键；Firefox 提交 bb54a58（Bug 1947131）注明 “Chrome and Firefox don't dispatch volume up and down to web content”。PWA 与标签页同一内核，独立窗口不改变按键分发。 | 物理音量键不可用 |
| Firefox Android（GeckoView） | 否。`shouldProcessKey` 明确跳过 VOLUME_UP/DOWN/MUTE。 | 不可用 |
| iOS Safari / 主屏幕网页 App | 否。没有按键事件；`HTMLMediaElement.volume` 在 iOS 只读且恒为 1（Apple 文档），`volumechange` 无法感知硬件键；只有原生 App 通过 `AVAudioSession.outputVolume` KVO 才能做到。 | 不可用 |
| Media Session API | 动作只有 play/pause/seek/track 等，没有音量。 | 不可用 |
| 桌面 Chrome / 蓝牙键盘 | 键盘上的媒体音量键可能以 `AudioVolumeUp/Down`（keyCode 175/174）送达；方向键一定送达。 | 可用 |

所以手机上真正可靠的是屏幕按钮；页面仍监听 `AudioVolumeUp/Down`（及 Firefox 旧值 `VolumeUp/Down`、keyCode 175/174/183/182），浏览器若将来分发即可直接生效。

### PWA

- 加了 `/manifest.webmanifest`（name、`display: standalone`、192/512 PNG 图标，图标由服务端绘制，仓库不带二进制）和 iOS 的 `apple-touch-icon`。
- 不加 Service Worker：Chrome 108（Android）/112（桌面）起菜单安装不再要求 fetch 处理器；且远程访问是局域网 `http://`，不是安全上下文，SW 本来就注册不了。Chrome 安装 PWA 需要 HTTPS，局域网 http 只能「添加到主屏幕」快捷方式；iOS 可加到主屏幕。两种方式都收不到音量键（见上表）。
- 不缓存任何文件数据。

## 2. 行为

- **保存**：网页预览图片时按 ↑ / 音量+ / 屏幕「↑存」；桌面快速预览图片时按 ↑。复制到快存目录，重名追加 ` (1)`、` (2)`…，提示「已保存」。
- **去重**：同一源文件 1 分钟内再保存不复制，只提示「已保存过」（服务端按源路径记录，网页与桌面共用）。
- **取消**：↓ / 音量− / 「↓撤」删除上一次保存产生的副本，提示「已取消」；取消后同一图片可立即再存。
- **安全**：只删除本次运行快存创建、且大小与修改时间未变的那个文件；没有记录或已变更时拒绝（「没有可取消的保存」/「副本已变更，未删除」）。
- **快存目录**：`UserConfigDir/sunvale/volsave.json`，网页与桌面共用。网页：顶栏「快存」输入远程路径；桌面：设置 ▸ 视图 ▸ 图片快存 ▸「设为当前文件夹」。
- **方向键冲突**：网页预览原先只用 Esc；桌面快速预览 ←/→ 切换图片，↑/↓ 原来没有绑定。

## 3. 接口（需管理员登录）

- `GET/POST /api/sunvale/volsave/target {dir}`：读 / 设快存目录（远程路径，必须在可访问根内且可写）。
- `POST /api/sunvale/volsave {path}`：保存 → `{name, dir, dup}`。目录不在远程根内时 403。
- `POST /api/sunvale/volsave/undo`：取消上一次保存。

## 4. 验收

- `remote_volsave_test.go`：未登录 401、未设目录、越界目录、保存、重名、59 秒内去重、61 秒后再存、取消、重复取消拒绝、取消后可再存、副本被改后拒绝删除。
- `volsave_ui_test.go`：桌面快速预览 ↑ 已保存 → ↑ 已保存过 → ↓ 已取消。
- 无头 Chrome（Pixel 5 仿真）通过 CDP 发送 `AudioVolumeUp/Down` 与 ArrowUp/Down，文件出现 / 消失。合成按键不代表真机会分发音量键。
