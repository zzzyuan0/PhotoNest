# COS 上传查看验收记录

这份记录用于完成 `complete-cos-upload-view-flow` 的任务 `5.3`：在真实 Go 运行环境和真实 COS 配置下，验证一次“上传到 COS 并在 Web 查看”的完整闭环。

## 验收前提

- API 进程和 Worker 进程都通过真实 Go 运行时启动。
- PostgreSQL 与 Redis 可用。
- `.env.local` 或运行环境中至少提供下列变量：
  - `PHOTONEST_DATABASE_DSN` 或 `PHOTONEST_DATABASE_PASSWORD`
  - `PHOTONEST_SESSION_SIGNING_KEY`
  - `PHOTONEST_BOOTSTRAP_PASSWORD`
  - `PHOTONEST_COS_SECRET_ID`
  - `PHOTONEST_COS_SECRET_KEY`
- 如果当前环境只提供 `STORAGE_COS_SECRET_ID` / `STORAGE_COS_SECRET_KEY`，开发脚本现在会自动桥接到 `PHOTONEST_*` 变量。
- COS bucket 已提前配置允许 `http://localhost:3000` 的上传跨域。

## 启动步骤

1. 启动 PostgreSQL 与 Redis。
2. 启动 API：

```bash
make api
```

3. 启动 Worker：

```bash
make worker
```

4. 启动 Web：

```bash
pnpm --filter @photonest/web dev
```

5. 打开健康检查：

```bash
curl http://localhost:8080/api/v1/health
```

## 验收步骤

1. 在 Web 登录页使用 bootstrap 账号登录。
2. 进入 `/import` 页面。
3. 使用 `libraryId=11111111-1111-1111-1111-111111111111`。
4. 上传一个小文件，确认走单次上传分支。
5. 上传一个大于 `8 MiB` 的文件，确认走 multipart 分支。
6. 等待页面出现“已确认入库”状态。
7. 打开首页、时间线或资产详情页，确认新资产可见，且处理中资产不会被误显示为失败。
8. 确认 Worker 已消费任务，并把处理状态推进到后续阶段。

## 记录模板

### 验收日期

- `待填写`

### 运行环境

- API：`待填写`
- Worker：`待填写`
- Web：`待填写`
- PostgreSQL：`待填写`
- Redis：`待填写`
- COS Bucket：`待填写`

### 样例文件

- 单次上传文件：`待填写`
- Multipart 上传文件：`待填写`

### 验收结果

- [ ] 登录成功
- [ ] 导入会话创建成功
- [ ] 单次上传成功
- [ ] Multipart 上传成功
- [ ] 上传确认成功
- [ ] 首页或时间线可见新资产
- [ ] 资产详情页可见处理中状态
- [ ] Worker 消费成功

### 备注

- `待填写`
