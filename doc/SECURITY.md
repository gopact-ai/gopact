# Security Policy

<!-- gopact:doc-language: zh,en -->

## 中文

`gopact` 处理的是 agent 运行时边界：模型输入输出、工具调用、checkpoint、artifact、MCP/A2A 通信和 verification evidence。安全问题请优先私下报告，避免在公开 issue 中泄露 exploit、token、prompt、tool payload 或客户数据。

## Supported Versions

| Version | Supported |
| --- | --- |
| `main` | Yes |
| tagged pre-v1 releases | Best effort |
| unsupported forks | No |

项目仍处于 pre-v1。安全修复优先进入 `main`，必要时再回补到公开 tag。

## Reporting a Vulnerability

请通过 GitHub 的 private vulnerability reporting 或 gopact-ai 组织维护者私下联系。报告中请包含：

- 受影响的 package、command 或 workflow；
- 可复现步骤；
- 影响范围和信任边界；
- 是否可能暴露 secret、prompt、tool args/results、artifact、checkpoint、A2A/MCP 消息或外部 token；
- 已知缓解方式。

不要在公开 issue、PR、commit message、测试 fixture 或日志里放入真实 secret、raw prompt、raw model response、raw tool payload 或客户数据。

## English

`gopact` sits on agent runtime boundaries: model input/output, tool calls, checkpoints, artifacts, MCP/A2A communication, and verification evidence. Please report security issues privately first so exploit details, tokens, prompts, tool payloads, or customer data do not leak through public issues.

## Supported Versions

| Version | Supported |
| --- | --- |
| `main` | Yes |
| tagged pre-v1 releases | Best effort |
| unsupported forks | No |

The project is still pre-v1. Security fixes target `main` first and may be backported to public tags when needed.

## Reporting a Vulnerability

Use GitHub private vulnerability reporting or contact the gopact-ai maintainers privately. Include:

- affected package, command, or workflow;
- reproduction steps;
- impact and trust boundary;
- whether secrets, prompts, tool args/results, artifacts, checkpoints, A2A/MCP messages, or external tokens may be exposed;
- known mitigations.

Do not include real secrets, raw prompts, raw model responses, raw tool payloads, or customer data in public issues, pull requests, commit messages, test fixtures, or logs.
