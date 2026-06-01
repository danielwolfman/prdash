# prdash

`prdash` is a local terminal dashboard for GitHub pull requests authored by the authenticated user. The v1 goal is a dense, colorful TUI that shows open authored PRs, current-head GitHub Actions jobs, adaptive refresh state, and confirmed rerun actions.

This repository is private while v1 is being QAed. It should remain private until Daniel explicitly decides the v1 release is ready for public distribution.

## Planned v1 Shape

- Local terminal app built with Go and Bubble Tea.
- Authenticates through the GitHub CLI in v1.
- Shows open PRs authored by the logged-in GitHub user.
- Loads per-job check detail for the top 40 visible PRs, sorted by PR update time.
- Uses adaptive refresh scheduling to stay inside GitHub API budgets.
- Supports confirmed rerun actions when the token has `workflow` scope.
- Writes token-redacted debug logs by default.

## Current Status

Milestone 5 is in progress: CLI skeleton, config defaults, GitHub CLI auth inspection, doctor checks, GitHub GraphQL PR discovery, paginated REST Actions workflow/job fetching, status normalization, mocked API tests, a dense TUI that opens immediately while PR/job data streams in, conservative live refresh, stale row markers, change indicators, and guarded PR-level rerun of failed jobs are implemented. Job/workflow-level focus controls are not implemented yet.

## Development

```sh
go test ./...
go run ./cmd/prdash doctor
go run ./cmd/prdash
go run ./cmd/prdash --limit 3
go run ./cmd/prdash --limit 3 --allow-rerun
```

The default command opens the TUI immediately, discovers authored open PRs, then fills in current GitHub Actions jobs as background workers complete. It refreshes on a conservative interval derived from the configured rate budget, marks stale rows, and highlights status changes. Press `q` to quit. Use `--limit 3` for a faster local smoke test.

Rerun actions are disabled by default. Use `--allow-rerun` for one run, or set `[actions].allow_rerun = true` in the config. Press `r` on a selected PR to rerun failed jobs for completed workflow runs, then confirm with `Enter`/`y` or cancel with `Esc`/`n`. Runs that are still queued or in progress are not rerun.
