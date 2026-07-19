// Command vibexp is the VibeXP platform command-line interface.
package main

import (
	"context"
	"os"

	"github.com/vibexp/cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute(context.Background(), os.Args[1:]))
}
