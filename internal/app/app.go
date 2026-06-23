package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/danielwolfman/prdash/internal/auth"
	"github.com/danielwolfman/prdash/internal/config"
	"github.com/danielwolfman/prdash/internal/doctor"
	ghapi "github.com/danielwolfman/prdash/internal/github"
	"github.com/danielwolfman/prdash/internal/hooks"
	logpkg "github.com/danielwolfman/prdash/internal/logging"
	"github.com/danielwolfman/prdash/internal/model"
	"github.com/danielwolfman/prdash/internal/tui"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func New() *cobra.Command {
	var configPath string
	var limitOverride int
	var allowRerun bool

	root := &cobra.Command{
		Use:   "prdash",
		Short: "A dense terminal dashboard for monitored GitHub PRs",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, err := loggerForConfig(configPath)
			if err != nil {
				return err
			}
			logger.Info("prdash_start", map[string]any{
				"version": Version,
				"commit":  Commit,
			})
			dashboard := tui.Dashboard{
				SnapshotAt:     time.Now(),
				Symbols:        "auto",
				Animations:     true,
				AnimationFPS:   6,
				Loader:         dashboardLoader(configPath, limitOverride, logger),
				ActionExecutor: actionExecutor(configPath, allowRerun, logger),
				ActionsEnabled: actionsEnabled(configPath, allowRerun),
				OpenURL:        openURL,
			}
			program := tea.NewProgram(tui.New(dashboard), tea.WithAltScreen(), tea.WithMouseCellMotion())
			_, err = program.Run()
			return err
		},
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file path")
	root.Flags().IntVar(&limitOverride, "limit", 0, "override max visible PRs for this run")
	root.Flags().BoolVar(&allowRerun, "allow-rerun", false, "enable confirmed GitHub Actions rerun commands for this run")

	root.AddCommand(initCommand(&configPath))
	root.AddCommand(configCommand(&configPath))
	root.AddCommand(authCommand())
	root.AddCommand(doctorCommand(&configPath))
	root.AddCommand(logsCommand(&configPath))
	root.AddCommand(versionCommand())

	return root
}

func loggerForConfig(configPath string) (*logpkg.Logger, error) {
	path, err := config.ResolvePath(configPath)
	if err != nil {
		return nil, err
	}
	if err := config.EnsureExists(path); err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	return logpkg.New(cfg.Logging)
}

