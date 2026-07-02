# gopact Documentation

<!-- gopact:doc-language: en -->

Chinese documentation: [README_zh.md](README_zh.md)

This directory contains long-lived project documentation other than the root README and LICENSE. The root README is for first-time readers; this directory serves three audiences:

- Users who need capability, installation, example, and stability information.
- Extension authors who implement providers, tools, MCP/A2A integrations, agent templates, or other modules.
- Maintainers who operate release gates, CI, versioning, security response, and repository governance.

### Recommended Reading Order

| Goal | Read |
| --- | --- |
| Check what works today | [FEATURES.md](FEATURES.md) |
| Contribute code | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Report a vulnerability | [SECURITY.md](SECURITY.md) |
| Review user-visible changes | [CHANGELOG.md](CHANGELOG.md) |
| Understand the architecture | [design/index.md](design/index.md) |
| Design or review public API | [design/api-ergonomics.md](design/api-ergonomics.md), [design/deprecation-policy.md](design/deprecation-policy.md), [design/versioning-policy.md](design/versioning-policy.md) |
| Build extensions or templates | [design/template-guide.md](design/template-guide.md), [design/modules.md](design/modules.md), [design/external-integration-roadmap.json](design/external-integration-roadmap.json) |
| Prepare v1 or release gates | [design/migration-guide.md](design/migration-guide.md), [design/v1-migration-plan.json](design/v1-migration-plan.json), [design/milestone-readiness.json](design/milestone-readiness.json) |
| Maintain repository rules | [maintainers/repository-governance.md](maintainers/repository-governance.md) |

### Documentation Layers

- `FEATURES.md` is the capability matrix; it only lists behavior with offline tests or conformance coverage.
- `design/` contains architecture, boundaries, migration, versioning, and extension design.
- `research/` contains background research and does not define committed public API.
- `superpowers/` contains historical execution plans and is not a user documentation entry point.
- `maintainers/` contains repository governance and release maintenance rules.
