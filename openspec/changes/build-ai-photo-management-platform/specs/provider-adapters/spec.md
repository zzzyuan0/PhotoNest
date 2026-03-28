## ADDED Requirements

### Requirement: 运维人员可以配置统一存储 Provider
系统 MUST 允许运维人员通过统一配置模型注册一个主对象存储 Provider，以及零个或多个次级存储 Provider。配置模型需要覆盖认证信息、bucket 或 container 标识、地域以及能力开关。

#### Scenario: 主存储 Provider 通过启动校验
- **WHEN** 服务以有效的 S3 兼容配置或厂商 OSS 配置启动
- **THEN** 系统在将该 Provider 标记为可写之前，先完成认证信息和目标 bucket 访问权限校验

### Requirement: 存储操作使用统一契约
系统 MUST 通过统一的存储契约执行对象写入、读取、head、删除、列举、预签名、metadata 更新和分片上传操作，从而保证导入与备份流程不依赖厂商特有的响应格式。

#### Scenario: 上传流程保持 Provider 无关
- **WHEN** 导入流程通过任意受支持的存储适配器保存资产
- **THEN** 无论底层 Provider 如何实现，流程都能得到统一的对象标识、metadata 字段和错误分类

### Requirement: AI 任务按策略与能力路由
系统 MUST 基于统一的 AI Provider 契约对 caption、OCR、embedding 等任务进行路由，并根据任务类型、能力支持、隐私策略和 Provider 健康状态选择执行目标。

#### Scenario: 敏感资产避免发送到远程 AI Provider
- **WHEN** 某个资产或相册被标记为仅允许本地处理
- **THEN** 任务路由器必须排除远程 AI Provider；如果没有符合策略的 Provider，则把任务标记为待处理或失败，并给出可操作的策略原因

### Requirement: Provider 故障不能污染照片库状态
系统 MUST 记录 Provider 的失败、重试和降级状态，同时不能破坏已有资产元数据，也不能把无关流程误标记为成功。

#### Scenario: AI Provider 在识别增强过程中失败
- **WHEN** 已配置的 AI Provider 在 caption 任务中返回瞬时错误
- **THEN** 系统记录本次失败尝试，保留此前已完成的资产元数据，并按任务策略安排重试
