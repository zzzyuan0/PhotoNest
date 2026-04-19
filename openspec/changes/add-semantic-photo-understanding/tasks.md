## 1. Provider 接入与运行时构建

- [x] 1.1 在 `internal/provider/ai` 中新增真实 `openai-compatible` Provider，实现基于图片 URL 的远程 `OCR` 与 `Caption` 调用
- [x] 1.2 扩展 AI Provider 配置模型，支持在单个 `openai-compatible` Provider 下声明默认 model profile 与 profile -> model 的映射关系
- [x] 1.3 提取统一的 AI Provider builder，让 `cmd/worker/main.go`、`internal/platform/httpserver/server.go` 与 Provider 重建逻辑按 `kind` 构建真实 Provider 或 `DeterministicProvider`
- [x] 1.4 在真实 Provider 中支持根据识别任务选择 `budget` / `default` profile，并将 profile 映射到具体视觉模型
- [x] 1.5 为真实 Provider 补充鉴权、超时、探活和错误分类处理，确保远程故障不会破坏现有增强状态机
- [x] 1.6 在识别运行记录、调试保留或遥测信息中记录本次实际使用的 provider、model profile 与模型名，便于质量和成本排查

## 2. 识别增强与语义归一化

- [x] 2.1 在 `internal/enrichment` 中把真实 OCR 与图片内容描述结果接入现有 caption / ocr 阶段
- [x] 2.2 增加语义归一化逻辑，把人数、人物呈现、场景地点、动作状态等图片内容信号映射为受控标签命名空间
- [x] 2.3 调整 `tags`、`searchDocument` 与相关资产字段生成逻辑，使 OCR、caption 与语义标签共同进入搜索整理链路

## 3. 搜索与结果展示接入

- [x] 3.1 调整 `internal/discovery` 与 HTTP handlers，使搜索和详情接口返回最小必要的图片内容理解摘要
- [x] 3.2 更新前端资产详情或结果映射逻辑，展示 OCR、描述和语义标签等新结果，同时保持处理中状态可解释
- [x] 3.3 校准识别阶段与用户文案，让“处理中”“已可搜索”“部分失败”能够准确反映真实 OCR 与内容分类进度

## 4. 测试、回归与运行说明

- [x] 4.1 为真实 `openai-compatible` Provider 增加基于 mock HTTP 服务的单元测试，覆盖 OCR/Caption 成功与失败路径
- [x] 4.2 为 enrichment / discovery 增加集成测试，覆盖语义标签写入、自然语言查询命中与隐私策略禁用场景
- [x] 4.3 补充或更新开发配置与说明文档，明确真实 OCR/图片理解所需的模型、Token、回退行为与已知限制
