package main

import (
	"fmt"
	"os"

	"github.com/amterp/beagle/internal/cli"
)

func main() {
	app := cli.New(os.Stdout, os.Stderr)
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