func openURL(ctx context.Context, target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("empty URL")
	}
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"open", "-a", "Google Chrome", target}, {"open", target}}
	case "windows":
		candidates = [][]string{{"rundll32", "url.dll,FileProtocolHandler", target}}
	default:
		candidates = [][]string{
			{"google-chrome", target},
			{"google-chrome-stable", target},
			{"chromium", target},
			{"chromium-browser", target},
			{"xdg-open", target},
		}
	}
	var lastErr error
	for _, candidate := range candidates {
		cmd := exec.CommandContext(ctx, candidate[0], candidate[1:]...)
		if err := cmd.Start(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no browser command configured")
}

func actionsEnabled(configPath string, flagEnabled bool) bool {
	if flagEnabled {
		return true
	}
	path, err := config.ResolvePath(configPath)
	if err != nil {
		return false
	}
	if err := config.EnsureExists(path); err != nil {
		return false
	}
	cfg, err := config.Load(path)
	if err != nil {
		return false
	}
	return cfg.Actions.AllowRerun
}

func actionExecutor(configPath string, flagEnabled bool, logger *logpkg.Logger) tui.ActionExecutor {
	return func(ctx context.Context, request tui.ActionRequest) tui.ActionResult {
		result := tui.ActionResult{Request: request}
		logger.Info("action_start", map[string]any{
			"kind":           string(request.Kind),
			"owner":          request.Owner,
			"repo":           request.Repo,
			"pr_number":      request.PRNumber,
			"pr_title":       request.PRTitle,
			"run_ids":        request.RunIDs,
			"job_count":      request.JobCount,
			"workflow_count": request.WorkflowCount,
		})
		path, err := config.ResolvePath(configPath)
		if err != nil {
			result.Error = err.Error()
			logger.Error("action_error", map[string]any{"error": err.Error(), "kind": string(request.Kind)})
			return result
		}
		if err := config.EnsureExists(path); err != nil {
			result.Error = err.Error()
			logger.Error("action_error", map[string]any{"error": err.Error(), "kind": string(request.Kind)})
			return result
		}
		cfg, err := config.Load(path)
		if err != nil {
			result.Error = err.Error()
			logger.Error("action_error", map[string]any{"error": err.Error(), "kind": string(request.Kind)})
			return result
		}
		if !flagEnabled && !cfg.Actions.AllowRerun {
			result.Error = "rerun disabled by config"
			logger.Warn("action_error", map[string]any{"error": result.Error, "kind": string(request.Kind)})
			return result
		}

		status, err := auth.Status(ctx, cfg.GitHub.Host)
		if err != nil {
			result.Error = err.Error()
			logger.Error("action_error", map[string]any{"error": err.Error(), "kind": string(request.Kind)})
			return result
		}
		if !status.HasRequiredScopes() {
			result.Error = fmt.Sprintf("missing GitHub token scopes: %s; run: %s", strings.Join(status.MissingScopes(), ", "), auth.RefreshScopesCommand(cfg.GitHub.Host))
			logger.Warn("action_error", map[string]any{"error": result.Error, "kind": string(request.Kind)})
			return result
		}
		token, err := auth.Token(ctx, cfg.GitHub.Host)
		if err != nil {
			result.Error = err.Error()
			logger.Error("action_error", map[string]any{"error": err.Error(), "kind": string(request.Kind)})
			return result
		}

		client := ghapi.NewClient(token, ghapi.WithLogger(logger))
		switch request.Kind {
		case tui.ActionRerunFailedJobs:
			for _, runID := range request.RunIDs {
				if err := client.RerunFailedJobs(ctx, request.Owner, request.Repo, runID); err != nil {
					result.Error = err.Error()
					logger.Error("action_error", map[string]any{"error": err.Error(), "kind": string(request.Kind), "run_id": runID})
					return result
				}
			}
			result.Message = fmt.Sprintf("rerun requested for %d failed jobs across %d workflow runs on %s/%s#%d",
				request.JobCount,
				request.WorkflowCount,
				request.Owner,
				request.Repo,
				request.PRNumber,
			)
		default:
			result.Error = fmt.Sprintf("unsupported action %q", request.Kind)
			logger.Warn("action_error", map[string]any{"error": result.Error, "kind": string(request.Kind)})
		}
		if result.Error == "" {
			logger.Info("action_success", map[string]any{
				"kind":      string(request.Kind),
				"owner":     request.Owner,
				"repo":      request.Repo,
				"pr_number": request.PRNumber,
				"run_ids":   request.RunIDs,
			})
		}
		return result
	}
}

func dashboardLoader(configPath string, limitOverride int, logger *logpkg.Logger) tui.Loader {
	return func(ctx context.Context, refresh <-chan struct{}, events chan<- tui.LoadEvent) {
		path, err := config.ResolvePath(configPath)
		if err != nil {
			events <- tui.LoadEvent{Error: err.Error(), Done: true}
			return
		}
		events <- tui.LoadEvent{Message: "loading config"}
		if err := config.EnsureExists(path); err != nil {
			events <- tui.LoadEvent{Error: err.Error(), Done: true}
			return
		}
		cfg, err := config.Load(path)
		if err != nil {
			events <- tui.LoadEvent{Error: err.Error(), Done: true}
			return
		}
		if limitOverride > 0 {
			cfg.Limits.MaxVisiblePRs = limitOverride
		}
		logger.Info("loader_config", map[string]any{
			"config_path":      path,
			"max_visible_prs":  cfg.Limits.MaxVisiblePRs,
			"max_concurrency":  cfg.Limits.MaxConcurrentRequests,
			"rate_budget_pct":  cfg.Limits.TargetRateBudgetPercent,
			"log_path":         logger.Path(),
			"include_owners":   len(cfg.Filters.IncludeOwners),
			"include_authors":  len(authorFiltersFromConfig(cfg)),
			"exclude_patterns": len(cfg.Filters.ExcludeRepos),
			"hooks_enabled":    cfg.Hooks.Enabled,
			"hook_commands":    len(cfg.Hooks.Commands),
		})
		hookDispatcher, err := hooks.NewDispatcher(cfg, logger)
		if err != nil {
			events <- tui.LoadEvent{Error: err.Error(), Done: true}
			return
		}

		events <- tui.LoadEvent{Message: "checking GitHub CLI auth"}
		status, err := auth.Status(ctx, cfg.GitHub.Host)
		if err != nil {
			events <- tui.LoadEvent{Error: err.Error(), Done: true}
			return
		}
		if !status.HasRequiredScopes() {
			events <- tui.LoadEvent{Error: fmt.Sprintf("missing GitHub token scopes: %s; run: %s", strings.Join(status.MissingScopes(), ", "), auth.RefreshScopesCommand(cfg.GitHub.Host)), Done: true}
			return
		}

		token, err := auth.Token(ctx, cfg.GitHub.Host)
		if err != nil {
			events <- tui.LoadEvent{Error: err.Error(), Done: true}
			return
		}

		client := ghapi.NewClient(token, ghapi.WithLogger(logger))
		searchLimit := maxInt(cfg.Limits.MaxVisiblePRs*2, cfg.Limits.MaxVisiblePRs)
		if searchLimit <= 0 {
			searchLimit = 40
		}
		if searchLimit > 100 {
			searchLimit = 100
		}
		includeAuthors := authorFiltersFromConfig(cfg)
		refreshInterval := calculateRefreshInterval(cfg, cfg.Limits.MaxVisiblePRs)
		for {
			events <- tui.LoadEvent{User: status.Account, Message: fmt.Sprintf("discovering up to %d monitored PRs", cfg.Limits.MaxVisiblePRs), SnapshotAt: time.Now()}
			cycleStart := time.Now()
			prs, err := client.SearchAuthoredOpenPRs(ctx, searchLimit, cfg.Filters.IncludeOwners, includeAuthors)
			if err != nil {
				logger.Error("loader_search_error", map[string]any{"error": err.Error()})
				events <- tui.LoadEvent{Error: err.Error(), Done: true, RefreshInterval: refreshInterval, SnapshotAt: time.Now()}
				refreshed, waitErr := waitForRefresh(ctx, refresh, refreshInterval)
				if waitErr != nil {
					logger.Info("loader_stop", map[string]any{"error": waitErr.Error()})
					return
				}
				token, client = refreshGitHubToken(ctx, cfg.GitHub.Host, token, logger)
				if refreshed {
					logger.Info("loader_hot_refresh", nil)
					events <- tui.LoadEvent{Message: "hot refresh after load error", SnapshotAt: time.Now(), RefreshInterval: refreshInterval}
				}
				continue
			}

			hookDispatcher.ObserveLifecycles(ctx, lifecyclePRs(prs, cfg), func(ctx context.Context, pr model.PullRequest) (model.PullRequest, error) {
				return client.PullRequest(ctx, pr.RepoFullName, pr.Number)
			})
			rows, excluded := prepareRows(prs, cfg)
			refreshInterval = calculateRefreshInterval(cfg, len(rows))
			logger.Info("loader_discovered_prs", map[string]any{
				"discovered":         len(prs),
				"visible":            len(rows),
				"excluded":           excluded,
				"refresh_interval":   refreshInterval.String(),
				"search_limit":       searchLimit,
				"estimated_requests": estimateRefreshRequests(len(rows)),
			})
			events <- tui.LoadEvent{
				TotalDiscovered: len(prs),
				ExcludedCount:   excluded,
				Rows:            rows,
				ReplaceRows:     true,
				Message:         fmt.Sprintf("refreshing jobs for %d PRs", len(rows)),
				RefreshInterval: refreshInterval,
				SnapshotAt:      time.Now(),
			}
			for i := range rows {
				row := rows[i]
				events <- tui.LoadEvent{Row: &row, TotalDiscovered: len(prs), ExcludedCount: excluded, RefreshInterval: refreshInterval}
			}
			streamJobFetches(ctx, client, rows, cfg.Limits.MaxConcurrentRequests, len(prs), excluded, events, logger, hookDispatcher)
			events <- tui.LoadEvent{
				Done:            true,
				TotalDiscovered: len(prs),
				ExcludedCount:   excluded,
				SnapshotAt:      time.Now(),
				RefreshInterval: refreshInterval,
				Message:         fmt.Sprintf("loaded %d PRs", len(rows)),
			}
			logger.Info("loader_cycle_complete", map[string]any{
				"visible":     len(rows),
				"excluded":    excluded,
				"duration_ms": time.Since(cycleStart).Milliseconds(),
			})

			refreshed, err := waitForRefresh(ctx, refresh, refreshInterval)
			if err != nil {
				logger.Info("loader_stop", map[string]any{"error": err.Error()})
				return
			}
			if refreshed {
				logger.Info("loader_hot_refresh", nil)
				events <- tui.LoadEvent{Message: "hot refresh after rerun", SnapshotAt: time.Now(), RefreshInterval: refreshInterval}
			}
		}
	}
}

func prepareRows(prs []model.PullRequest, cfg config.Config) ([]tui.Row, int) {
	maxVisible := cfg.Limits.MaxVisiblePRs
	if maxVisible <= 0 {
		maxVisible = 40
	}

	rows := make([]tui.Row, 0, maxVisible)
	excluded := 0
	for _, pr := range prs {
		if !config.RepoAllowedByOwner(pr.RepoFullName, cfg.Filters.IncludeOwners) {
			excluded++
			continue
		}
		if config.RepoExcluded(pr.RepoFullName, cfg.Filters.ExcludeRepos) {
			excluded++
			continue
		}
		if len(rows) >= maxVisible {
			break
		}
		rows = append(rows, tui.Row{PR: pr, Loading: true})
	}
	return rows, excluded
}

func lifecyclePRs(prs []model.PullRequest, cfg config.Config) []model.PullRequest {
	filtered := make([]model.PullRequest, 0, len(prs))
	for _, pr := range prs {
		if !config.RepoAllowedByOwner(pr.RepoFullName, cfg.Filters.IncludeOwners) {
			continue
		}
		if config.RepoExcluded(pr.RepoFullName, cfg.Filters.ExcludeRepos) {
			continue
		}
		filtered = append(filtered, pr)
	}
	return filtered
}

func refreshGitHubToken(ctx context.Context, host, currentToken string, logger *logpkg.Logger) (string, *ghapi.Client) {
	token, err := auth.Token(ctx, host)
	if err != nil {
		logger.Warn("loader_token_refresh_error", map[string]any{"error": err.Error()})
		return currentToken, ghapi.NewClient(currentToken, ghapi.WithLogger(logger))
	}
	if token != currentToken {
		logger.Info("loader_token_refreshed", nil)
	}
	return token, ghapi.NewClient(token, ghapi.WithLogger(logger))
}

func streamJobFetches(ctx context.Context, client *ghapi.Client, rows []tui.Row, concurrency, totalDiscovered, excluded int, events chan<- tui.LoadEvent, logger *logpkg.Logger, hookDispatcher *hooks.Dispatcher) {
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > 4 {
		concurrency = 4
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i := range rows {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			row := rows[i]
			start := time.Now()
			row.Loading = false
			runs, err := client.CurrentWorkflowRunsWithJobs(ctx, row.PR)
			if err != nil {
				row.FetchError = err.Error()
				logger.Error("row_fetch_error", map[string]any{
					"repo":        row.PR.RepoFullName,
					"pr_number":   row.PR.Number,
					"pr_title":    row.PR.Title,
					"duration_ms": time.Since(start).Milliseconds(),
					"error":       err.Error(),
				})
				events <- tui.LoadEvent{Row: &row, TotalDiscovered: totalDiscovered, ExcludedCount: excluded}
				return
			}
			row.Runs = runs
			logger.Info("row_fetch_complete", map[string]any{
				"repo":        row.PR.RepoFullName,
				"pr_number":   row.PR.Number,
				"pr_title":    row.PR.Title,
				"runs":        len(runs),
				"jobs":        len(allWorkflowJobs(runs)),
				"duration_ms": time.Since(start).Milliseconds(),
			})
			hookDispatcher.Observe(ctx, row.PR, row.Runs)
			if hookDispatcher.WantsPullRequestActivity() {
				activities, err := client.PullRequestActivities(ctx, row.PR, 20)
				if err != nil {
					logger.Warn("pr_activity_fetch_error", map[string]any{
						"repo":      row.PR.RepoFullName,
						"pr_number": row.PR.Number,
						"error":     err.Error(),
					})
				} else {
					hookDispatcher.ObserveActivities(ctx, row.PR, activities)
				}
			}
			events <- tui.LoadEvent{Row: &row, TotalDiscovered: totalDiscovered, ExcludedCount: excluded}
		}(i)
	}
	wg.Wait()
}

func allWorkflowJobs(runs []model.WorkflowRun) []model.Job {
	var jobs []model.Job
	for _, run := range runs {
		jobs = append(jobs, run.Jobs...)
	}
	return jobs
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func calculateRefreshInterval(cfg config.Config, visibleRows int) time.Duration {
	minSeconds := cfg.Limits.MinRefreshIntervalSecond
	if minSeconds <= 0 {
		minSeconds = 30
	}
	maxSeconds := cfg.Limits.MaxRefreshIntervalSecond
	if maxSeconds <= 0 {
		maxSeconds = 300
	}
	budgetPercent := cfg.Limits.TargetRateBudgetPercent
	if budgetPercent <= 0 || budgetPercent > 100 {
		budgetPercent = 60
	}

	estimatedRequests := estimateRefreshRequests(visibleRows)
	allowedPerHour := 5000 * budgetPercent / 100
	calculatedSeconds := estimatedRequests * 3600 / maxInt(1, allowedPerHour)
	if calculatedSeconds < minSeconds {
		calculatedSeconds = minSeconds
	}
	if calculatedSeconds > maxSeconds {
		calculatedSeconds = maxSeconds
	}
	return time.Duration(calculatedSeconds) * time.Second
}

func estimateRefreshRequests(visibleRows int) int {
	if visibleRows <= 0 {
		return 2
	}
	// Each row needs one run-list request plus one or more job-list pages. Large
	// matrix workflows commonly spill past GitHub's 100-job page size. Hooked
	// PR activity adds one GraphQL request per row when configured.
	return 2 + visibleRows*6
}

func waitForRefresh(ctx context.Context, refresh <-chan struct{}, d time.Duration) (bool, error) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-refresh:
		return true, nil
	case <-timer.C:
		return false, nil
	}
}

