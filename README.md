# 🤖 ai-dev-workflow

**The Portable AI Engineering Kit for Professional Workspaces.**

[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/sebGilR/ai-dev-workflow)](https://goreportcard.com/report/github.com/sebGilR/ai-dev-workflow)
[![Homebrew](https://img.shields.io/badge/homebrew-tap-orange.svg)](https://github.com/sebGilR/homebrew-tap)

`ai-dev-workflow` is a vendor-agnostic framework that bridges **Claude Code** and **GitHub Copilot**, bringing professional-grade AI engineering patterns to your local VS Code environment. It keeps your working state portable, your conventions consistent, and your AI agents context-aware across every repository in your workspace.

---

## ⚡️ Key Features

*   🚀 **Native Integration**: Adds powerful `/wip-*` slash commands to Claude Code.
*   📂 **Portable WIP State**: Maintains branch-scoped workflow state in a gitignored `.wip/` folder.
*   🧠 **Vendor-Agnostic Memory**: A semantic search layer that works across Claude, Gemini, and Copilot.
*   🔭 **Semantic Navigation**: Integrated with **Serena** for deep symbol lookup and call-chain tracing.
*   🏗️ **Multi-Repo Ready**: One-command bootstrapping for complex workspaces with dozens of repositories.
*   🛡️ **Hardened Permissions**: Managed `settings.json` with safe defaults for AI tool access.

---

## 🚀 Quick Start

### 1. Install the CLI

```bash
brew tap sebGilR/homebrew-tap
brew install aidw
```

### 2. Setup the Workspace

Initialize the global environment and bootstrap your current repository in one command:

```bash
aidw bootstrap --setup-shell --interactive .
```

### 3. Start Engineering

Open Claude Code and initialize your branch:

```bash
/wip-start
```

---

## 🛠 For Developers (Optional)

If you want to contribute to the skills or agents, clone the repository and use the `--source-path` flag to symlink them:

```bash
git clone https://github.com/sebGilR/ai-dev-workflow.git
aidw bootstrap --setup-shell --source-path ./ai-dev-workflow .
```
---

## 🛠 The Skill Library

Our library of curated skills automates the entire development lifecycle:

| Command | Phase | Description |
| :--- | :--- | :--- |
| `/wip-start` | **Init** | Initialize branch state and seed documentation. |
| `/wip-plan` | **Design** | Use the `wip-planner` agent to draft an implementation path. |
| `/wip-research` | **Discovery**| Deep-dive into the codebase and gather context. |
| `/wip-implement` | **Build** | Execute the next chunk of work. |
| `/wip-review` | **Validate** | Run multi-source reviews and prioritize findings. |
| `/wip-pr` | **Deliver** | Synthesize work into a professional Pull Request draft. |
| `/wip-resume` | **Continuity**| Pick up exactly where you left off after a break. |

---

## 🤖 Specialized Subagents

`ai-dev-workflow` provisions specialized Claude subagents to handle complex cognitive tasks:

*   🔍 **`wip-researcher`**: High-precision file discovery and pattern matching.
*   📐 **`wip-planner`**: Architectural reasoning and task decomposition.
*   🛡️ **`wip-reviewer`**: Adversarial code review and security auditing.
*   🧪 **`wip-tester`**: Automated test generation and failure diagnosis.

---

## 🏗 Architecture

The workflow operates on a **Local-First, AI-Native** philosophy:

1.  **Global Layer (`~/.claude`)**: Stores shared skills, agents, and the vector database for memory.
2.  **Workspace Layer (`.wip/`)**: A gitignored, branch-specific directory that tracks progress, research notes, and execution logs.
3.  **Knowledge Layer (`.claude/repo-docs/`)**: Lightweight, persistent project context that keeps the AI aligned with your team's specific patterns.

---

## 🛠 Requirements

| Tool | Status | Purpose |
| :--- | :--- | :--- |
| **Go 1.21+** | Required | Powers the `aidw` CLI. |
| **Node.js** | Optional | Required for Context7 (API documentation). |
| **uv / uvx** | Optional | Required for Serena (Semantic navigation). |
| **Claude Code** | Recommended | The primary interface for slash commands. |

---

## 🤝 Contributing

This project is now public and we welcome contributions! Whether it's a new skill, a better agent prompt, or a CLI improvement:

1.  Fork the repo.
2.  Create your feature branch (`/wip-start`).
3.  Submit a Pull Request.

---

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.

---

<p align="center">
  Built with ❤️ for AI-native engineers.
</p>
