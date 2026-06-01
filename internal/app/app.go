package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/danielwolfman/prdash/internal/auth"
	"github.com/danielwolfman/prdash/internal/config"
	"github.com/danielwolfman/prdash/internal/doctor"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	var configPath string

	root := &cobra.Command{
		Use:   "prdash",
		Short: "A dense terminal dashboard for authored GitHub PRs",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "prdash TUI is not implemented yet. Run `prdash doctor` to verify local setup.")
			return nil
		},
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file path")

	root.AddCommand(configCommand(&configPath))
	root.AddCommand(authCommand())
	root.AddCommand(doctorCommand(&configPath))

	return root
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
