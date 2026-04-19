## MODIFIED Requirements

### Requirement: AI 任务按策略与能力路由
系统 MUST 基于统一的 AI Provider 契约对 caption、OCR、embedding 等任务进行路由，并根据任务类型、能力支持、隐私策略和 Provider 健康状态选择执行目标；对于声明为 `openai-compatible` 的视觉 Provider，系统 MUST 支持基于图片内容的真实 OCR 与图片理解调用，而不是仅依赖文件名或演示性占位结果。

#### Scenario: 远程视觉 Provider 执行真实 OCR
- **WHEN** 某个 `openai-compatible` Provider 被配置为支持 `ocr`
- **THEN** Worker 向该 Provider 传递受控图片访问输入并获得基于图片内容的 OCR 结果，而不是用文件名分词替代识别

#### Scenario: 远程视觉 Provider 执行图片内容理解
- **WHEN** 某个 `openai-compatible` Provider 被配置为支持 `caption`
- **THEN** 系统基于图片内容生成描述与可归一化的场景/人物语义信号，并将其交给后续归一化流程处理

#### Scenario: 单个 Provider 内按 profile 选择视觉模型
- **GIVEN** 某个 `openai-compatible` Provider 在统一 endpoint、鉴权和超时配置下声明了多个视觉 model profile
- **WHEN** enrichment 为某次识别任务显式请求 `budget` 或 `default`
- **THEN** Provider 在同一条 Provider 定义内将 profile 映射到具体模型名完成远程调用
- **AND** MUST NOT 通过切换到另一条 Provider 定义来表达模型档位差异

#### Scenario: 未显式指定时使用默认 profile
- **GIVEN** 某个 `openai-compatible` Provider 已配置默认 profile 与 profile -> model 映射
- **WHEN** enrichment 发起一次未显式指定 profile 的图片理解请求
- **THEN** Provider MUST 使用默认 profile
- **AND** MUST 将该 profile 映射到对应视觉模型发起真实远程调用

#### Scenario: 敏感资产避免发送到远程 AI Provider
- **WHEN** 某个资产或相册被标记为仅允许本地处理
- **THEN** 任务路由器必须排除远程 AI Provider；如果没有符合策略的 Provider，则把任务标记为待处理或失败，并给出可操作的策略原因

### Requirement: 同一轮视觉识别必须保持一致的模型档位
系统 MUST 保证同一张图片在同一轮识别任务中的视觉理解阶段使用同一个 model profile，以避免 `caption`、`ocr` 与后续语义分类结果来自不同模型而造成风格漂移、成本核算复杂或调试困难。

#### Scenario: Caption 与 OCR 共享同一 profile
- **GIVEN** 某图片的一轮识别任务已绑定一个视觉 profile
- **WHEN** 系统依次执行 `caption` 与 `ocr` 阶段
- **THEN** 两个阶段 MUST 使用同一 profile
- **AND** MUST 保持同一 Provider 内部的 profile -> model 映射结果一致

### Requirement: Provider 故障不能污染照片库状态
系统 MUST 记录 Provider 的失败、重试和降级状态，同时不能破坏已有资产元数据，也不能把无关流程误标记为成功；当真实视觉 Provider 返回不可用、超时、鉴权失败或非预期响应时，系统 MUST 给出可分类的错误结果，并保留已完成的 OCR、描述或分类结果。

#### Scenario: 视觉 Provider 在 OCR 阶段瞬时失败
- **WHEN** 已配置的远程视觉 Provider 在 OCR 调用中返回临时错误
- **THEN** 系统记录这次失败、保留此前已成功的资产元数据，并按任务策略安排重试，而不会把资产误标记为识别完成

#### Scenario: 视觉 Provider 返回不可归一化结果
- **WHEN** 远程视觉 Provider 返回了无法映射到内部结果模型的响应
- **THEN** 系统将该次调用标记为失败或部分失败，保留调试上下文，并且不会把未归一化的原始响应直接当作事实元数据写入照片库

### Requirement: 有效模型档位选择必须可观测
系统 MUST 在识别运行记录、调试保留或等效的可观测性元数据中保留本次请求实际使用的 provider、profile 和模型名，以支持质量对比、成本排查与回归分析。

#### Scenario: 调试元数据记录生效的 profile 与模型
- **GIVEN** 一次真实远程视觉请求成功或失败
- **WHEN** 系统保存识别运行记录或调试元数据
- **THEN** 记录中 MUST 包含实际命中的 provider 名称
- **AND** MUST 包含本次使用的 model profile
- **AND** MUST 包含解析后的具体模型名
