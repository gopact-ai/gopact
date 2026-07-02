# gopact Extension Conformance

<!-- gopact:doc-language: en -->

Chinese documentation: [extension-conformance-template_zh.md](extension-conformance-template_zh.md)

Template for extension conformance documentation. External modules use it to declare target kind, required suites, CI commands, examples, and security boundaries.

## Extension Target

Declare the target name, kind, source paths, required suites, examples, and ownership.

## Required Suites

List each conformance suite and its runnable command.

## CI Commands

Document required checks such as `git diff --check`, `go mod tidy && git diff --exit-code`, `go test -count=1 ./...`, and `go vet ./...`.

## Examples

Link runnable examples that exercise the target.

## Integration Tests

Real-service integration tests must stay opt-in and must not run in default CI.

## Security Boundary

Document how credentials, prompts, artifacts, and provider responses are redacted or avoided.
