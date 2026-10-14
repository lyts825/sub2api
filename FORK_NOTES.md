# Token 配额

本 fork 基于 `Wei-Shaw/sub2api` 的 `v0.2.16`（`4d9ac3e48`），当前版本 `0.4.0-token-quota.1`。

## 使用

在「API 密钥」页面创建或编辑 Key，可配置累计、每日、每周、每月和每分钟（TPM）Token 上限。整数 `0` 或留空表示该项不限。各项同时生效，任何一项耗尽都会拒绝新请求。列表和编辑弹窗显示用量与窗口重置时间。

周期按服务配置时区（默认 `Asia/Shanghai`）计算：每天零点、每周一零点、每月一日零点自动恢复；TPM 使用自然分钟窗口，不是滚动 60 秒。按结算时刻归属窗口；跨窗口等待锁的迟到结算不会使窗口退回，保守计入当前已推进的窗口。编辑弹窗的「重置 Token 用量」只清零 Key 累计量，不重置周期或用户共享用量。

管理员可在「用户管理 → 用户操作 → Token 总限额」配置用户累计额度，合并该用户所有 Key 的用量。普通用户只能查看共享额度，不能更改或重置；新建、删除 Key 或重置 Key 累计量不能绕过共享额度。管理员的独立清零只重置用户累计量。

已有 API 保持兼容，只增加以下字段：

| 接口 | 新增字段 |
| --- | --- |
| `POST /api/v1/keys` | `token_quota`：非负整数，默认 `0` |
| `PUT /api/v1/keys/:id` | `token_quota`：省略不变；`reset_token_quota: true`：清零累计量 |
| Key 创建／更新请求 | `token_limits: {daily, weekly, monthly, tpm}`：四项均为非负整数；更新时省略对象不变，提供对象则整体替换，未填写项为 `0` |
| Key 创建、查询、列表、更新响应 | `token_quota`、`token_quota_used`、`token_limits`、`token_usage`、`user_token_quota` |
| `GET /v1/usage` | 设置 Token 上限时返回 `token_limits`、`token_usage`、`user_token_quota`；设置 Key 累计上限时还返回 `token_quota: {limit, used, remaining, exhausted, unit: "tokens"}` |
| `GET /api/v1/admin/users/:id/token-quota` | 管理员查询 `{quota, used}` |
| `PUT /api/v1/admin/users/:id/token-quota` | 管理员更新 `{quota?: 非负整数, reset_used?: true}`，省略 quota 保留原值；响应 `{quota, used}` |

`token_usage` 中 daily/weekly/monthly/tpm 各返回 `{limit, used, resets_at}`；`user_token_quota` 返回 `{quota, used}`。

用量达到上限时，新请求返回 HTTP `429`。标准入口错误码为 `API_KEY_TOKEN_QUOTA_EXHAUSTED`、`API_KEY_TOKEN_DAILY_EXHAUSTED`、`API_KEY_TOKEN_WEEKLY_EXHAUSTED`、`API_KEY_TOKEN_MONTHLY_EXHAUSTED`、`API_KEY_TOKEN_TPM_EXCEEDED` 或 `USER_TOKEN_QUOTA_EXHAUSTED`；周期超限附 `Retry-After` 秒数。OpenAI 和 Gemini 入口沿用各自错误格式。Responses WebSocket 每轮检查，超限后关闭连接。用量查询仍可访问。

鉴权与排队后检查直接读取当前数据库计数，即使认证缓存中原本不限额，新设置的共享或周期额度也会生效。配额读取失败返回 `503`；每次检查增加一次按主键关联查询，列表采用批量读取。

## 计量与边界