func configCommand(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect or edit prdash configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.ResolvePath(*configPath)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "edit",
		Short: "Open the config file in $EDITOR",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.ResolvePath(*configPath)
			if err != nil {
				return err
			}
			if err := config.EnsureExists(path); err != nil {
				return err
			}
			editor := strings.TrimSpace(os.Getenv("EDITOR"))
			if editor == "" {
				return fmt.Errorf("EDITOR is not set; config path: %s", path)
			}
			edit := exec.CommandContext(cmd.Context(), editor, path)
			edit.Stdin = os.Stdin
			edit.Stdout = os.Stdout
			edit.Stderr = os.Stderr
			return edit.Run()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Print config path and repo filters",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config: %s\n", path)
			if len(cfg.Filters.IncludeOwners) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "include_owners: all")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "include_owners:")
				for _, owner := range cfg.Filters.IncludeOwners {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", owner)
				}
			}
			if len(cfg.Filters.IncludeAuthors) == 0 && len(cfg.Filters.IncludeAuthorRules) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "include_authors: authenticated user only")
			} else if len(cfg.Filters.IncludeAuthors) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "include_authors:")
				for _, author := range cfg.Filters.IncludeAuthors {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s (all included owners)\n", author)
				}
			}
			if len(cfg.Filters.IncludeAuthorRules) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "include_author:")
				for _, rule := range cfg.Filters.IncludeAuthorRules {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", rule.Author)
					for _, repo := range rule.Repos {
						fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", repo)
					}
				}
			}
			if len(cfg.Filters.ExcludeRepos) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "exclude_repos: none")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "exclude_repos:")
				for _, repo := range cfg.Filters.ExcludeRepos {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", repo)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "allow_rerun: %t\n", cfg.Actions.AllowRerun)
			fmt.Fprintf(cmd.OutOrStdout(), "hooks_enabled: %t\n", cfg.Hooks.Enabled)
			fmt.Fprintf(cmd.OutOrStdout(), "hook_commands: %d\n", len(cfg.Hooks.Commands))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "include-owner owner",
		Short: "Only include repositories owned by this user or organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			owner := strings.TrimSpace(args[0])
			if owner == "" {
				return fmt.Errorf("owner must not be empty")
			}
			added := config.AddIncludedOwner(&cfg, owner)
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if added {
				fmt.Fprintf(cmd.OutOrStdout(), "included owner %s\n", owner)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "owner %s already included\n", owner)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove-owner owner",
		Short: "Remove an owner from the include-owner filter",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			owner := strings.TrimSpace(args[0])
			if owner == "" {
				return fmt.Errorf("owner must not be empty")
			}
			removed := config.RemoveIncludedOwner(&cfg, owner)
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(cmd.OutOrStdout(), "removed owner %s\n", owner)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "owner %s was not included\n", owner)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "include-author author [owner/repo...]",
		Short: "Also include open PRs authored by this GitHub user or app",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			author := strings.TrimSpace(args[0])
			if author == "" {
				return fmt.Errorf("author must not be empty")
			}
			repos := args[1:]
			for _, repo := range repos {
				if !strings.Contains(strings.TrimSpace(repo), "/") {
					return fmt.Errorf("repo %q must be owner/repo", repo)
				}
			}
			added := config.AddIncludedAuthor(&cfg, author, repos...)
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if added {
				if len(repos) == 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "included author %s\n", author)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "included author %s for %d repos\n", author, len(repos))
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "author %s already included\n", author)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove-author author",
		Short: "Remove an author from the include-author filter",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			author := strings.TrimSpace(args[0])
			if author == "" {
				return fmt.Errorf("author must not be empty")
			}
			removed := config.RemoveIncludedAuthor(&cfg, author)
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(cmd.OutOrStdout(), "removed author %s\n", author)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "author %s was not included\n", author)
			}
			return nil
		},
	})

	rerunCmd := &cobra.Command{
		Use:   "rerun",
		Short: "Configure rerun actions",
	}
	rerunCmd.AddCommand(&cobra.Command{
		Use:   "enable",
		Short: "Enable confirmed rerun actions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			cfg.Actions.AllowRerun = true
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "rerun enabled")
			return nil
		},
	})
	rerunCmd.AddCommand(&cobra.Command{
		Use:   "disable",
		Short: "Disable rerun actions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			cfg.Actions.AllowRerun = false
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "rerun disabled")
			return nil
		},
	})
	cmd.AddCommand(rerunCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "exclude owner/repo",
		Short: "Exclude a repository from the dashboard",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			repo := strings.TrimSpace(args[0])
			if repo == "" {
				return fmt.Errorf("repo must not be empty")
			}
			added := config.AddExcludedRepo(&cfg, repo)
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if added {
				fmt.Fprintf(cmd.OutOrStdout(), "excluded %s\n", repo)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s already excluded\n", repo)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "include owner/repo",
		Short: "Remove a repository from the exclude list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			repo := strings.TrimSpace(args[0])
			if repo == "" {
				return fmt.Errorf("repo must not be empty")
			}
			removed := config.RemoveExcludedRepo(&cfg, repo)
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(cmd.OutOrStdout(), "included %s\n", repo)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s was not excluded\n", repo)
			}
			return nil
		},
	})

	return cmd
}

