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

Milestone 2 is complete: CLI skeleton, config defaults, GitHub CLI auth inspection, doctor checks, GitHub GraphQL PR discovery, REST Actions workflow/job fetching, status normalization, and mocked API tests. The TUI is not implemented yet.

## Development

```sh
go test ./...
go run ./cmd/prdash doctor
```
