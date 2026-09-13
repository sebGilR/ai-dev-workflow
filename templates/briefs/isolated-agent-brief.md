# Isolated Agent Brief

Ground rules for any dispatched, background, or worktree-isolated agent.
Paste or reference this brief in the dispatch prompt whenever a coordinator
hands work to an agent operating in its own checkout/worktree, separate from
the coordinator's own working tree.

- **Branch-check-first**: before editing anything, confirm you're on the
  branch/worktree you were told to work in (`git branch --show-current`,
  and `pwd` if a worktree path was specified). Do not assume — verify. If
  the branch doesn't match what the dispatch said, stop and report back
  rather than editing anyway.
- **No amend/rebase**: never `git commit --amend` or `git rebase` on a
  branch you don't own outright. Create new commits. Amending or rebasing
  shared/coordinator-owned history silently destroys other agents' work.
- **Commit + push per step, don't batch** — but only if the dispatch brief
  actually asked you to commit. Many dispatches leave committing to the
  coordinator; check first. If you were asked to commit, commit and push
  after each discrete step rather than accumulating a large uncommitted
  diff — smaller commits are easier to review and recover from.
- **Never `cd` to the primary/coordinating checkout.** You operate
  entirely within your assigned worktree path. Reading or writing under
  another session's working tree — even read-only — can race with that
  session's own edits.
- **Quote, don't paraphrase, spec text.** When implementing against a
  spec/plan/design document, quote the relevant text verbatim with a
  `file:line` citation rather than restating it from memory. Paraphrasing
  hides drift between what the spec says and what you built; a verbatim
  quote makes that drift auditable later.
- **Never pin `main`'s SHA** in scripts/config the way you might pin a
  dependency version. `main` is a moving target — referencing it by
  branch name (not a frozen SHA) is intentional, and freezing it defeats
  the point of tracking the branch.
- **Premise-check before editing.** Verify the file/symbol/behavior
  you're about to change still exists and matches what the dispatch brief
  claims — grep or read it first. Dispatch briefs can go stale between
  being written and being executed; don't build an edit on a premise you
  haven't confirmed.
- **Bound every long-running command.** Set an explicit timeout on tests,
  builds, or other slow commands, or use a polling/Monitor pattern. Never
  let a suite run unbounded — an agent that hangs waiting on output it
  will never get blocks the whole dispatch.
