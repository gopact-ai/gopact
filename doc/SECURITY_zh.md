# Security Policy

<!-- gopact:doc-language: zh -->

[英文文档](./SECURITY.md)

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
