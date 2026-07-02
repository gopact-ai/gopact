# Repository Governance

<!-- gopact:doc-language: zh -->

[英文文档](./repository-governance.md)

## 中文

本文档定义公开仓库的维护规则。即使当前主要开发者只有一人，`main` 也必须通过 PR 和 CI 门禁更新；这样每次变更都有审计记录、测试证据和自动合并状态。

## Pull Request Flow

`main` 分支要求：

- 只能通过 pull request 更新；
- required status checks 必须全部通过；
- branch 必须基于最新 `main`；
- 禁止 force push 和删除；
- 保持线性历史；
- 只允许 squash merge；
- 所有 conversation 必须 resolved。

必需检查：

- `hygiene`
- `unit`
- `race`
- `static`
- `coverage`
- `conformance`，仅 core 仓库
- `security`
- `test`
- `author-policy`

`author-policy` 的规则：

- admin 作者 PR：CI 全绿即可合并；
- 非 admin 作者 PR：最新 commit 必须至少有一名 admin approval；
- 审批过旧 commit 后继续 push，审批失效。

## Admin Auto-Merge

`admin-automerge` workflow 只对 admin 作者 PR 生效。它使用 squash auto-merge，并在 required checks 全绿后由 GitHub 完成合并。

该 workflow 不 checkout PR 代码，也不执行 PR 代码。非 admin PR 不自动开启 auto-merge，必须由 admin 审批后再合入。

仓库设置应保持：

- `allow_auto_merge: true`
- `allow_squash_merge: true`
- `allow_merge_commit: false`
- `allow_rebase_merge: false`
- `delete_branch_on_merge: true`
- secret scanning 和 push protection enabled
- Dependabot security updates enabled

## Public Release Checks

公开发布、调整主干规则或发布 tag 前，维护者应确认：

```bash
./scripts/public-readiness-check.sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
golangci-lint run ./...
go test -coverprofile=coverage.out ./...
govulncheck ./...
```

`public-readiness-check.sh` 扫描 tracked files 和 commit messages 中的高置信 secret pattern。脚本只输出文件名或 commit hash，不打印匹配到的 secret 内容。
