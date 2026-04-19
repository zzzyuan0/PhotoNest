## ADDED Requirements

### Requirement: Provider 设置接口必须支持更新 COS 运行时配置
系统 MUST 实现 `PUT /api/v1/settings/providers/{providerName}`，支持更新 COS Provider 的运行时配置，并返回脱敏后的配置摘要与能力状态。

#### Scenario: 运维更新 COS Provider 配置
- **WHEN** 运维人员向 `PUT /api/v1/settings/providers/{providerName}` 提交新的 COS bucket、region、endpoint 或凭据配置
- **THEN** 系统保存经归一化和脱敏处理后的配置，并在响应中返回该 Provider 的可用状态摘要，而不是明文秘密值

### Requirement: Provider 设置更新必须先校验后生效
系统 MUST 在应用新的 COS Provider 配置前执行必要的连通性、权限和私有读约束校验；校验失败时不得将该配置标记为生效。

#### Scenario: 新配置无法访问目标 bucket
- **WHEN** 运维提交的 COS 配置无法完成认证或 bucket 访问校验
- **THEN** 系统拒绝激活该配置，并返回可操作的失败原因，同时保持上一份可用配置不受影响
