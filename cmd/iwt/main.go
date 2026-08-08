package main

import (
	"os"

	"github.com/utahta/intellij-wt/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
