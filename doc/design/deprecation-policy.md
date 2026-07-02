# gopact Public API Deprecation Policy

<!-- gopact:doc-language: en -->

Chinese documentation: [deprecation-policy_zh.md](deprecation-policy_zh.md)

Public API deprecation policy. It defines stability states, Deprecated comments, migration windows, and review requirements before public API removal.

## Stability Levels

Public API groups are labeled with stability states such as `experimental`, `transitional`, `stable`, and `deprecated` in `public-api-boundary.json`.

## Deprecation Markers

Deprecated APIs must use Go doc comments that include `Deprecated:` and must point to a replacement when one exists. Coverage must stay aligned with `public-api-examples.json`.

## Removal Windows

Removal is not allowed before the documented migration window and release gate pass. Pre-v1 APIs may change faster than v1 APIs, but the change still needs an entry in the migration plan.

## Compatibility Review

Compatibility review checks `public-api-boundary.json`, `public-api-examples.json`, [versioning-policy.md](versioning-policy.md), and the current release gate before a public API is removed.