func loadConfigForEdit(configPath string) (string, config.Config, error) {
	path, err := config.ResolvePath(configPath)
	if err != nil {
		return "", config.Config{}, err
	}
	if err := config.EnsureExists(path); err != nil {
		return "", config.Config{}, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return "", config.Config{}, err
	}
	return path, cfg, nil
}

func authorFiltersFromConfig(cfg config.Config) []ghapi.AuthorFilter {
	filters := make([]ghapi.AuthorFilter, 0, len(cfg.Filters.IncludeAuthors)+len(cfg.Filters.IncludeAuthorRules))
	for _, author := range cfg.Filters.IncludeAuthors {
		filters = append(filters, ghapi.AuthorFilter{Author: author})
	}
	for _, rule := range cfg.Filters.IncludeAuthorRules {
		filters = append(filters, ghapi.AuthorFilter{Author: rule.Author, Repos: rule.Repos})
	}
	return filters
}

func initCommand(configPath *string) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a prdash config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.ResolvePath(*configPath)
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil && !force {
				fmt.Fprintf(cmd.OutOrStdout(), "config already exists: %s\n", path)
			} else {
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				if err := config.Save(path, config.Default()); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "created config: %s\n", path)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "auth scopes: %s\n", auth.RefreshScopesCommand("github.com"))
			fmt.Fprintln(cmd.OutOrStdout(), "run: prdash doctor")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config")
	return cmd
}

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print prdash version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "prdash %s\ncommit %s\nbuilt %s\n", Version, Commit, Date)
			return nil
		},
	}
}

