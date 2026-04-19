## Why

当前项目已经具备图片上传到对象存储、导入确认、时间线/地点浏览和资产详情查看的最小骨架，但关键链路仍停留在“进程内演示态”：导入数据与队列状态默认保存在内存中，Worker 不消费任务，Web 端缺少大文件分片上传闭环，Provider 设置更新接口也未实现。结果是服务重启后上传记录与处理状态会丢失，真实 COS 环境下难以稳定完成“上传成功后立即可查看，并在后台持续增强”的完整流程。

现在需要把这条主链路从本地能力验证提升为可重复验证的端到端流程，确保用户可以在真实 COS 配置下完成上传、确认入库、触发异步处理，并在 Web 端稳定查看新上传的图片。

## What Changes

- 将导入会话、资产记录、对象引用和查看所需元数据从内存仓储切换到 PostgreSQL 持久化实现，消除服务重启导致的状态丢失。
- 将增强任务从 API 进程内存队列切换为 Redis 驱动队列，并让独立 Worker 实际消费任务，完成上传后的异步处理闭环。
- 补齐 Provider 设置更新接口，支持更新 COS 相关配置并执行必要的连通性与写入校验。
- 完成 Web 导入页的大文件分片上传流程，使浏览器可以基于后端票据执行 multipart 上传并完成确认。
- 收紧上传确认与查看链路，确保上传成功的资产能够稳定出现在总览、时间线和资产详情页面，并具备可验证的端到端测试。

## Capabilities

### New Capabilities

- 无

### Modified Capabilities

- `photo-ingestion`: 调整导入与上传需求，要求导入会话、资产与对象引用持久化到 PostgreSQL，并支持 Web 端对 COS 的分片上传与确认闭环。
- `photo-enrichment`: 调整异步处理需求，要求增强任务进入 Redis 队列并由独立 Worker 消费，而不是停留在 API 进程内存中。
- `photo-discovery`: 调整发现需求，要求新上传并确认的图片在持久化后可稳定出现在时间线、总览和资产详情查看流程中。
- `provider-adapters`: 调整 Provider 配置需求，要求支持通过设置接口更新 COS Provider 配置并验证其可用性。

## Impact

- 受影响代码包括 `internal/ingestion`、`internal/enrichment`、`internal/discovery`、`internal/platform/httpserver`、`cmd/api`、`cmd/worker`、`apps/web/pages/import.vue` 以及与 PostgreSQL/Redis/COS 相关的配置装配逻辑。
- 受影响 API 包括导入会话、上传票据、上传确认、资产列表/详情、Provider 设置更新，以及与异步处理状态相关的返回行为。
- 需要补齐 PostgreSQL 运行时接线、Redis 队列接线、Worker 消费循环与端到端测试基线，并验证在真实 Go 运行环境中能够稳定走通上传到 COS 再查看的主流程。
