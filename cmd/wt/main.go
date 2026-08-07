package main

import (
	"fmt"
	"os"

	"github.com/utahta/intellij-wt/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "wt:", err)
		os.Exit(1)
	}
}
