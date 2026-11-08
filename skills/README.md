# Repository skills

The repository includes Hermes Agent skills that provide reusable installation and operational procedures for this project.

## Install

Install the skill directory from the repository root into the target Hermes home with the provided installer:

```sh
./scripts/install-hermes-skill.sh
```

It installs into `$HERMES_HOME/skills/devops/` or the default `$HOME/.hermes/skills/devops/`. For a named profile, set `HERMES_HOME` to that profile's Hermes home (for example, `$HOME/.hermes/profiles/<profile>`) before running it. If a different skill already exists at the destination, inspect it first; use `./scripts/install-hermes-skill.sh --replace` only when intentionally replacing it. The installer backs up a replaced copy under `$HERMES_HOME/cache/mem-skill-backups/`. Start a new Hermes session if the current session does not discover the newly installed skill.

## Included

- [`devops/mem-service-ops/SKILL.md`](devops/mem-service-ops/SKILL.md) — mem service deployment, operations, trust boundaries, graph regeneration, and backup/restore guidance.
- `devops/mem-service-ops/references/clients.md` — MCP client setup guidance.
- `devops/mem-service-ops/scripts/verify-gate.sh` — optional live service trust-gate smoke test; creates and removes probe records.
- `devops/mem-service-ops/templates/mem-mcp.py` — starter stdio MCP bridge for another agent harness.

The MCP bridge is a template; review it and configure authentication securely before use.

## Verify

```sh
bash -n scripts/install-hermes-skill.sh
HERMES_HOME="$HOME/.hermes" ./scripts/install-hermes-skill.sh
```

The installer is idempotent when the installed copy matches. It stops without replacing a differing copy unless `--replace` is explicitly provided.
