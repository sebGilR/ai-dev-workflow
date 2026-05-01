package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var Version = "0.1.0-dev"

var Root = &cobra.Command{
	Use:     "aidw",
	Short:   "AI dev workflow CLI",
	Version: Version,
}

// PrintJSON marshals v as indented JSON and writes it to stdout.
func PrintJSON(v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

// Die writes msg to stderr and exits 1.
func Die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
