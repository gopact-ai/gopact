# gopact Documentation

<!-- gopact:doc-language: zh -->

[英文文档](./README.md)

## 中文

这里保存 `gopact` 除根 README 和 LICENSE 以外的长期文档。根 README 面向首次访问者；本目录面向三类读者：

- 使用者：了解当前 SDK 能力、安装方式、示例入口和稳定性边界。
- 扩展开发者：实现 provider、tool、MCP/A2A、agent template 或其他 extension。
- 维护者：维护 release gate、CI、版本策略、安全响应和公开仓库治理。

### 推荐阅读顺序

| 目标 | 阅读 |
| --- | --- |
| 判断现在能用什么 | [FEATURES.md](FEATURES.md) |
| 开始贡献代码 | [CONTRIBUTING.md](CONTRIBUTING.md) |
| 报告安全问题 | [SECURITY.md](SECURITY.md) |
| 查看用户可见变化 | [CHANGELOG.md](CHANGELOG.md) |
| 理解整体架构 | [design/index.md](design/index.md) |
| 设计或评审 public API | [design/api-ergonomics.md](design/api-ergonomics.md), [design/deprecation-policy.md](design/deprecation-policy.md), [design/versioning-policy.md](design/versioning-policy.md) |
| 编写扩展或 template | [design/template-guide.md](design/template-guide.md), [design/modules.md](design/modules.md), [design/external-integration-roadmap.json](design/external-integration-roadmap.json) |
| 准备 v1 / release gate | [design/migration-guide.md](design/migration-guide.md), [design/v1-migration-plan.json](design/v1-migration-plan.json), [design/milestone-readiness.json](design/milestone-readiness.json) |
| 维护仓库规则 | [maintainers/repository-governance.md](maintainers/repository-governance.md) |

### 文档分层

- `FEATURES.md` 是能力矩阵，只记录已有离线验收或 conformance 的能力。
- `design/` 保存架构、边界、迁移、版本和 extension 设计。
- `research/` 保存调研材料，不代表已经承诺的 public API。
- `superpowers/` 保存历史执行计划，不作为用户文档入口。
- `maintainers/` 保存仓库治理和发布维护规则。
