## Context

当前项目已经有一条完整的照片增强骨架：上传确认后进入 Redis 队列，Worker 依次执行 metadata、caption、OCR、embedding、indexing，并把结果落到资产记录与发现接口中。问题不在于“没有增强阶段”，而在于增强结果仍以演示性质为主：`openai-compatible` 配置存在，但运行时实际上统一构造成 `DeterministicProvider`，OCR、caption 和部分分类信号主要来自文件名分词而非图片内容本身。

这导致两个直接后果：

- 用户看到的 OCR、描述和分类结果并不可靠，无法支撑基于内容的查询。
- 当前搜索与整理能力虽然已经依赖 `caption_text`、`ocr_text`、`tags`、`search_document`，但这些字段还没有稳定承接“人物属性、人数形态、场景地点、动作状态”等真实视觉语义。

这次设计需要在不推翻现有 ingestion / enrichment / discovery 主链路的前提下，把“图片内容理解”真正落到现有 Provider 契约、归一化结果模型和发现接口里。

## Goals / Non-Goals

**Goals:**

- 让 `openai-compatible` Provider 真正调用远程视觉模型，基于图片 URL 完成 OCR 与内容描述，而不是基于文件名生成占位结果。
- 在现有 `caption`、`ocr`、`tags`、`search_document` 模型上，引入一层可归一化的语义分类结果，覆盖人数、性别表达、场景地点、动作状态等图片内容信号。
- 让这些信号稳定进入搜索文档、标签和发现接口，从而支持“男生/女生”“单人/合照”“海边/山上/城市”“路边行走/坐在车上”等查询。
- 保持当前 Worker 状态机、隐私策略、调试保留和 discovery API 的整体形态不被大幅重写。

**Non-Goals:**

- 本次不引入人脸识别、人脸聚类、身份识别或生物特征库。
- 本次不追求一套复杂的视觉 ontology，也不要求引入独立的图数据库或搜索引擎。
- 本次不把所有 AI 能力都切换到真实远程 Provider；优先保证 OCR、caption 和图片内容分类落地，embedding 是否继续远程化可保持兼容扩展位。
- 本次不重新设计照片库前端的完整 UI，只要求前后端契约足以承接语义结果的展示与检索。

## Decisions

### 决策：新增真实的 `openai-compatible` Provider，并把构建逻辑从“统一 deterministic”改为按 `kind` 分支

为什么这样做：

当前最大的落差不是 enrichment 状态机不存在，而是配置与运行行为不一致。继续沿用 `DeterministicProvider` 只能让测试稳定，无法让真实 OCR 和图片内容理解落地。因此需要新增一个真实 Provider 实现，并把 `cmd/worker/main.go`、`internal/platform/httpserver/server.go`、Provider 热更新后的重建逻辑统一切到同一套 `buildAIProviders` 工厂。

实现上优先让 `OCR` 和 `Caption` 走远程视觉模型调用，输入使用现有受控下载 URL，输出返回归一化文本；`Health` 继续使用轻量探活。为了降低本次变更范围，`Embedding` 可以暂时保持现有兼容策略，但 Provider 接口和路由选择必须允许后续替换为真实 embedding 调用。

备选方案：

- 直接把 `DeterministicProvider` 改成远程调用：不采用，因为会污染测试环境与本地稳定性，失去占位 provider 的价值。
- 在 enrichment 层内联 HTTP 调用远程模型：不采用，因为会绕过 Provider 抽象，破坏现有路由、错误分类和隐私策略边界。

### 决策：将 DashScope 视觉能力建模为单一 `openai-compatible` Provider，并在 Provider 内支持模型档位切换

为什么这样做：

当前要解决的核心问题不是引入多个彼此独立的 AI 供应商，而是在同一条图片理解链路里，同时满足“先跑通、可控成本”和“正式可用、效果稳定”两类需求。阿里百炼通过 OpenAI-compatible endpoint 提供视觉模型能力，不同模型之间共享相同的鉴权方式、Base URL、超时与请求结构，真正变化的主要是 `model` 字段。因此，更合理的建模方式是把 DashScope 视为单一 `openai-compatible` Provider，再由 Provider 内部维护不同档位到具体模型的映射。

第一阶段定义两个视觉档位：