func logsCommand(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Inspect prdash debug logs",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the active log file path",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			path, err := logpkg.ResolvePath(cfg.Logging.Path)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	})

	var lines int
	tail := &cobra.Command{
		Use:   "tail",
		Short: "Print the last log lines",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := loadConfigForEdit(*configPath)
			if err != nil {
				return err
			}
			path, err := logpkg.ResolvePath(cfg.Logging.Path)
			if err != nil {
				return err
			}
			entries, err := logpkg.Tail(path, lines)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				fmt.Fprintln(cmd.OutOrStdout(), entry)
			}
			return nil
		},
	}
	tail.Flags().IntVar(&lines, "lines", 80, "number of log lines to print")
	cmd.AddCommand(tail)

	return cmd
}

func authCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect GitHub CLI authentication",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show GitHub CLI auth status and required scope coverage",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := auth.Status(cmd.Context(), "github.com")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), status.String())
			if !status.HasRequiredScopes() {
				fmt.Fprintf(cmd.OutOrStdout(), "\nTo refresh scopes, run:\n  %s\n", auth.RefreshScopesCommand("github.com"))
			}
			return nil
		},
	})

	return cmd
}

func doctorCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Verify local prdash prerequisites",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := doctor.Run(cmd.Context(), *configPath)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), report)
			return nil
		},
	}
}
