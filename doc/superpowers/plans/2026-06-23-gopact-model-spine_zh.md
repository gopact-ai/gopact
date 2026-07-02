# Gopact Model Spine Implementation Plan

<!-- gopact:doc-language: zh -->

[英文文档](./2026-06-23-gopact-model-spine.md)

## 中文

> **For agentic workers:** Continue from M1 Runtime Spine. Keep implementation provider-neutral and standard-library only unless a production adapter explicitly requires a dependency.

**Goal:** Build M2 Model Spine so `gopact` can route model calls through typed provider registries, route sets, fake providers, OpenAI-compatible adapters, structured route events, fallback decisions, error classification, and normalized usage metadata.

**Architecture:** Keep root `gopact` as shared model request/response/event contracts. Put provider registry and router in `provider`. Put concrete HTTP adapters under `adapters/model/*`. SDK still does not read config files; applications inject typed `provider.RouteSet` and registered providers.

## Scope

Implemented in this pass:

- root model contract fields: `ModelRequest.IDs`, `Model`, `RouteHint`, `Capabilities`, `Budget`, `Usage`, `ModelRoute`, `ModelResponse`;
- model route events on root `Event`;
- `provider.Registry`;
- `provider.Router`;
- `provider.RouteSet`, `Route`, `Candidate`, `FallbackPolicy`;
- `RetryDecision` and `FailoverDecision` contracts;
- provider `ErrorClass` and wrapping error classification;
- `provider.Fake` for tests and examples;
- OpenAI-compatible non-streaming chat completion adapter;
- positive and negative tests for registry, routing, fallback, capability filtering, errors, fake provider, and adapter HTTP behavior.

Not completed in this pass:

- streaming delta adapter semantics;
- health scoring and weighted routing;
- token/cost estimation before provider response;
- provider-specific route metadata policies;
- unified plugin registration hooks for model middleware;
- production OpenAI/Anthropic/Gemini adapters.

## Verification

Commands used:

```bash
go test ./... -count=1
```

Follow-up gate before closing M2:

```bash
go vet ./...
git diff --check
```
