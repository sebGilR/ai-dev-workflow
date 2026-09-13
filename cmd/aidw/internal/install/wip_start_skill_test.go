package install

import (
	"strings"
	"testing"

	embedfs "aidw"
)

// --- Cluster J Task 10: regression tests for the /wip-start freeform wiring

// freeformHeading is the literal heading D1-REVISED's new trailing section
// in claude/skills/wip-start/SKILL.md begins with.
const freeformHeading = "## Freeform mode (only when explicitly requested)"

// wipStartSkillDeliveryGolden is a byte-for-byte golden of
// claude/skills/wip-start/SKILL.md's content BEFORE the freeform heading —
// the frontmatter plus the 7 existing delivery steps — captured at Task 9's
// revision time. This is the strongest available form of "(b) delivery
// unaffected": it catches any accidental edit to the existing lines, not
// just the absence of specific substrings.
const wipStartSkillDeliveryGolden = `---
name: wip-start
description: Initialize the branch-scoped .wip/<branch>/ folder and seed all workflow files.
---

When this skill is used:

1. Detect the current repo root.
2. Run:

` + "```bash" + `
~/.claude/ai-dev-workflow/bin/aidw start .
` + "```" + `

3. Read the resulting ` + "`status.json`" + ` and ` + "`context.md`" + `.
4. **Project Intelligence (JIT)**:
   - If ` + "`.claude/repo-docs/`" + ` is empty or missing, run ` + "`/wip-document-project`" + ` to perform a deep research pass and generate core documentation.
   - Otherwise, refresh the semantic memory index:
     ` + "```bash" + `
     ~/.claude/ai-dev-workflow/bin/aidw memory index . .claude/repo-docs/
     ` + "```" + `
5. Summarize what was initialized and the project intelligence status.
6. Suggest running ` + "`/wip-plan`" + ` to begin the spec-driven planning sequence (Clarify -> Draft -> Skeptic Review).
7. Continue the conversation from the initialized workflow state.

`

func readWipStartSkill(t *testing.T) string {
	t.Helper()
	data, err := embedfs.FS.ReadFile("claude/skills/wip-start/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded wip-start SKILL.md: %v", err)
	}
	return string(data)
}

// TestWipStartSkill_FreeformSectionSkipsBootstrap proves the *instruction
// text* of the new freeform section never tells an agent to run any of the
// bootstrap-coupled commands/skill-steps — it cannot prove an agent obeys
// its own skill file (an agent ignoring the file is a host-level failure
// mode out of scope for this cluster), only that the text itself is
// correct.
func TestWipStartSkill_FreeformSectionSkipsBootstrap(t *testing.T) {
	content := readWipStartSkill(t)
	idx := strings.Index(content, freeformHeading)
	if idx == -1 {
		t.Fatalf("SKILL.md does not contain the freeform heading %q", freeformHeading)
	}
	freeformSection := content[idx:]

	for _, forbidden := range []string{"aidw start .", "aidw bootstrap", "wip-document-project", "memory index"} {
		if strings.Contains(freeformSection, forbidden) {
			t.Errorf("freeform section unexpectedly contains %q:\n%s", forbidden, freeformSection)
		}
	}
	if !strings.Contains(freeformSection, "aidw work start --mode freeform") {
		t.Errorf("freeform section does not contain the expected invocation %q", "aidw work start --mode freeform")
	}
}

// TestWipStartSkill_DeliverySectionUnchanged proves half (b) of D1-REVISED:
// the delivery-mode section of the same file — everything before the new
// freeform heading — is byte-for-byte identical to what it was before Task
// 9's edit (a literal golden, not just a substring check, so it also
// catches an accidental edit to the existing lines).
func TestWipStartSkill_DeliverySectionUnchanged(t *testing.T) {
	content := readWipStartSkill(t)
	idx := strings.Index(content, freeformHeading)
	if idx == -1 {
		t.Fatalf("SKILL.md does not contain the freeform heading %q", freeformHeading)
	}
	deliverySection := content[:idx]

	if deliverySection != wipStartSkillDeliveryGolden {
		t.Errorf("delivery-mode section of SKILL.md changed:\n--- got ---\n%s\n--- want ---\n%s", deliverySection, wipStartSkillDeliveryGolden)
	}
	// Redundant, human-readable cross-check in case the golden constant
	// itself is ever edited incorrectly later.
	if !strings.Contains(deliverySection, "aidw start .") {
		t.Error("delivery section no longer contains 'aidw start .'")
	}
	if !strings.Contains(deliverySection, "memory index") {
		t.Error("delivery section no longer contains the JIT 'memory index' step")
	}
}
