# prdash v1 Spec

## Goals

`prdash` is a general-purpose open-source local terminal dashboard for GitHub PRs authored by the authenticated user plus configured monitored authors. It should be useful for dogfooding first, then hardened through public early releases.

## Non-Goals

- Standalone OAuth device flow in v1.
- Review-request dashboard.
- Job-level rerun controls.
- Redacted support bundle command.
- OS notifications, sound, Slack, or daemon mode.
- GitHub Enterprise Server as an officially supported target.
- Required-check-only aggregation.

## Auth

v1 requires GitHub CLI authentication. The app reads `gh auth token --hostname github.com` at startup and keeps the token only in memory. It never auto-refreshes scopes. If required scopes are missing, it prints the exact `gh auth refresh` command for the user to run.

Required scopes:

- `repo`
- `read:org`
- `workflow`

## PR Discovery

Use GraphQL search for open monitored PRs:

```text
is:pr is:open author:@me archived:false sort:updated-desc
```

Configured monitored authors are searched with the same query shape and merged with authenticated-user results by latest update time.

Include drafts and mark them visually. Exclude archived repositories by default. Apply file-based repo exclusion patterns after discovery.

## Visible Scope

Default to the top 40 visible PRs sorted by PR `updated_at` descending. Load per-job data for every visible PR. If the budget cannot support fresh data for all rows, keep rows visible and mark stale rows clearly.

## Checks and Jobs

Use REST GitHub Actions APIs for workflow runs, jobs, and rerun actions. Current state is anchored to the PR head SHA. Show the newest/current attempt by default and hide previous attempts unless a future toggle reveals them.

Aggregate status uses all current-head jobs, not required-only checks. External checks are read-only when cheap to fetch; otherwise show an external-check marker.

## TUI

The main view is dense, colorful, and animated. It uses compact job chips for every PR and shows full job names/details on focus or expansion. Unicode is default with ASCII fallback.

Keyboard support is complete; mouse support is an enhancement. Clicking a rerun affordance opens confirmation, never executes directly.

## Rerun

Alpha keeps rerun disabled by default. v1 enables rerun by default only when the token has `workflow` scope. All rerun actions require confirmation.

Pressing `r` on a selected PR reruns failed jobs across all failed current-head workflow runs that are completed. Runs that are still queued or in progress are not rerun.

Whole-workflow rerun and individual job rerun are stronger deliberate actions outside the v1 milestone path.

## Rate Scheduling

The app cannot guarantee GitHub will never apply secondary limits, but it must never intentionally schedule beyond a conservative local budget. It uses rate-limit headers, low concurrency, adaptive intervals, stale row marking, and exponential backoff for secondary limits.

Default scheduler config:

```toml
[limits]
max_visible_prs = 40
target_rate_budget_percent = 60
min_refresh_interval_seconds = 30
max_refresh_interval_seconds = 300
max_concurrent_requests = 2
```

## Logging and Privacy

Debug logs are enabled by default, token-redacted, size-limited, and written to the platform cache directory. Logs include repo names and PR numbers, but omit PR titles by default.

Security posture:

- No telemetry.
- No token persistence by `prdash` in v1.
- No network calls except GitHub API for the configured host.
- Authorization headers are never logged.
- Rerun actions always require confirmation.

## Milestones

1. CLI skeleton, config defaults, GitHub CLI auth, doctor.
2. GitHub PR and Actions data model with mocked fixtures.
3. Static dense TUI.
4. Adaptive scheduler, live refresh, stale rows, animations.
5. Confirmed rerun actions.
6. Private QA hardening and release iterations.
