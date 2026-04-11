## ADDED Requirements

### Requirement: 运维人员可以配置统一存储 Provider
系统 MUST 允许运维人员通过统一配置模型注册一个主对象存储 Provider，以及零个或多个次级存储 Provider。MVP 首版 MUST 支持将腾讯云 COS 配置为主存储 Provider。配置模型需要覆盖认证信息、bucket 或 container 标识、地域、endpoint、对象 key 前缀以及能力开关。

#### Scenario: 腾讯云 COS 主存储通过启动校验
- **WHEN** 服务以有效的腾讯云 COS 配置启动
- **THEN** 系统在将该 Provider 标记为可写之前，先完成认证信息、地域和目标 bucket 访问权限校验

#### Scenario: 本地开发可使用 S3 兼容模拟器联调
- **WHEN** 开发环境使用受支持的 S3 兼容对象存储模拟器启动服务
- **THEN** 系统仍然通过统一存储契约完成启动校验，以保证上传和备份链路可以在无云账号场景下联调

### Requirement: Provider 凭据必须受控加载并统一脱敏
系统 MUST 通过环境变量、Secret Manager 引用、加密存储或等价的受控来源加载对象存储与 AI Provider 凭据；任何 API 响应、日志、错误、指标、trace 和健康检查结果都不能暴露完整密钥、Token、预签名 URL 或可直接复用的认证头。

#### Scenario: 运维查看已保存的 Provider 配置
- **WHEN** 运维人员通过管理接口查看某个已配置 Provider 的详情
- **THEN** 系统只返回脱敏后的密钥摘要、外部引用标识或能力状态，而不是完整秘密值

#### Scenario: Provider 启动校验失败
- **WHEN** 服务在启动或健康检查中发现 Provider 凭据无效
- **THEN** 系统记录可操作的错误分类和失败原因，但不会在返回体或日志里输出完整秘密值

### Requirement: 存储操作使用统一契约
系统 MUST 通过统一的存储契约执行对象写入、读取、head、删除、列举、预签名、metadata 更新和分片上传操作，从而保证导入与备份流程不依赖厂商特有的响应格式。

#### Scenario: 上传流程保持 Provider 无关
- **WHEN** 导入流程通过任意受支持的存储适配器保存资产
- **THEN** 无论底层 Provider 如何实现，流程都能得到统一的对象标识、metadata 字段和错误分类

#### Scenario: 服务端签发腾讯云 COS 预签名上传
- **WHEN** Web 客户端请求某个待上传对象的上传凭据
- **THEN** 存储 Provider 适配层返回与服务端分配对象 key 绑定的上传方法、URL、必要请求头或表单字段、过期时间以及确认阶段所需的校验元数据

#### Scenario: 分片上传通过统一契约完成初始化与校验
- **WHEN** 文件大小超过单次上传阈值而需要走分片上传
- **THEN** 系统通过统一存储契约完成分片初始化、分片级预签名信息生成、完成提交和最终对象校验，而不是把 COS 特有的流程泄漏给上层导入状态机

### Requirement: 存储 Provider 必须默认私有访问并限制签名作用域
系统 MUST 将主存储和备份目标配置为默认私有读；上传和下载签名都必须绑定单个对象、有限时效和最小必需权限，而不能生成可长期复用或可跨对象泛化的访问凭据。

#### Scenario: 目标 bucket 被配置为公共读
- **WHEN** 服务启动时检测到主存储或备份目标存在公共读风险
- **THEN** 系统拒绝将该目标标记为合格写入目标，或至少发出阻断级告警，直到运维显式修复配置

#### Scenario: 已授权用户请求下载受保护资产
- **WHEN** 应用层确认某个已认证请求有权读取指定资产
- **THEN** 存储 Provider 适配层只返回与该对象绑定、分钟级有效期且不可列举其他对象的受控下载信息

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
