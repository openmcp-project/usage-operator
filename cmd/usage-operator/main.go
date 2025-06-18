package main

import (
	"context"
	"fmt"
	"os"

	"github.com/openmcp-project/usage-operator/cmd/usage-operator/app"
)

func main() {
	ctx := context.Background()
	defer ctx.Done()
	cmd := app.NewUsageOperatorCommand(ctx)

	if err := cmd.Execute(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}
