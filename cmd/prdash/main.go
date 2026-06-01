package main

import (
	"context"
	"fmt"
	"os"

	"github.com/danielwolfman/prdash/internal/app"
)

func main() {
	if err := app.New().ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