- 沿用现有归一化用量：输入 + 输出 + 缓存写入 + 缓存读取 Token。OpenAI 缓存读取已从输入中分离，不重复计数；缓存写入子分类不再重复相加。价格与倍率不影响 Token 数，免费请求同样计数。
- 标准模式从相应版本升级后开始累计，未设置上限的 Key 也累计。简易模式保留原有不计费语义：配置任意 Key／用户 Token 上限或启用原有金额窗口时才累计，不补计此前不限额时的用量。上限设为 `0` 不清除既有累计量。
- 请求结束后按上游实际返回的用量结算。当前请求、已获准排队或执行的请求，以及计费任务尚未落库期间获准的请求可能使累计量超过上限；本功能不在生成过程中截断输出，也不预估预留 Token。
- 计数与现有计费请求去重共用数据库事务；清理用量日志不会清除计数。受限 Key 的计费任务不会因工作队列采用 drop/sample 策略而直接丢弃，但计费持久化故障仍沿用上游错误处理，需检查服务日志。
- 无 Token 用量报告的图像、视频、按音频时长计费请求不会虚构 Token。上游尚未实现 Token 计费的 Live/Realtime 会话不具备完整 Token 配额计量能力；需要限制此类请求时仍应使用原有金额或其他限额。
- 用户可管理自己 Key 的上限和累计量重置，与现有金额配额权限一致；需要强制所有 Key 的总上限时使用管理员配置的用户共享额度。

## 部署与兼容

按原项目流程同时构建前后端。`0.3.0-token-quota.1` 的迁移 `246_add_api_key_token_quota.sql` 为 `api_keys` 增加累计上限和累计量。此版启动时自动执行 `247_add_token_limit_windows.sql`，新增 `api_key_token_windows` 与 `user_token_quotas` 两个小表，全部限额默认 `0`，不改原有金额计费字段。

升级保留已有 Key 累计计数，新增周期和用户共享计数从此版升级后开始；不扫描或回填可能已清理的历史日志。无需额外后台重置任务，无新增依赖。

多实例部署应将全部实例升级到此版本；旧实例不执行 Token 配额检查或计数。回退旧二进制时新字段可保留，但 Token 限额不再生效。认证快照版本已升级，避免复用缺失限额字段的旧缓存。

## 版本记录

### 0.4.0-token-quota.1 — 2026-10-14

- 从 `0.3.0-token-quota.1` 兼容新增每 Key 日／周／月／TPM 限额和管理员用户共享累计额度，保留原有累计与金额配额行为。
- 配置及计数复用原鉴权、计费去重事务和前端表单；新增计数不会被改名、改限额或删除 Key 覆盖。WebSocket 两种接入模式逐轮刷新所有额度，排队后新增额度的计费任务也不会因 drop/sample 队列策略而直接丢弃。
- 验证通过：前端类型检查、定向 ESLint、75 项既有测试与 15 项新增本地测试；后端定向 service/handler/middleware/admin 回归（含缓存中原本不限额后动态配置的 30 个入口／模式／额度组合、WebSocket 两种模式、夏令时窗口及队列溢出）；真实 PostgreSQL 18.6 的迁移、CRUD、并发跨 Key 累计、去重、窗口切换及事务失败回滚。
- 前端生产构建及嵌入前端的后端构建通过；本地二进制版本输出为 `0.4.0-token-quota.1`。前端构建仍有依赖工具弃用、Browserslist 数据过期、混合导入及较大分块提示，不阻断构建。
- 验证边界：真实本地数据库配合模拟上游，未使用真实供应商账户或生产部署，未运行全仓无关测试。计量仍为请求完成后结算，已放行请求可能超额，Live/Realtime 边界同上。
- 对应提交由本地标签 `v0.4.0-token-quota.1` 定位；源码推送 fork，标签不推送以避免自动发布。新增测试材料精确忽略并保留本地。

### 0.3.0-token-quota.1 — 2026-10-13

- 兼容新增每 Key 累计 Token 上限、展示、独立重置、入口拦截和持久化计数。
- 复用原计费、鉴权、迁移、前端表单；未引入运行时依赖。
- 验证通过：前端类型检查、定向 ESLint、相关 45 项测试、生产构建；后端构建与相关 service/middleware/handler 回归；Responses WebSocket 两种模式的限额与动态启用场景；PostgreSQL 18.6 空库完整迁移、APIKey 仓库完整测试及 7 项既有计费事务回归。数据库验证覆盖去重、16 并发累计、免费用量、重置、显式更新字段和失败回滚。
- 验证边界：使用真实本地 PostgreSQL 与模拟上游响应；未连接真实供应商账户，未部署到生产。未运行全仓无关测试。
- 对应 Git 提交由本地标签 `v0.3.0-token-quota.1` 定位；标签不推送，以免触发上游自带的 Release 发布流程。源代码推送到 fork，不表示已发布二进制或部署服务。
- 新增测试材料仅保留本地并精确忽略；上游原有测试由 CI 引用，保留原跟踪避免破坏原验证入口。
