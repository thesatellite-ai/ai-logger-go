package main

import (
	"fmt"
	"os"

	"github.com/khanakia/ai-logger/internal/cli"
)

// Set by ldflags at build time (see Taskfile.yml).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cli.Version = version
	cli.Commit = commit
	cli.BuildDate = date
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
