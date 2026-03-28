# PhotoNest

PhotoNest 是一个面向个人分散照片库的照片管理平台，目标是把来自手机相册、聊天导出文件、云盘和本地目录的照片与视频统一收拢到一个可检索、可增强、可备份的主库中。

项目当前处于 OpenSpec 驱动的设计阶段：仓库里已经整理了产品范围、架构决策、能力规格和任务拆分，但业务代码还没有开始实现。这份 README 的目标是帮助后续开发者快速理解项目定位，并找到下一步落地入口。

## 项目目标

- 统一导入照片和视频资产，形成稳定的主照片库
- 抽象对象存储 Provider，避免绑定单一云厂商
- 抽象 AI Provider，统一 caption、OCR、embedding 等识别结果
- 建立异步处理流水线，补齐 EXIF、地点、标签、描述与索引
- 提供时间线、地点浏览、自然语言搜索、重复照片审查等发现能力
- 提供独立备份、导出与恢复路径，降低照片资产丢失风险

## 当前状态

- 已完成：OpenSpec proposal、design、specs 与 tasks 初稿
- 已完成：仓库初始化、GitHub 远程仓库配置
- 待开始：项目代码结构、运行时、数据库 schema、导入链路与 Worker

如果你现在准备开始实现，建议优先从“基础设施”阶段进入，也就是先创建共享领域模型、API/Worker 运行时和本地开发依赖。

## 核心能力

项目当前定义了以下能力边界：

- `provider-adapters`：统一管理对象存储和 AI 服务的接入方式、配置校验、调用路由与降级策略
- `photo-ingestion`：导入照片和视频资产，完成去重、持久化与派生资源登记
- `photo-enrichment`：异步提取 EXIF、地点、OCR、标签、描述和向量特征，并跟踪处理状态
- `photo-discovery`：支持时间线、地点、主题和自然语言检索，覆盖重复照片审查与精选相册整理
- `backup-and-export`：提供二级备份、完整性校验、导出包和恢复所需元数据

## 设计摘要

当前设计倾向于先实现一个“模块化单体 + 后台 Worker”的 MVP：

- API 进程负责上传、配置、浏览与检索请求
- Worker 进程负责 EXIF、AI 识别、索引更新和备份任务
- PostgreSQL 作为元数据事实来源
- `pgvector` 与 PostgreSQL 全文检索共同支撑混合搜索
- 对象存储负责原图、缩略图、预览图等二进制资产
- Provider 返回结果必须先归一化，再进入内部领域模型

这套设计兼顾了首版落地成本和后续扩展空间，避免过早拆成微服务。

## 仓库结构

```text
.
├── AGENTS.md
├── openspec/
│   ├── config.yaml
│   └── changes/
│       └── build-ai-photo-management-platform/
│           ├── proposal.md
│           ├── design.md
│           ├── tasks.md
│           └── specs/
└── .codex/
```

关键入口：

- `openspec/changes/build-ai-photo-management-platform/proposal.md`：项目背景、范围与影响说明
- `openspec/changes/build-ai-photo-management-platform/design.md`：架构决策、风险与迁移计划
- `openspec/changes/build-ai-photo-management-platform/tasks.md`：推荐实现顺序
- `openspec/changes/build-ai-photo-management-platform/specs/`：能力级需求说明
- `AGENTS.md`：仓库协作约定

## 建议的启动顺序

1. 确定首版技术栈，例如后端框架、前端框架、队列方案和对象存储模拟器
2. 建立 API 与 Worker 的基础目录结构
3. 定义统一领域模型和数据库 schema
4. 补齐本地开发脚本，例如数据库、Redis、对象存储和迁移工具
5. 从最小导入链路开始实现，再接入异步增强流程

## 开发方式

当前仓库采用“规范先行”的方式推进：

- 先在 OpenSpec 中明确变更背景、设计决策和需求边界
- 再按 `tasks.md` 的顺序逐步实现
- 代码、文档和后续变更说明默认使用中文

## 下一步建议

如果继续往前推进，这个仓库最适合优先补下面三项：

- 初始化项目目录结构，例如 `apps/`、`packages/`、`services/` 或其他明确分层
- 增加本地开发环境，例如 `docker-compose.yml`、迁移脚本和统一配置入口
- 建立最小可运行的 API 与 Worker 骨架

## 参考文档

- [Proposal](./openspec/changes/build-ai-photo-management-platform/proposal.md)
- [Design](./openspec/changes/build-ai-photo-management-platform/design.md)
- [Tasks](./openspec/changes/build-ai-photo-management-platform/tasks.md)

