---
name: wip-auto
description: 'Autonomous execution mode for small, low-risk tasks. Runs Start -> Plan -> Implement in a single turn.'
agent: analyst
---

# Workflow: Autonomous Auto-Pilot

**Best for**: Documentation, unit test boilerplate, minor refactors.

## THE PROTOCOL

1. **Initialize**: Run /wip-start to initialize the branch state.
2. **Auto-Plan**: Use the wip-planner to create a spec.md with explicit tasks.
3. **Auto-Pilot**: Transition to the Implementer role and work through the tasks autonomously using this loop:

### Execution Loop:
1. **Next Task**: Run ~/.claude/ai-dev-workflow/bin/aidw task next . to get the next job.
2. **Safety Check**: Run ~/.claude/ai-dev-workflow/bin/aidw policy check . "<command>" before any execution.
3. **Act**:
   - If verdict is "allow": Execute the task autonomously.
   - If verdict is "prompt": Stop and ask the user for permission (Allow Once, Allow Always, Deny).
4. **Log**: Run ~/.claude/ai-dev-workflow/bin/aidw task done . <id> "<description>" after every successful step.
5. **Repeat**: Go back to step 1 until aidw task next returns "finished".

4. **Verify**: Run a final self-review pass against the spec's AC.
5. **Finalize**: Set the stage to pr-prep.

Stop & Alert: If any step fails or an unexpected error occurs, stop immediately and report the error to the user.
