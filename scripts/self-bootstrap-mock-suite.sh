#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

git diff --check
go mod tidy
git diff --exit-code -- go.mod go.sum
./scripts/public-readiness-check.sh

go test -count=1 ./...
go test -run '^Example' ./...
go test -count=1 ./graph ./gopacttest/graphconformance
go test -count=1 ./checkpoint ./gopacttest/checkpointconformance
go test -count=1 . ./provider ./gopacttest/providerconformance
go test -count=1 ./tools ./gopacttest/toolconformance
go test -count=1 ./mcp
go test -count=1 ./a2a ./gopacttest/a2aconformance
go test -count=1 -run ExampleNewHTTPRegistryHandler ./a2a
go test -count=1 ./a2a -run TestMeshSyncEvery
go test -count=1 ./a2a -run TestMeshSyncEnvEvery
go test -count=1 ./cmd/gopact
go test -count=1 -run Channel . ./gopacttest
go test -count=1 . ./sandbox ./gopacttest/secretconformance ./gopacttest/promptinjectionconformance
go test -count=1 ./gopacttest
go test -count=1 -run SelfBootstrap ./gopacttest
