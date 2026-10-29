# 22 — 远程管理与 MCP 端到端回归

## 界面

- 插件设置显示远程服务的「运行中 / 正在启动 / 已停止」状态，并提供「连接 / 停止」。
- 助手设置的 MCP 区显示进程内与远程端状态；远程端提供地址复制和「测试」。
- 测试在后台连接 Streamable HTTP，结果回到 UI 线程；错误文字可选中复制。

## MCP（真实 SDK + HTTP 回环）

`TestAssistantMCPStreamableEndToEnd` 使用 `github.com/modelcontextprotocol/go-sdk/mcp` 的 `StreamableClientTransport` 连接真实 HTTP 监听器。

| 工具 | 参数 / 副作用控制 | 结果 |
| --- | --- | --- |
| `assistant_config_list` | 空参数，临时配置目录 | 通过 |
| `assistant_config_save` | 临时供应商、测试占位密钥 | 通过 |
| `assistant_config_activate` | 激活临时供应商 | 通过 |
| `assistant_config_delete` | 删除临时供应商 | 通过 |
| `assistant_status` | 空参数 | 通过 |
| `a2a_info` | 空参数 | 通过 |
| `assistant_run` | 临时根目录，模型执行器替身 | 通过 |

测试先执行 `ListTools`，断言服务公布全部 7 个控制工具，再逐个调用。应用内文件 MCP 的注册、参数与文件操作继续由 `TestSunvaleMCPReachesAnyPath`、schedule MCP 测试覆盖。

## OpenList 兼容远程服务（真实 HTTP 回环）

`TestRemoteOpenListEndToEnd` 使用随机本地端口和 `net/http.Client`，所有文件位于临时 HOME。

| 操作 | API | 结果 |
| --- | --- | --- |
| 登录 | `POST /api/auth/login` | 通过 |
| 列目录 | `POST /api/fs/list` | 通过 |
| 上传 | `PUT /api/fs/put` | 通过 |
| 下载 | `POST /api/fs/get` 后访问签名地址 | 通过，正文一致 |
| 重命名 | `POST /api/fs/rename` | 通过 |
| 删除 | `POST /api/fs/remove` | 通过，磁盘文件消失 |

## 真实助手

`TestAssistantLiveToolCall` 仅在 `SUNVALE_LIVE=1` 时运行，读取当前已激活供应商，要求模型先调用 `list_dir`；测试只记录是否发生工具调用，不输出供应商密钥。普通 CI 自动跳过。
