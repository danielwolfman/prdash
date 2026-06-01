package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/danielwolfman/prdash/internal/auth"
	"github.com/danielwolfman/prdash/internal/config"
	"github.com/danielwolfman/prdash/internal/doctor"
	ghapi "github.com/danielwolfman/prdash/internal/github"
	"github.com/danielwolfman/prdash/internal/model"
	"github.com/danielwolfman/prdash/internal/tui"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	var configPath string
	var limitOverride int

	root := &cobra.Command{
		Use:   "prdash",
		Short: "A dense terminal dashboard for authored GitHub PRs",
		RunE: func(cmd *cobra.Command, args []string) error {
			dashboard, err := loadDashboard(cmd, configPath, limitOverride)
			if err != nil {
				return err
			}
			program := tea.NewProgram(tui.New(dashboard), tea.WithAltScreen(), tea.WithMouseCellMotion())
			_, err = program.Run()
			return err
		},
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file path")
	root.Flags().IntVar(&limitOverride, "limit", 0, "override max visible PRs for this run")

	root.AddCommand(configCommand(&configPath))
	root.AddCommand(authCommand())
	root.AddCommand(doctorCommand(&configPath))

	return root
}

func loadDashboard(cmd *cobra.Command, configPath string, limitOverride int) (tui.Dashboard, error) {
	path, err := config.ResolvePath(configPath)
	if err != nil {
		return tui.Dashboard{}, err
	}
	if err := config.EnsureExists(path); err != nil {
		return tui.Dashboard{}, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return tui.Dashboard{}, err
	}
	if limitOverride > 0 {
		cfg.Limits.MaxVisiblePRs = limitOverride
	}

	status, err := auth.Status(cmd.Context(), cfg.GitHub.Host)
	if err != nil {
		return tui.Dashboard{}, err
	}
	if !status.HasRequiredScopes() {
		return tui.Dashboard{}, fmt.Errorf("missing GitHub token scopes: %s\nrun: %s", strings.Join(status.MissingScopes(), ", "), auth.RefreshScopesCommand(cfg.GitHub.Host))
	}

	token, err := auth.Token(cmd.Context(), cfg.GitHub.Host)
	if err != nil {
		return tui.Dashboard{}, err
	}

	client := ghapi.NewClient(token)
	searchLimit := maxInt(cfg.Limits.MaxVisiblePRs*2, cfg.Limits.MaxVisiblePRs)
	if searchLimit <= 0 {
		searchLimit = 40
	}
	if searchLimit > 100 {
		searchLimit = 100
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Loading up to %d authored PRs from GitHub...\n", cfg.Limits.MaxVisiblePRs)
	prs, err := client.SearchAuthoredOpenPRs(cmd.Context(), searchLimit)
	if err != nil {
		return tui.Dashboard{}, err
	}

	rows, excluded := buildRows(cmd, client, prs, cfg)
	return tui.Dashboard{
		User:            status.Account,
		SnapshotAt:      time.Now(),
		Rows:            rows,
		TotalDiscovered: len(prs),
		ExcludedCount:   excluded,
		Symbols:         cfg.UI.Symbols,
		Animations:      cfg.UI.Animations,
		AnimationFPS:    cfg.UI.AnimationFPS,
	}, nil
}

func buildRows(cmd *cobra.Command, client *ghapi.Client, prs []model.PullRequest, cfg config.Config) ([]tui.Row, int) {
	maxVisible := cfg.Limits.MaxVisiblePRs
	if maxVisible <= 0 {
		maxVisible = 40
	}

	rows := make([]tui.Row, 0, maxVisible)
	excluded := 0
	for _, pr := range prs {
		if config.RepoExcluded(pr.RepoFullName, cfg.Filters.ExcludeRepos) {
			excluded++
			continue
		}
		if len(rows) >= maxVisible {
			break
		}
		rows = append(rows, tui.Row{PR: pr})
	}
	fetchRows(cmd, client, rows, cfg.Limits.MaxConcurrentRequests)
	return rows, excluded
}

func fetchRows(cmd *cobra.Command, client *ghapi.Client, rows []tui.Row, concurrency int) {
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

			runs, err := client.CurrentWorkflowRunsWithJobs(cmd.Context(), rows[i].PR)
			if err != nil {
				rows[i].FetchError = err.Error()
				return
			}
			rows[i].Runs = runs
		}(i)
	}
	wg.Wait()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
