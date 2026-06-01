package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/danielwolfman/prdash/internal/auth"
	"github.com/danielwolfman/prdash/internal/config"
)

func Run(ctx context.Context, explicitConfigPath string) (string, error) {
	var b strings.Builder

	fmt.Fprintln(&b, "prdash doctor")

	path, err := config.ResolvePath(explicitConfigPath)
	if err != nil {
		fmt.Fprintf(&b, "config path: fail (%v)\n", err)
	} else {
		fmt.Fprintf(&b, "config path: %s\n", path)
	}

	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(&b, "gh: fail (GitHub CLI not found)")
		return b.String(), nil
	}
	fmt.Fprintln(&b, "gh: ok")

	status, err := auth.Status(ctx, "github.com")
	if err != nil {
		fmt.Fprintf(&b, "auth: fail (%v)\n", err)
		return b.String(), nil
	}
	if status.Authenticated {
		fmt.Fprintf(&b, "auth: ok (%s)\n", status.Account)
	} else {
		fmt.Fprintln(&b, "auth: fail (not authenticated)")
	}

	if missing := status.MissingScopes(); len(missing) > 0 {
		fmt.Fprintf(&b, "scopes: fail (missing %s)\n", strings.Join(missing, ", "))
		fmt.Fprintf(&b, "scope refresh: %s\n", auth.RefreshScopesCommand("github.com"))
	} else {
		fmt.Fprintln(&b, "scopes: ok")
	}

	if _, err := auth.Token(ctx, "github.com"); err != nil {
		fmt.Fprintf(&b, "token: fail (%v)\n", err)
	} else {
		fmt.Fprintln(&b, "token: ok (not printed)")
	}

	return b.String(), nil
}
