# gopact Self-Bootstrap Roadmap

<!-- gopact:doc-language: en -->

Chinese documentation: [self-bootstrap-roadmap_zh.md](self-bootstrap-roadmap_zh.md)

Roadmap for making gopact self-bootstrapping. It defines staged goals, release evidence, testing standards, and the level at which gopact can maintain itself.

## Current Long-Running Phase Goal

The next product phase is to make gopact a production-grade, self-bootstrapping Agent SDK. The SDK must be able to run domain agents, compose them into an A2A agent mesh, validate provider behavior through mock and real-provider tests, and use the same runtime evidence to operate on the gopact repositories themselves.

The target state is not a single demo. It is a repeatable engineering loop:

- analyze a repository or agent cluster;
- produce a structured plan;
- apply controlled code or configuration changes;
- run tests and capture command evidence;
- preserve diff, policy, checkpoint, artifact, review, A2A task, and release-gate evidence;
- resume from stable checkpoints after approval, cancellation, or failure;
- publish a release bundle only when the mock CI gates and required local integration gates pass.

## Differentiation

gopact optimizes for verifiable agent engineering rather than opaque agent classes. Its core advantage must be low-friction, evidence-first composition: users should be able to create a vertical agent, expose an agent card, join an A2A mesh, and verify behavior without adopting a hosted platform or rewriting provider/template code.

This implies three product constraints:

- core owns stable contracts, local defaults, conformance kits, graph/workflow runtime, evidence, checkpoint/resume, policy, and A2A lifecycle semantics;
- `gopact-ext` owns production providers, storage, channel, registry, and template implementations behind those contracts;
- `gopact-examples` owns runnable workflows and agent clusters that double as smoke tests and user-facing usage references.

## Phase Slices

| Slice | Goal | Required proof |
| --- | --- | --- |
| P1 A2A lifecycle | Agent cards, readiness-aware discovery, lease registration, heartbeat renewal, call, stream, cancel, routing, policy, and auth | Offline A2A tests plus HTTP registry mock integration examples |
| P2 workflow orchestration | Branch, DAG fan-in, dynamic fan-out, loop limits, subgraph/runnable nodes, interrupt, checkpoint, and resume | Graph and checkpoint conformance plus golden trajectories |
| P3 agent templates | ReAct, Plan-Execute, Supervisor, Agent-as-Tool, and Dev Agent templates in `gopact-ext` | Template conformance, golden trajectories, failure-path tests |
| P4 provider validation | OpenAI-compatible, Agnes, Ark/OpenAI-compatible, streaming, tool calls, structured output, thinking controls, error classification, timeout, and cancel | CI mock tests plus local `.env` Agnes integration tests |
| P5 scaffold and examples | Environment-driven examples, dotenv support, agent cluster scaffold, generated agent run path, and mock provider path | Example smoke tests and scaffold tests |
| P6 self-bootstrap gate | Dev Agent can analyze, plan, apply, test, review, and assemble release evidence for gopact repositories | Release bundle validation and self-bootstrap release gate |

## Testing Standard

Coverage is a baseline, not a completion criterion. Every expected feature point must have tests that pin behavior, failure modes, and public API ergonomics.

CI must use only mock, fake, or local in-memory services. CI must not read `.env`, require real provider keys, or depend on public network availability.

Local integration must use `.env`-configured Agnes provider credentials when provider behavior is under test. Real-provider tests must run behind an explicit integration tag and must never print tokens, model identifiers, or secrets into logs, golden files, docs, or release evidence.

Required test classes:

- unit tests for validation, state transitions, and errors;
- contract tests for provider, tool, checkpoint, A2A, channel, memory, and template ports;
- conformance tests for `gopact-ext` implementations;
- golden trajectory tests for event ordering and run export shape;
- mock integration tests for CI-stable end-to-end behavior;
- local real-provider integration tests for Agnes-backed streaming, tools, structured output, thinking controls, timeout, cancel, and error classification.

## Completion Bar

This phase is complete only when:

- `doc/FEATURES.md` lists every completed capability with an offline proof command;
- core, ext, and examples CI pass on mock services;
- local Agnes integration has recent passing evidence for provider and template behavior;
- examples can start an agent cluster from environment variables and mock defaults;
- Dev Agent can produce a release bundle containing run export, replay plan, verification report, diff, command, review, policy, checkpoint, artifact, and A2A task evidence;
- secret scan confirms no provider token, model id, or local `.env` content leaked into tracked files.
