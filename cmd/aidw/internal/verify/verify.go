// Package verify implements the `verify` command — install health check.
package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	embedfs "aidw"
	"aidw/cmd/aidw/internal/config"
	"aidw/cmd/aidw/internal/memory"
	"aidw/cmd/aidw/internal/wip"
)

var (
	repoDocs      = []string{"architecture.md", "patterns.md", "commands.md", "testing.md", "gotchas.md"}
	copilotSkills = []string{
		"wip-start", "wip-plan", "wip-research", "wip-implement",
		"wip-review", "wip-fix-review", "wip-resume", "wip-pr",
		"wip-install", "wip-cleanup", "wip-clear", "wip-document-project",
		"wip-setup-brew", "wip-sync",
	}
	copilotAgents = []string{"wip-planner", "wip-researcher", "wip-reviewer", "wip-tester", "wip-analyst", "wip-skeptic"}
)

// CheckItem represents a single verification result.
type CheckItem struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "pass", "FAIL", "warn"
	Detail string `json:"detail,omitempty"`
}

// Results aggregates all check results.
type Results struct {
	Checks   []CheckItem `json:"checks"`
	Passed   int         `json:"passed"`
	Failed   int         `json:"failed"`
	Warnings int         `json:"warnings"`
	OK       bool        `json:"ok"`
}

