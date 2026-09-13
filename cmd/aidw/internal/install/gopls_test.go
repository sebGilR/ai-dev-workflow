package install

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func lookPathStub(present map[string]bool) lookPathFunc {
	return func(name string) (string, error) {
		if present[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestDetectGoplsNoGoToolchain(t *testing.T) {
	var out bytes.Buffer
	status := detectGopls(false, &out, lookPathStub(nil), strings.NewReader(""), nil)

	if status.GoPresent {
		t.Error("expected GoPresent=false when go is not on PATH")
	}
	if status.Warning != "" {
		t.Errorf("expected no warning when go is absent, got %q", status.Warning)
	}
}

func TestDetectGoplsAlreadyPresent(t *testing.T) {
	var out bytes.Buffer
	status := detectGopls(true, &out, lookPathStub(map[string]bool{"go": true, "gopls": true}), strings.NewReader(""), nil)

	if !status.GoPresent || !status.GoplsPresent {
		t.Errorf("expected both present, got %+v", status)
	}
	if status.Warning != "" {
		t.Errorf("expected no warning when gopls is present, got %q", status.Warning)
	}
	if out.Len() != 0 {
		t.Errorf("expected no prompt output, got %q", out.String())
	}
}

func TestDetectGoplsMissingNonInteractive(t *testing.T) {
	var out bytes.Buffer
	status := detectGopls(false, &out, lookPathStub(map[string]bool{"go": true}), strings.NewReader(""), nil)

	if !status.GoPresent || status.GoplsPresent {
		t.Errorf("expected go present, gopls absent, got %+v", status)
	}
	if status.Warning == "" {
		t.Error("expected a warning when gopls is missing")
	}
	if status.Installed {
		t.Error("must never install when not interactive")
	}
	if out.Len() != 0 {
		t.Errorf("must not prompt when not interactive, got %q", out.String())
	}
}

func TestDetectGoplsMissingInteractiveDeclines(t *testing.T) {
	var out bytes.Buffer
	installCalled := false

	status := detectGopls(true, &out, lookPathStub(map[string]bool{"go": true}), strings.NewReader("n\n"), func(w io.Writer) error {
		installCalled = true
		return nil
	})

	if installCalled {
		t.Error("declining the prompt must not invoke the installer")
	}
	if status.Installed {
		t.Error("status.Installed must be false when the user declines")
	}
	if status.Warning == "" {
		t.Error("expected a warning to remain after declining")
	}
	if !strings.Contains(out.String(), "Install gopls now") {
		t.Errorf("expected a prompt to be printed, got %q", out.String())
	}
}

func TestDetectGoplsMissingInteractiveAccepts(t *testing.T) {
	var out bytes.Buffer
	installCalled := false
	present := map[string]bool{"go": true}

	status := detectGopls(true, &out, lookPathStub(present), strings.NewReader("y\n"), func(w io.Writer) error {
		installCalled = true
		// Simulate `go install` making gopls resolvable on PATH.
		present["gopls"] = true
		return nil
	})

	if !installCalled {
		t.Error("accepting the prompt must invoke the installer")
	}
	if !status.Installed {
		t.Error("status.Installed must be true after a successful install that lands gopls on PATH")
	}
	if status.Warning != "" {
		t.Errorf("expected no warning after a successful install, got %q", status.Warning)
	}
}

func TestDetectGoplsInstallSucceedsButNotOnPath(t *testing.T) {
	var out bytes.Buffer

	// go install can report success while placing the binary somewhere
	// not on PATH (e.g. GOBIN unset and GOPATH/bin missing from PATH) —
	// the stub deliberately never adds "gopls" to simulate that.
	status := detectGopls(true, &out, lookPathStub(map[string]bool{"go": true}), strings.NewReader("y\n"), func(w io.Writer) error {
		return nil
	})

	if status.Installed {
		t.Error("status.Installed must be false when gopls still isn't resolvable after install")
	}
	if !strings.Contains(status.Warning, "not on PATH") {
		t.Errorf("expected a PATH warning, got %q", status.Warning)
	}
}

func TestDetectGoplsMissingInteractiveInstallFails(t *testing.T) {
	var out bytes.Buffer

	status := detectGopls(true, &out, lookPathStub(map[string]bool{"go": true}), strings.NewReader("yes\n"), func(w io.Writer) error {
		return errors.New("network unreachable")
	})

	if status.Installed {
		t.Error("status.Installed must be false when the installer errors")
	}
	if !strings.Contains(status.Warning, "network unreachable") {
		t.Errorf("expected the install error surfaced in the warning, got %q", status.Warning)
	}
}