- `budget`：映射到 `qwen3-vl-flash`，用于低成本跑通、历史批量补跑和预算敏感场景。
- `default`：映射到 `qwen-vl-plus`，作为正式默认档，用于线上常规识别和更稳定的图片理解质量。

这样做的原因是：

- `endpoint`、`token`、超时、健康检查和错误分类都可以保持一套，不必因为模型不同拆成多个 Provider。
- `enrichment` 只需要表达“这次任务使用哪个 profile”，不需要感知具体模型名。
- `discovery` 继续只消费 `caption`、`ocr`、`tags` 与 `search_document`，不需要知道底层使用的是 `flash` 还是 `plus`。
- 后续如果新增更高质量或更低成本的模型，只需要扩展 profile 映射，不需要改变 Provider 拓扑和调用边界。

不采用“一个模型一个 Provider 实例”的原因：

如果把 `qwen3-vl-flash` 和 `qwen-vl-plus` 建模为两个独立 Provider，会把“模型差异”误建模成“供应商差异”。这两个模型实际共享相同的 vendor、endpoint、鉴权方式和能力边界，拆分后只会引入重复的健康检查、路由候选、失败计数、遥测标签和调试信息，增加运行时复杂度，也会让配置与观测层变得更难理解。

第一阶段边界：

- 一张图片的一次识别任务只绑定一个 model profile。
- 同一轮识别中的 `caption` 与 `ocr` 使用同一 profile，不混用不同模型。
- profile 先只作用于视觉理解能力；`embedding` 是否独立配置，保留后续扩展位。
- 系统默认 profile 为 `default`，`budget` 主要用于批量补跑、预算敏感任务或显式降本场景。

配置方向：

当前 `AIProviderConfig` 只有单个 `model` 字段，后续应演进为“单 provider + 多 profile 模型映射”的结构，至少需要表达当前默认 profile，以及 provider 支持的 profile 到模型名的映射关系。

概念上接近：

```yaml
aiProviders:
  - name: aliyun-vision
    kind: openai-compatible
    endpoint: https://dashscope.aliyuncs.com/compatible-mode/v1
    modelProfile: default
    models:
      budget: qwen3-vl-flash
      default: qwen-vl-plus
```

风险与约束：

- OpenAI-compatible 表示接入方式兼容，不代表响应细节、错误码和图像输入行为与 OpenAI 官方完全一致，因此需要通过 mock 和真实环境验证视觉请求与错误处理。
- `qwen3-vl-flash` 和 `qwen-vl-plus` 在 OCR、caption 和语义分类质量上可能存在差异，因此需要让 profile 在运行记录和调试信息中可见，便于比较成本与效果。
- 第一阶段不建议在同一轮识别中混用不同模型，以避免结果风格不一致、成本核算复杂和调试困难。

### 决策：语义分类结果先归一化到“受控标签 + 搜索文档片段”，而不是立即引入全新复杂表结构

为什么这样做：

用户提出的能力包括人物属性、人数、场景地点、动作状态等，这些都可以先映射为受控词表标签，例如：

- `people:single` / `people:group`
- `gender:female-presenting` / `gender:male-presenting`
- `scene:beach` / `scene:mountain` / `scene:city`
- `activity:walking` / `activity:inside-car`

现有 discovery 和 search 已经消费 `tags`、`caption_text`、`ocr_text`、`search_document`，因此本次最稳的方式是把结构化语义先归一化为这三类落点：

- `tags`：承接可过滤、可解释的短标签；
- `caption_text`：承接自然语言摘要；
- `search_document`：承接完整检索文档，保证语义查询命中。

如有必要，可以在 `asset` 模型里增加一个轻量的结构化分类字段或内部调试字段，但不应让数据库演化阻塞整条能力落地。

备选方案：

- 新增多张专门的 classification 表：暂不采用，因为会显著扩大迁移与查询复杂度。
- 只把结果塞进自由文本 caption：不采用，因为会削弱过滤能力和结果可解释性。

### 决策：把图片内容分类视为 caption/OCR 之后的同层增强结果，而不是独立新状态机

为什么这样做：

当前状态机已经有 `metadata`、`caption`、`ocr`、`embedding`、`indexing`。如果再新增一整套独立状态机会让用户看到更多技术阶段，也会增加重试、幂等与前端文案复杂度。本次更适合把“图片内容分类”归到 AI 理解阶段中，让其成为 caption/OCR 结果归一化的一部分，并在 `ai-ready` / `indexed` 阶段自然体现。

