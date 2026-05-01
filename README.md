# 🤖 ai-dev-workflow

**The Professional AI Engineering Kit for Modern Workspaces.**

[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/sebGilR/ai-dev-workflow)](https://goreportcard.com/report/github.com/sebGilR/ai-dev-workflow)
[![Homebrew](https://img.shields.io/badge/homebrew-tap-orange.svg)](https://github.com/sebGilR/homebrew-tap)

`ai-dev-workflow` is a vendor-agnostic framework that transforms **Claude Code** and **GitHub Copilot** from "chat interfaces" into a cohesive **Local-First AI Engineering System**. It brings industry-standard software patterns (Design-first, Adversarial Review, Context Distillation) to your local environment, keeping your state portable and your AI context razor-sharp.

---

## ⚡️ The Core Pillars

### 1. 🧠 Persistent Intelligence
Unlike ephemeral chat sessions, `aidw` maintains a gitignored `.wip/` directory for every branch. Your research, implementation plans, and architectural decisions persist across tool restarts and session context resets.

### 2. 🔭 Deep Context & Navigation
Integrated with **Serena** (Semantic Navigation) and **Context7** (Live Documentation), your AI doesn't just "guess" code—it traces call chains, looks up symbol definitions, and reads the latest library documentation in real-time.

### 3. 🛡️ Professional Rigor
Go beyond basic code generation. The workflow enforces:
*   **Adversarial Review**: A specialized pass where a second model (Gemini/GPT/Codex) attempts to find flaws in the proposed implementation.
*   **Context Distillation**: Automatic summarization of branch state into compact `<2KB` digests, drastically reducing token usage and cost.
*   **Hardened Sandbox**: Pre-configured permission rules to protect sensitive files (`.env`, `secrets/`) from AI tools.

### 4. 🚀 Zero-Config Onboarding
Fully "cloneless" installation via Homebrew. Get a professional AI engineering environment set up in seconds, with intelligent defaults for model routing (Frontier vs. Efficient tiers).

---

## 🚀 Quick Start

### 1. Install the CLI

```bash
brew tap sebGilR/homebrew-tap
brew install aidw
```

### 2. Setup the Workspace

Initialize the global environment and bootstrap your current repository:

```bash
aidw bootstrap --setup-shell --interactive .
```

### 3. Start Engineering

Open Claude Code and initialize your branch:

```bash
/wip-start
```

---

## 🛠 The Skill Library

Curated skills that automate the "boring stuff" so you can focus on architecture:

| Command | Phase | Description |
| :--- | :--- | :--- |
| `/wip-start` | **Init** | Seed branch state, initialize `status.json`, and bridge GitHub context. |
| `/wip-plan` | **Design** | Architectural task decomposition using the `wip-planner` agent. |
| `/wip-research` | **Discovery**| Multi-vector codebase analysis (Grep + Serena + Live Docs). |
| `/wip-implement` | **Build** | Chunked implementation with automatic progress tracking in `execution.md`. |
| `/wip-review` | **Validate** | Multi-source review pass including adversarial auditing. |
| `/wip-pr` | **Deliver** | Synthesize professional PR drafts from your branch's WIP history. |
| `/wip-resume` | **Continuity**| Zero-latency session resumption using distilled context summaries. |

---

## 🤖 Specialized Subagents

`ai-dev-workflow` provisions specialized Claude subagents to handle complex cognitive tasks:

*   🔍 **`wip-researcher`**: High-precision discovery. "Find where we handle X and tell me the pattern."
*   📐 **`wip-planner`**: Strategic reasoning. "I need to migrate Y to Z. Propose a 5-step path."
*   🛡️ **`wip-reviewer`**: Adversarial audit. "Review this diff like a senior security engineer."
*   🧪 **`wip-tester`**: Quality assurance. "Write edge-case tests for this logic and verify coverage."

---

## 🏗 Built-in Optimizations

*   📉 **Token Compression**: Integrated with **RTK** to compress terminal output by 60-90%.
*   🎯 **Model Routing**: Automated tiering between high-reasoning **Frontier** models and fast, low-cost **Efficient** models.
*   🔄 **Automatic Refinement**: Branch notes are automatically distillation-summarized during stage transitions to keep the prompt length minimal.

---

## 🛠 For Developers (Optional)

If you want to contribute to the skills or agents, clone the repository and use the `--source-path` flag to symlink them:

```bash
git clone https://github.com/sebGilR/ai-dev-workflow.git
aidw bootstrap --setup-shell --source-path ./ai-dev-workflow .
```

---

## 🤝 Contributing

This project is built for the community. Whether it's a new skill, a better agent prompt, or a CLI improvement, we welcome your PRs!

1.  Fork the repo.
2.  Use `/wip-start` to work on your feature.
3.  Submit your PR.

---

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.

---

<p align="center">
  Built with ❤️ for AI-native engineers.
</p>
