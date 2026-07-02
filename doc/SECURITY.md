# Security Policy

<!-- gopact:doc-language: en -->

Chinese documentation: [SECURITY_zh.md](SECURITY_zh.md)

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
