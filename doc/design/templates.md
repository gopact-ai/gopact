# gopact Agent Template Design

<!-- gopact:doc-language: en -->

Chinese documentation: [templates_zh.md](templates_zh.md)

Agent template design. It covers ReAct, Agent-as-Tool, Dev Agent, human-in-the-loop, trajectory tests, and template ownership outside core.

## Dev Agent Process Evidence

Dev Agent templates record process evidence through `RecordProcessRecords`, build workflow evidence through `BuildWorkflowProcessRecords`, and persist workflow process checks through `RecordWorkflowProcessRecords`.

Release evidence is bundled through `BuildReleaseBundle`, represented as a `ReleaseBundle`, recorded with `RecordReleaseBundleCheck`, and emitted as `release_bundle` evidence.
