# photo-enrichment Specification

## Purpose
TBD - created by archiving change complete-cos-upload-view-flow. Update Purpose after archive.
## Requirements
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

### Requirement: 资产必须经过异步识别增强状态机
系统 MUST 以异步方式处理已接收的资产，并通过明确的生命周期状态追踪导入、派生资源生成、元数据提取、AI 识别增强、索引更新以及部分失败或终态失败。

#### Scenario: 识别增强不会阻塞上传完成
- **WHEN** 某次上传被系统接收进入照片库
- **THEN** 系统在 AI 分析完成前就返回导入成功，并随着后台阶段完成持续更新资产处理状态

### Requirement: 每个资产都要提取拍摄元数据和地点信息
系统 MUST 从受支持资产中提取拍摄时间、设备信息、尺寸、媒体类型和 GPS 坐标等结构化元数据，并将可用的 GPS 坐标解析为可读地点字段。

#### Scenario: 带 GPS 的照片获得地点元数据
- **WHEN** 一张导入照片在元数据中包含有效经纬度
- **THEN** 系统保存原始坐标和解析后的地点字段，以供后续浏览和过滤使用

### Requirement: AI 识别结果必须以归一化元数据存储
系统 MUST 把 caption、tag、OCR 文本、embedding，以及可选的人脸或主题聚类等结果，以统一内部元数据结构进行存储，而不是把 Provider 的原始响应体作为事实来源。

#### Scenario: 对包含文字的图片完成 OCR 和描述生成
- **WHEN** AI 识别增强在一张包含可见文字的图片上成功运行
- **THEN** 系统在资产元数据记录中保存 OCR 文本、生成的描述和可检索标签

### Requirement: 敏感元数据的持久化与暴露必须受隐私策略约束
系统 MUST 把 GPS 坐标、地点解析结果、OCR 文本、caption、embedding 以及未来可选的人脸或主题聚类视作受策略约束的敏感元数据，并支持在照片库、相册或资产级别控制哪些阶段允许执行、持久化或对外返回。

#### Scenario: 敏感相册禁止远程 OCR 与地理反解
- **WHEN** 某个相册被配置为禁止将内容发送到远程 Provider，且仅允许保留最小本地元数据
- **THEN** Worker 跳过不符合策略的 OCR 或地理反解阶段，记录清晰的策略原因，并保留其他允许的处理结果

#### Scenario: 一般发现接口请求不需要原始 GPS 坐标
- **WHEN** 前端请求时间线或搜索结果列表
- **THEN** 系统只返回当前视图所需的最小元数据，而不会默认附带原始坐标、完整 OCR 全文或其他高敏感字段

### Requirement: 原始 Provider 响应默认不保留且调试保留必须可过期
系统 MUST 默认只持久化归一化后的内部识别记录，而不长期保存原始 AI Provider 响应；若为了排查问题临时开启调试保留，则必须附带管理员访问限制、自动过期时间和显式清理路径。

#### Scenario: 正常识别流程完成
- **WHEN** 某个资产的 caption、OCR 或 embedding 任务成功完成
- **THEN** 系统保存归一化后的内部结果，并且默认不把 Provider 原始响应持久化为长期事实数据

#### Scenario: 运维临时开启原始响应调试保留
- **WHEN** 管理员在受控窗口内为某个故障任务开启调试保留
- **THEN** 系统为该保留记录设置自动过期时间，并限制只有具备相应权限的管理员可以查看或清除

### Requirement: 部分识别失败必须可见且可恢复
系统 MUST 允许某一个识别阶段失败，而不丢弃其他已成功阶段的结果，并且要暴露一个可恢复的错误状态，便于后续重试或人工检查。

#### Scenario: 描述成功但 embedding 生成失败
- **WHEN** caption 生成成功，但 embedding 生成失败
- **THEN** 该资产仍然可以通过已有的结构化元数据和文本元数据被检索到，同时将失败的 embedding 阶段标记为待重试或待人工检查

