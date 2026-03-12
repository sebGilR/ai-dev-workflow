package main

import (
	"os"

	"aidw/cmd/aidw/cmd"
)

func main() {
	if err := cmd.Root.Execute(); err != nil {
		os.Exit(1)
	}
}
