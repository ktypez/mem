# Agent skills

This repository ships a portable `mem-service-ops` skill in the [Agent Skills format](https://agentskills.io/specification). The instructions are tool-neutral and work with compatible agent runtimes; the included MCP bridge is optional, not required.

## Install

From the repository root, run:

```sh
./scripts/install-agent-skill.sh
```

The default `all` target installs to the shared user-level skill directory for Codex/OpenCode (`~/.agents/skills`), Claude Code (`~/.claude/skills`), and Hermes (`$HERMES_HOME/skills/devops`, default `~/.hermes/skills/devops`). Choose one with `--target codex`, `--target opencode`, `--target claude`, or `--target hermes`. Codex and OpenCode share one copy here.

For a named Hermes profile, set `HERMES_HOME` to that profile's home. If a destination already has a different skill, the installer stops to preserve local changes. Review first, then use `--replace` intentionally; old copies are backed up under `~/.cache/mem-skill-backups/` or Hermes' cache. Start a new agent session if its skill list was already loaded.

You may also install directly into a repository-local discovery directory, which keeps the skill beside the project for all contributors:

- Codex: `.agents/skills/mem-service-ops/`
- OpenCode: `.opencode/skills/mem-service-ops/` or its documented compatible `.agents/skills/` location
- Claude Code: `.claude/skills/mem-service-ops/`
- Other compatible runtimes: use their documented skill discovery path

Copy `devops/mem-service-ops/` as a directory, preserving its `references/`, `scripts/`, `templates/`, and `agents/` subdirectories. Verify the skill is visible to the runtime before relying on it.

## Contents

- `devops/mem-service-ops/SKILL.md` — portable service workflow and safety rules.
- `devops/mem-service-ops/references/clients.md` — REST/MCP client guidance.
- `devops/mem-service-ops/scripts/verify-gate.sh` — optional live smoke test; creates and removes probes.
- `devops/mem-service-ops/templates/mem-mcp.py` — optional stdio MCP bridge.
- `devops/mem-service-ops/agents/openai.yaml` — optional Codex display metadata.

## Use with another agent

- Agent Skills compatibility loads instructions; it does not install/configure/authenticate mem.
- Use direct REST or a native integration when available. Use MCP only if the runtime supports or requires MCP.
- The bridge reads `MEM_SERVICE_URL`, `MEM_SOURCE`, and `MEM_API_TOKEN` from its process environment; it never reads Hermes-specific secret files and fails closed without a token.
- `MEM_SOURCE` is an asserted identity, not authentication. Keep sources outside `MEM_STEWARD_SOURCE` unless their writes should bypass the review inbox. Shared API-token auth does not independently verify the identity header.

## Verify

```sh
bash -n scripts/install-agent-skill.sh
./scripts/install-agent-skill.sh --help
```

For any additional runtime, test its documented discovery path with a fresh session before claiming it is supported.