# PhotoNest 运维与交付说明

这份文档覆盖当前仓库已经落地的开发与运维闭环，包括部署、回滚、OpenAPI Client 生成、腾讯云 COS 配置、隐私策略、密钥治理、审计排查以及备份与导出校验。

## 1. 启动与部署

### 本地依赖启动

- 仅启动 PostgreSQL 与 Redis：

```bash
make up
```

- 启动 PostgreSQL、Redis 与对象存储模拟器 MinIO：

```bash
make up-sim
```

### 启动 API 与 Worker

- API 进程：

```bash
make api
```

- Worker 进程：

```bash
make worker
```

### 一键启动整套本地栈

- 对象存储模拟器模式：

```bash
make dev
```

- 真实 COS 模式：

```bash
make dev-cos
```

- 查看状态：

```bash
make status
```

- 停止应用进程：

```bash
make stop
```

一键启动会把 API、Worker 和 Web 的日志写入 `.cache/dev-stack/logs/`。

### 健康检查

```bash
make health
```

`GET /api/v1/health` 现在除了基础依赖健康状态外，还会返回最近的 telemetry 快照，便于确认：

- `provider.health`
- `job.failure`
- `index.progress`
- `backup.lag`
- `export.generated`
- `audit.anomaly`

## 2. 回滚策略

出现回滚需求时，建议按下面顺序收敛风险：

1. 停止新的上传入口和导出入口，避免继续产生副作用。
2. 停止 Worker 或后台任务消费，冻结识别、索引和备份复制。
3. 保留 PostgreSQL 中的资产与对象引用事实数据，不直接删除对象存储中的原图或派生资源。
4. 如果问题来自前端发布，可先回滚 `apps/web` 构建产物，不影响已经入库的资产。
5. 如果问题来自备份或导出流程，可保留主库读取能力，仅关闭 `/api/v1/exports` 和自动复制。

## 3. OpenAPI 与前端 Client

### 重新生成 TypeScript Client

```bash
pnpm gen:client
```

当前变更已经把下面这些契约纳入生成流程：

- 时间线与地点浏览
- 重复候选审查
- 收藏与精选相册
- 受控导出与恢复计划摘要

如果修改了 `openapi/openapi.yaml`，请在提交前重新执行一次生成，并确认 `apps/web/lib/api/schema.d.ts` 已同步更新。

## 4. 腾讯云 COS 配置

### 主库存储

主库存储配置示例位于：

- `config/examples/app.yaml`

关键字段包括：

- `storageProviders.primary.kind: tencent-cos`
- `storageProviders.primary.bucket`
- `storageProviders.primary.region`
- `storageProviders.primary.endpoint`
- `storageProviders.primary.keyPrefix`
- `storageProviders.primary.privateRead: true`

### 二级备份

备份目标使用：

- `storageProviders.backup[]`

建议至少保证：

- 主库与备份桶使用独立的访问密钥
- `privateRead` 始终为 `true`
- `keyPrefix` 与主库存储路径隔离
- 备份桶单独配置生命周期与权限策略

### CORS

COS/模拟器的跨域配置示例位于：

- `config/examples/cos-cors.json`

前端直传需要把 Web 域名加入 `allowedOrigins`，并确保上传只允许短时效、单对象作用域签名。

## 5. 隐私策略

当前隐私策略接口：

- `PUT /api/v1/settings/privacy-policy`

重点开关包括：

- `gpsMode`
- `ocrMode`
- `captionMode`
- `embeddingMode`

建议的运维基线：

- 默认保持 `gpsMode=owner-only`
- 对敏感照片库或相册关闭远程 OCR、caption、embedding
- 为真实视觉识别配置 `modelProfile` 与 `models`，至少保留 `default` 和 `budget` 两档，便于线上默认识别与历史批量补跑分离
- 不要把原始 GPS、完整 OCR 文本和 Provider 原始回包作为普通排障数据长期保留

## 6. 密钥治理

所有高敏感凭据应优先通过环境变量或受控 Secret Source 注入，不建议直接写死到配置文件：

- `PHOTONEST_COS_SECRET_ID`
- `PHOTONEST_COS_SECRET_KEY`
- `PHOTONEST_COS_BACKUP_SECRET_ID`
- `PHOTONEST_COS_BACKUP_SECRET_KEY`
- `PHOTONEST_AI_OPENAI_TOKEN`
- `PHOTONEST_SESSION_SIGNING_KEY`
- `PHOTONEST_BOOTSTRAP_PASSWORD`

仓库内的 `config/examples/secret-sources.yaml` 可作为密钥引用策略示例。

同时要注意：

- 日志、审计、任务载荷和 telemetry 不应记录完整密钥
- 导出与调试链路不能返回可长期复用的稳定下载地址
- 调试排查结束后，应及时撤销临时凭据与导出工件

## 7. 审计查询与异常访问排查

当前审计事件通过结构化日志输出，格式以 `audit {json}` 为主。重点动作包括：

- 登录
- 导出
- Provider 设置变更
- 隐私策略变更
- 收藏或相册写入失败
- 未认证、CSRF 失败、近期认证缺失等异常访问

### 快速排查示例

查看最近的审计异常：

```bash
rg 'audit ' ./ -g '*.log'
```

如果日志已经被收集到外部系统，建议优先过滤：

- `result=denied`
- `result=invalid`
- `action=library.export`
- `action=asset.favorite.update`
- `action=album.asset.add`

`GET /api/v1/health` 返回中的 `audit.anomaly` telemetry 也可以作为第一层告警信号。

## 8. 备份复制与校验

当前备份流程会：

1. 在上传确认后对原图和派生对象执行复制。
2. 为备份目标写入独立的对象引用。
3. 记录 `backup_records` 风格的状态信息。
4. 仅在复制成功并通过长度校验后，将资产标记为 `backupStatus=verified`。

### 日常核查建议

- 时间线与资产详情返回 `backupStatus`
- `/api/v1/health` 中可查看最近 `backup.lag` 快照
- 若某个资产一直停留在 `failed`，优先检查：
  - 备份桶权限
  - key prefix 是否正确
  - 备份对象是否可写
  - 目标对象长度是否与源对象一致

## 9. 导出包与恢复规划

导出接口：

- `POST /api/v1/exports`

当前行为：

- 需要认证、授权、CSRF 和近期认证
- 返回短时效 `archiveUrl`
- 同时返回 `redactedManifestUrl`
- 响应里包含 `recoveryPlan`

导出归档内包含：

- `manifest.json`
- `manifest.redacted.json`
- `assets/<assetId>/<originalFilename>`

### 恢复规划建议

使用 `manifest.json` 时，至少检查：

- `assetId`
- `contentSha256`
- `mediaType`
- `timelineAt`
- `objectReferences.purpose`

如果 `recoveryPlan.warnings` 中提示缺失 `original` 引用，应先停止恢复，重新核对导出内容完整性。