// Run performs all installation health checks. workspacePath is optional.
func Run(workspacePath string) *Results {
	r := &Results{}

	addCheck := func(name string, passed bool, detail string, warn bool) {
		status := "pass"
		if !passed {
			if warn {
				status = "warn"
			} else {
				status = "FAIL"
			}
		}
		r.Checks = append(r.Checks, CheckItem{Name: name, Status: status, Detail: detail})
		switch status {
		case "pass":
			r.Passed++
		case "warn":
			r.Warnings++
		default:
			r.Failed++
		}
	}

	check := func(name string, passed bool, detail ...string) {
		d := ""
		if len(detail) > 0 {
			d = detail[0]
		}
		addCheck(name, passed, d, false)
	}
	warn := func(name string, passed bool, detail ...string) {
		d := ""
		if len(detail) > 0 {
			d = detail[0]
		}
		addCheck(name, passed, d, !passed)
	}

	// Source repo checks via embedded FS (templates) and install root
	checkEmbedded := func(path string) bool {
		_, err := embedfs.FS.Open(path)
		return err == nil
	}

	claudeHome, _ := os.UserHomeDir()
	claudeHome = filepath.Join(claudeHome, ".claude")
	installRoot := filepath.Join(claudeHome, "ai-dev-workflow")

	checkInstall := func(relPath string) bool {
		return fileExists(filepath.Join(installRoot, relPath))
	}

	check("source: install.sh", checkInstall("install.sh"))
	check("source: templates/global/settings.template.json",
		checkEmbedded("templates/global/settings.template.json"))
	check("source: templates/global/claude_managed_block.md",
		checkEmbedded("templates/global/claude_managed_block.md"))
	check("source: templates/vscode/tasks.template.json",
		checkEmbedded("templates/vscode/tasks.template.json"))
	check("source: templates/github/copilot-instructions.md",
		checkEmbedded("templates/github/copilot-instructions.md"))

	for _, doc := range repoDocs {
		check(fmt.Sprintf("source: templates/repo-docs/%s", doc),
			checkEmbedded("templates/repo-docs/"+doc))
	}

	// .github/copilot-instructions.md is in the install root
	check("source: .github/copilot-instructions.md",
		checkInstall(".github/copilot-instructions.md"))

	for _, skill := range copilotSkills {
		skillPath := filepath.Join(installRoot, "claude", "skills", skill, "SKILL.md")
		check(fmt.Sprintf("source: skill %s", skill), fileExists(skillPath))
	}
	for _, agent := range copilotAgents {
		agentPath := filepath.Join(installRoot, "claude", "agents", agent+".md")
		check(fmt.Sprintf("source: agent %s", agent), fileExists(agentPath))
	}

	// Install checks
	symlinkOk := fileExists(installRoot)
	detail := ""
	if symlinkOk {
		resolved, err := filepath.EvalSymlinks(installRoot)
		if err == nil {
			detail = resolved
		}
	} else {
		detail = "not found"
	}
	check("install: ~/.claude/ai-dev-workflow symlink", symlinkOk, detail)

	for _, skill := range copilotSkills {
		dest := filepath.Join(claudeHome, "skills", skill)
		ok := fileExists(filepath.Join(dest, "SKILL.md"))
		d := ""
		if ok {
			if info, err := os.Lstat(dest); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if resolved, err := filepath.EvalSymlinks(dest); err == nil {
					d = "symlink -> " + resolved
				}
			}
		} else {
			d = "not installed"
		}
		warn(fmt.Sprintf("install: skill %s", skill), ok, d)
	}

	for _, agent := range copilotAgents {
		dest := filepath.Join(claudeHome, "agents", agent+".md")
		warn(fmt.Sprintf("install: agent %s", agent), fileExists(dest))
	}

	// Settings
	settingsPath := filepath.Join(claudeHome, "settings.json")
	if fileExists(settingsPath) {
		data, err := os.ReadFile(settingsPath)
		if err == nil {
			var settings map[string]any
			if jerr := json.Unmarshal(data, &settings); jerr != nil {
				check("install: settings.json valid JSON", false, "parse error")
			} else {
				_, hasPerms := settings["permissions"]
				check("install: settings.json has permissions", hasPerms)
			}
		}
	} else {
		warn("install: settings.json exists", false)
	}

	// CLAUDE.md managed block
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
	if fileExists(claudeMD) {
		data, _ := os.ReadFile(claudeMD)
		warn("install: CLAUDE.md managed block",
			strings.Contains(string(data), "BEGIN AI-DEV-WORKFLOW MANAGED BLOCK"))
	} else {
		warn("install: CLAUDE.md exists", false)
	}

	// Global gitignore
	gitConfigOut, err := exec.Command("git", "config", "--global", "core.excludesfile").Output()
	if err == nil && strings.TrimSpace(string(gitConfigOut)) != "" {
		giPath := strings.TrimSpace(string(gitConfigOut))
		if strings.HasPrefix(giPath, "~") {
			home, _ := os.UserHomeDir()
			giPath = filepath.Join(home, giPath[1:])
		}
		if fileExists(giPath) {
			data, _ := os.ReadFile(giPath)
			gi := string(data)
			check("install: global gitignore has .wip/", strings.Contains(gi, ".wip/"))
			check("install: global gitignore has .claude/repo-docs/", strings.Contains(gi, ".claude/repo-docs/"))
		} else {
			warn("install: global gitignore file exists", false, giPath)
		}
	} else {
		warn("install: global gitignore configured", false)
	}

	// Workspace check
	if workspacePath != "" {
		ws, err := filepath.Abs(workspacePath)
		if err == nil && fileExists(ws) {
		repos, _ := wip.DetectRepos(ws)
			check(fmt.Sprintf("workspace: found %d repo(s)", len(repos)), len(repos) > 0)
			for _, repo := range repos {
				name := filepath.Base(repo)
				warn(fmt.Sprintf("workspace: %s .wip/", name), fileExists(filepath.Join(repo, ".wip")))
				warn(fmt.Sprintf("workspace: %s .claude/repo-docs/", name),
					fileExists(filepath.Join(repo, ".claude", "repo-docs")))
				warn(fmt.Sprintf("workspace: %s .github/copilot-instructions.md", name),
					fileExists(filepath.Join(repo, ".github", "copilot-instructions.md")))
				warn(fmt.Sprintf("workspace: %s .github/skills/wip-start/SKILL.md", name),
					fileExists(filepath.Join(repo, ".github", "skills", "wip-start", "SKILL.md")))
				warn(fmt.Sprintf("workspace: %s .github/agents/wip-planner.md", name),
					fileExists(filepath.Join(repo, ".github", "agents", "wip-planner.md")))
			}
		} else {
			check("workspace: path exists", false, workspacePath)
		}
	}

	// MCP / tool checks
	uvxOk := commandExists("uvx")
	npxOk := commandExists("npx")
	warn("mcp: uvx installed (for Serena)", uvxOk)
	warn("mcp: npx installed (for Context7)", npxOk)

	db, err := memory.Open()
	if err == nil {
		warn("mcp: vector extension loaded", db.VectorEnabled())
		db.Close()
	} else {
		warn("mcp: memory db access", false, err.Error())
	}

	mcpConfig := filepath.Join(claudeHome, "mcp.json")
	if fileExists(mcpConfig) {
		data, err := os.ReadFile(mcpConfig)
		if err == nil {
			var mcpData map[string]any
			if jerr := json.Unmarshal(data, &mcpData); jerr != nil {
				check("mcp: mcp.json valid JSON", false, "parse error")
			} else {
				servers, ok := mcpData["mcpServers"].(map[string]any)
				if !ok {
					warn("mcp: mcpServers has expected object structure", false,
						"mcpServers must be a JSON object")
					servers = map[string]any{}
				}
				_, hasSerena := servers["serena"]
				_, hasContext7 := servers["context7"]
				warn("mcp: serena configured", hasSerena)
				warn("mcp: context7 configured", hasContext7)
			}
		}
	} else {
		warn("mcp: mcp.json exists", false, "run installer or create ~/.claude/mcp.json manually")
	}

	// Adversarial review provider check
	checkAdversarialProvider(warn)

	r.OK = r.Failed == 0
	return r
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// checkAdversarialProvider adds a provider-specific warn check for adversarial review.
// Uses config.Load() to respect the same priority logic as the rest of the tool.
func checkAdversarialProvider(warn func(string, bool, ...string)) {
	cfg := config.Load()
	if !cfg.AdversarialReview {
		return
	}

	provider := cfg.ResolvedProvider()
	installed := false
	var checkCmd *exec.Cmd

	switch provider {
	case "gemini":
		installed = commandExists("gemini")
		warn("adversarial: gemini CLI installed", installed,
			"see https://github.com/google-gemini/gemini-cli")
		if installed {
			checkCmd = exec.Command("gemini", "--prompt", "ping", "--output-format", "text")
			checkCmd.Stdin = strings.NewReader("pong")
		}
	case "copilot":
		installed = commandExists("copilot")
		warn("adversarial: copilot CLI installed", installed,
			"see https://github.com/github/copilot-cli")
		if installed {
			checkCmd = exec.Command("copilot", "--prompt", "ping pong")
		}
	case "codex":
		installed = commandExists("codex")
		warn("adversarial: codex CLI installed", installed,
			"see https://github.com/openai/codex")
		if installed {
			checkCmd = exec.Command("codex", "ping")
			checkCmd.Stdin = strings.NewReader("pong")
		}
	default:
		warn(fmt.Sprintf("adversarial: unknown provider %q", provider), false,
			"valid values: gemini, copilot, codex")
		return
	}

	if !installed {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, checkCmd.Path, checkCmd.Args[1:]...)
	cmd.Stdin = checkCmd.Stdin

	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			warn(fmt.Sprintf("adversarial: %s functional check", provider), false, "timeout after 15s")
		} else {
			// Provide snippet of error output
			errMsg := strings.TrimSpace(string(out))
			if len(errMsg) > 200 {
				errMsg = errMsg[:197] + "..."
			}
			if errMsg == "" {
				errMsg = err.Error()
			}
			warn(fmt.Sprintf("adversarial: %s functional check", provider), false, errMsg)
		}
	} else {
		warn(fmt.Sprintf("adversarial: %s functional check", provider), true)
	}
}
