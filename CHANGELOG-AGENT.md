# Agent Change Log

## [11] v2: persist Cline account pool in Postgres

- Files: `internal/cline/kit/data.go`, `internal/cline/kit/data_test.go`, `internal/store/sync.go`, `internal/store/sync_test.go`, `CHANGELOG-v2.md`
- Reason: Cline's pool was outside the configured state directory and missing from Postgres file sync, so it could not be restored with Codex and Antigravity data.
- Test result: `go test ./internal/cline/kit ./internal/store ./internal/cline/app` passed; `go build -buildvcs=false ./cmd/zenflash-llm` passed.
- Rollback: `git revert 6f50ec9a1aaee07e939b87b8c2a50b0aa38be7df`
