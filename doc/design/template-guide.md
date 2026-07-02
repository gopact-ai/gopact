# gopact Template Guide

<!-- gopact:doc-language: en -->

Chinese documentation: [template-guide_zh.md](template-guide_zh.md)

Guide for building external graph templates. It defines template boundaries, step export/resume, event and verification expectations, memory handling, and conformance.

## Template Boundary

Templates own orchestration. Core owns graph, events, runtime IDs, checkpoints, and verification contracts.

## Step Export and Resume

Templates must support stable step export and resume boundaries when they expose long-running or human-in-the-loop behavior.

## Events and Verification

Templates should emit meaningful graph events and record verification evidence for externally visible behavior.

## Memory and Side Effects

Memory writes, deferred effects, and external side effects must be explicit and replayable when possible.

## Conformance

Template behavior should be covered by conformance or golden trajectory tests.
