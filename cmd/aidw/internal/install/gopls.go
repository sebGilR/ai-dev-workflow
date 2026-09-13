package install

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// goplsInstallCmd is the fix instructions surfaced in warnings and prompts.
const goplsInstallCmd = "go install golang.org/x/tools/gopls@latest"

// GoplsStatus reports what DetectGopls found and, if it prompted, what it did.
type GoplsStatus struct {
	GoPresent    bool
	GoplsPresent bool
	Installed    bool
	Warning      string
}

// lookPathFunc matches exec.LookPath's signature so tests can stub it.
type lookPathFunc func(string) (string, error)

// DetectGopls checks whether the Go toolchain is present and, if so, whether
// gopls (the language server Serena needs for Go symbol lookups) is on PATH.
// Detection always runs, on both `aidw bootstrap` and `aidw upgrade`, so a
// missing gopls surfaces as a warning either way. Installing it is only ever
// offered interactively — `aidw upgrade` writes JSON to stdout and must not
// block on stdin.
func DetectGopls(interactive bool, w io.Writer) GoplsStatus {
	return detectGopls(interactive, w, exec.LookPath, os.Stdin, runGoInstallGopls)
}

func detectGopls(interactive bool, w io.Writer, lookPath lookPathFunc, stdin io.Reader, install func(io.Writer) error) GoplsStatus {
	status := GoplsStatus{}

	if _, err := lookPath("go"); err != nil {
		// No Go toolchain on this machine: gopls is moot, not a problem.
		return status
	}
	status.GoPresent = true

	if _, err := lookPath("gopls"); err == nil {
		status.GoplsPresent = true
		return status
	}

	status.Warning = fmt.Sprintf("gopls not found: Serena's Go language server won't start. Fix: %s", goplsInstallCmd)

	if !interactive {
		return status
	}

	fmt.Fprintln(w, "\nSerena uses gopls for Go symbol lookups, but it isn't installed.")
	fmt.Fprintf(w, "Install gopls now (%s)? [y/N]: ", goplsInstallCmd)

	reader := bufio.NewReader(stdin)
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	if ans != "y" && ans != "yes" {
		return status
	}

	if err := install(w); err != nil {
		status.Warning = fmt.Sprintf("gopls install failed: %v (fix manually: %s)", err, goplsInstallCmd)
		return status
	}

	// go install places the binary under `go env GOPATH`/bin (or GOBIN),
	// which is often not on PATH — re-check rather than trust the install
	// succeeded at making gopls actually runnable.
	if _, err := lookPath("gopls"); err != nil {
		status.Warning = "gopls installed but not on PATH: add $(go env GOPATH)/bin (or $GOBIN) to PATH"
		return status
	}

	status.Installed = true
	status.Warning = ""
	fmt.Fprintln(w, "  gopls installed.")
	return status
}

func runGoInstallGopls(w io.Writer) error {
	cmd := exec.Command("go", "install", "golang.org/x/tools/gopls@latest")
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}
