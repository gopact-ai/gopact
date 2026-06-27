# Security Policy

## Supported Versions

`gopact` is pre-v1 and currently private. Security fixes target the `main` branch
until a public release line is declared.

## Reporting a Vulnerability

Do not open a public issue for suspected vulnerabilities while the repository is
private. Report privately to the maintainers through the gopact-ai organization
owner channel or the private security contact used for the repository.

Include:

- affected package or command
- reproduction steps
- impact and trust boundary
- whether secrets, prompts, tool payloads, artifacts, or external tokens may be
  exposed

## Handling Guidelines

- Do not include secrets, tokens, raw prompts, raw model responses, raw tool
  args/results, or private customer data in issues, tests, or logs.
- Prefer redacted fixtures and shape metadata.
- Security-sensitive changes must preserve policy, redaction, sandbox,
  verification, and release-gate evidence boundaries.
