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

Milestone 7 is in progress: CLI skeleton, config defaults, `init`, `version`, repo filter commands, debug log commands, GitHub CLI auth inspection, doctor checks, GitHub GraphQL PR discovery, paginated REST Actions workflow/job fetching, status normalization, mocked API tests, a dense TUI that opens immediately while PR/job data streams in, PR/job selection, Chrome/browser opening for selected PRs and jobs, conservative live refresh, stale row markers, change indicators, guarded PR-level rerun of failed jobs, and immediate refresh after successful rerun requests are implemented. Job-level rerun controls are not implemented yet.

## Install

During private QA:

```sh
go install ./cmd/prdash
```

For a future public release, the intended install shape is:

```sh
go install github.com/danielwolfman/prdash/cmd/prdash@latest
```

## Setup

```sh
prdash init
prdash doctor
prdash auth status
prdash config list
prdash config exclude owner/repo
prdash config include owner/repo
prdash logs path
prdash logs tail --lines 80
prdash version
```

`prdash init` creates the default config without overwriting an existing file unless `--force` is passed. Rerun actions require the GitHub CLI token to have the `workflow` scope; `prdash doctor` prints the exact `gh auth refresh` command when scopes are missing.

Debug logs are enabled by default and write to the user cache directory unless `[logging].path` is set. Logs include startup/config state, loader refresh cycles, GitHub request method/status/duration, per-PR job fetch timing, rerun actions, and hot-refresh triggers. Tokens are redacted and PR titles are omitted by default.

## Development

```sh
go test ./...
go run ./cmd/prdash doctor
go run ./cmd/prdash
go run ./cmd/prdash --limit 3
go run ./cmd/prdash --limit 3 --allow-rerun
```

The default command opens the TUI immediately, discovers authored open PRs, then fills in current GitHub Actions jobs as background workers complete. It refreshes on a conservative interval derived from the configured rate budget, marks stale rows, and highlights status changes. Press `j`/`k` or arrows to move across PRs and visible jobs, `o` to open the selected PR or job in Chrome/browser, and `q` to quit. Use `--limit 3` for a faster local smoke test.

Rerun actions are disabled by default. Use `--allow-rerun` for one run, or set `[actions].allow_rerun = true` in the config. Press `r` on a selected PR to rerun failed jobs for completed workflow runs, then confirm with `Enter`/`y` or cancel with `Esc`/`n`. Runs that are still queued or in progress are not rerun. A successful rerun request wakes the loader immediately instead of waiting for the next scheduled refresh.
