## ADDED Requirements

### Requirement: 上传确认后的增强任务必须进入 Redis 持久队列
系统 MUST 在上传确认成功后把派生资源生成、元数据提取和后续识别增强任务写入 Redis 驱动队列，而不是仅保存在 API 进程内存中。

#### Scenario: 上传确认成功后 API 进程退出
- **WHEN** 某个资产已经确认入库且增强任务已经入队，随后 API 进程退出
- **THEN** 队列中的任务仍然保留，且不会因为 API 进程重启而丢失

### Requirement: 独立 Worker 必须消费增强任务并幂等推进状态
系统 MUST 由独立 Worker 进程消费 Redis 队列中的增强任务，并以幂等方式更新派生资源、识别阶段运行记录和资产处理状态。

#### Scenario: Worker 正常消费待处理资产
- **WHEN** Redis 队列中存在某个已确认资产的增强任务且 Worker 正在运行
- **THEN** Worker 读取任务、执行对应阶段，并将结果与阶段状态持久化到 PostgreSQL

#### Scenario: Worker 重复收到同一资产阶段任务
- **WHEN** 因重试或重复投递导致 Worker 再次消费同一资产同一阶段的任务
- **THEN** 系统不会重复创建冲突记录，而是根据现有阶段状态执行幂等跳过或安全重试