具体上可以采用以下方式：

- `caption` 阶段：产出图片内容描述与初步场景/人物摘要；
- `ocr` 阶段：产出图片可见文本；
- 新的分类归一化步骤：在 AI 结果返回后，抽取受控标签并写入 `tags` / `search_document`；
- `indexing` 阶段：统一把这些结果写入检索字段。

备选方案：

- 新增 `classification` 阶段：暂不采用，因为会修改状态映射、前端文案和重试逻辑，收益不如直接复用现有 AI 阶段。
- 把分类逻辑塞进 discovery 查询时动态计算：不采用，因为会让结果不稳定且无法持久化。

### 决策：查询能力优先通过现有搜索入口扩展，不先做单独的高级过滤 API

为什么这样做：

用户的核心诉求是“能搜到图片里的内容和场景”。现有系统已经有搜索入口和搜索文档，因此第一阶段优先把语义分类信号写入搜索链路，让用户可以先通过统一搜索完成“女生合照海边”“坐在车上的照片”这类查询。详情页与列表摘要再逐步把这些语义结果显式展示出来。

这能避免过早引入 `gender=...&scene=...&activity=...` 一类专门过滤参数，同时保留后续把高频标签升级为结构化过滤器的空间。

备选方案：

- 先设计一套专用 discovery filter API：暂不采用，因为需要同步改前端筛选 UI 和 OpenAPI，范围过大。
- 只返回详情，不接搜索：不采用，因为会失去用户最直接的能力感知。

## Risks / Trade-offs

- [远程视觉模型输出不稳定，分类词汇容易漂移] → 通过受控词表归一化，把自由文本结果压缩成稳定标签，再进入搜索文档与 UI。
- [真实 OCR/视觉调用增加延迟和成本] → 保持异步 Worker 执行，不阻塞上传确认；并允许通过隐私策略禁用远程阶段。
- [把太多语义都折叠到 tags/search_document，可能降低后续精细过滤能力] → 保留标签命名空间和轻量结构化扩展位，避免未来迁移时失去来源。
- [Provider 构建逻辑分散在多个入口，容易再次出现配置与运行行为不一致] → 提取统一 builder，并让 API、Worker 与运行时重建都复用它。
- [对性别、人数、场景和动作的自动判断存在误判风险] → 在需求和展示层将其定义为“机器识别的可检索标签”，不作为高确定性事实，也不承诺绝对准确。

## Migration Plan

1. 先在 `internal/provider/ai` 中引入真实 `openai-compatible` Provider，并提供统一 builder。
2. 保持 `DeterministicProvider` 作为测试与本地回退实现，避免一次性破坏现有测试。
3. 在 enrichment 中增加语义分类归一化逻辑，把远程 OCR/caption 的结果映射到 `tags` 与 `search_document`。
4. 调整 discovery 返回和前端结果展示，使语义结果至少能在详情和搜索中被看见。
5. 逐步补充集成测试与 fixtures，验证真实 provider、占位 provider 和隐私禁用场景都能走通。
6. 如果远程 provider 在某环境下不可用，可回退到 deterministic 路径，但必须在状态或日志中明确可见，避免再次混淆“配置存在”和“真实能力存在”。

## Open Questions

- 本次是否需要为结构化语义标签新增单独字段，还是先完全复用现有 `tags` / `search_document` 即可？
- 对“男生/女生”这类标签，产品层是否需要更谨慎地表达为“男性呈现/女性呈现”或“人物外观推断”，以降低误导风险？
- `openai-compatible` Provider 的配置是否在本次直接升级为 `profile -> model` 映射，还是先保留单 `model` 字段并通过最小兼容方式引入 `default` / `budget`？
- `embedding` 是否需要与视觉能力分开配置为独立模型字段，还是先只让 profile 作用于 `caption` / `ocr` / 图片语义理解？
- `budget` profile 是否只用于手动补跑和批量回填，还是允许管理员把它设为全局默认档位？
- 识别运行记录、调试信息和后台设置中，是否需要显式暴露本次使用的 model profile，以支持质量对比、成本排查和回归分析？
- 如果同一张图片未来允许二次高质量重跑，是否需要保留“当前生效 profile”和“历史识别 profile”两个层次的元数据？
