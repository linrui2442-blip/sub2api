# Sub2API Personal Private Edition

面向 Windows 的私有 LLM 网关，服务于一个 Owner 和少量可信成员。它是长期运行 AI
Agent 的基础设施，不是公开 SaaS，也不是聊天玩具。

## 保留能力

- GPT/OpenAI、Gemini Provider，以及 Claude/Anthropic 等未来 Provider 的扩展边界。
- Provider OAuth、Token 持久化与自动刷新。
- Account Pool、健康检查、配额状态、Scheduler、冷却与 Failover。
- OpenAI 兼容 API Gateway 与协议转换。
- Owner 管理的成员、Group、权限、API Key、Usage 与 Audit。
- SQLite 与进程内本地缓存；Personal EXE 不依赖 Docker、PostgreSQL、Redis 或 WSL。

## 功能边界

公开注册、社交登录、Tenant/Organization、支付、订阅、充值、推广返佣、市场、云备份和
服务器部署栈已从 Personal Edition 移除。默认只监听 `127.0.0.1`；Owner 可以显式配置
可信 LAN/VPN 地址。

## Windows 快速开始

从 Personal Edition Release 下载 `sub2api-personal-windows-x64.zip`，解压后运行
`sub2api-personal.exe`。浏览器会打开本地初始化页面；创建 Owner 后，在控制台添加
Provider 账号和 API Key。

默认数据目录为 `%LOCALAPPDATA%\Sub2 Personal`：

- `sub2api-personal.db`：SQLite 数据库
- 本地日志和运行配置

可选覆盖变量：`SUB2_PERSONAL_DATA_DIR`、`SUB2_PERSONAL_SQLITE_PATH`、
`SERVER_HOST`、`SERVER_PORT`。

除非上游服务条款明确允许，Provider 账号应保持 `owner_only`。成员请求仍按各自 API Key
记录 Usage 与 Audit。

详见 [Personal Edition V1](docs/PERSONAL_EDITION_V1.md)。
