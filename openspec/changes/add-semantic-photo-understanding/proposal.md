## Why

当前项目中的 OCR、caption 和分类能力主要仍停留在基于文件名生成占位结果的阶段，无法真正从图片内容中识别文字、人物、场景、动作与地点语义，也就无法支撑“搜海边合照”“找单人女生照片”“看坐在车上的照片”这类真实的照片整理与检索需求。

现在需要把识别增强链路从“文件名驱动的演示态”推进到“图片内容驱动的可用态”，让照片库真正具备基于画面内容的 OCR、语义查询和结构化分类能力，并把这些结果稳定映射到搜索、浏览与状态展示中。

## What Changes

- 将 `openai-compatible` AI Provider 从占位实现扩展为真实的图片理解调用路径，使 OCR 和图片内容分析不再依赖文件名推断。
- 扩展识别增强结果模型，在现有 caption、OCR、tags 基础上补充人物属性、人数形态、场景地点、动作状态等可归一化的图片内容分类信号。
- 明确结构化分类与自然语言检索之间的关系，让“男生/女生”“单人/合照”“海边/山上/城市”“走路/坐车”等语义能够进入搜索文档、标签与后续过滤入口。
- 调整识别阶段与状态说明，区分“仅基础 OCR 已完成”和“内容理解与分类已可搜索”的用户可见状态，避免把半成品结果误判为完整识别。
- 为发现接口补充语义分类结果的最小暴露契约，确保列表、详情与搜索接口在遵守隐私策略的前提下返回足够的内容理解摘要。
- 为 Provider 适配层补充真实远程视觉模型调用、结果归一化、错误分类和调试保留要求，确保从图片到识别结果的链路可验证、可回退、可审计。

## Capabilities

### New Capabilities

- 无

### Modified Capabilities

- `provider-adapters`: AI Provider 路由需要支持真实图片 OCR/视觉理解调用、结果归一化和远程 Provider 失败处理。
- `photo-enrichment`: 识别增强需要从图片内容中提取真实 OCR、caption 与结构化分类结果，并把这些结果持久化为可搜索元数据。
- `photo-discovery`: 搜索、详情与浏览能力需要消费新的语义分类结果，支持基于内容、场景、人数与动作的查询和呈现。

## Impact

- 主要影响 `internal/provider/ai`、`cmd/worker`、`internal/platform/httpserver/server.go` 中的 AI Provider 构建与调用逻辑。
- 主要影响 `internal/enrichment`、`internal/discovery`、`internal/asset` 与 PostgreSQL 持久化层中识别结果归一化、搜索文档生成和详情返回结构。
- 可能影响 OpenAPI schema、`apps/web/pages/index.vue` 与相关 UI workflow 文案，以展示更细粒度的识别与分类结果。
- 需要新增或调整与远程视觉模型调用相关的测试、隐私策略约束和调试保留边界，但不要求改变当前导入或备份主链路。
