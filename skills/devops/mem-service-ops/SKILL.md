---
name: mem-service-ops
description: Use when deploying or verifying the mem service.
version: 1.1.0
author: mem contributors
license: MIT
platforms: [linux]
metadata:
  hermes:
    tags: [mem, go, postgres, systemd, memory]
    related_skills: []
---

# mem service operations

Use this skill for changes, deployment, and troubleshooting in the mem repository. Follow the checked-in source and service units; do not assume host-specific paths or credentials.

## Prerequisites

- Linux host with Go matching `go.mod`, PostgreSQL, systemd, and repository access.
- Service configuration is provided through a protected environment file. Never print or commit secrets from it.
- Confirm the actual systemd unit name, binary path, listen address, and repo root before operating; installations may differ.

## Procedure

### Inspect and validate

1. Read `README.md`, `SECURITY.md`, relevant handlers, tests, and service/socket units for the change.
2. Check `git status` and preserve unrelated modifications. Stage only intended paths; never stage build artifacts or backups.
3. Before deploying, run `gofmt -l .`, `go vet ./...`, `go test ./...`, and `git diff --check`. Resolve every failure before touching the running service.

### Deploy a service change

1. Confirm the requested scope and any data/schema impact. Back up production data before schema changes or risky data operations.
2. Build a new binary from the validated source and retain the currently deployed binary for rollback.
3. Install the new binary using the host's established deployment process. Restart only the confirmed mem service unit when authorized; never guess the unit name.
4. Verify the service is active, check recent logs without exposing environment values, call `/health`, and exercise the changed authenticated API/UI route. Verify the live result, not just the build.
5. Keep the rollback binary until live checks pass. If verification fails, restore the prior binary and verify recovery.

## Trust and access model

- The service should bind to loopback. Do not bind it to `0.0.0.0`; use a deliberately scoped proxy/socket for approved network access.
- `/health` is unauthenticated. API routes use bearer auth when configured; the UI uses Basic Auth when configured. Keep the endpoint on trusted networks even with auth enabled.
- Writer identity is read from `X-Mem-Source`, then JSON `source`, else `agent`. Identities in `MEM_STEWARD_SOURCE` can create active nodes; other identities create proposed nodes. Treat writer identity as a trust boundary: a shared API token does not distinguish clients.
- Bulk deletion requires both `MEM_ENABLE_BULK_DELETE=true` and `X-Confirm-Delete: all`.

## Graph regeneration

For a requested regeneration, use the authenticated `POST /v1/memory/rebuild-edges` API. It replaces generated `same_topic` and `same_subject` relationships, considers nodes of every status, and preserves other relationship types. Back up important data first. The response's `edges_created` value counts regenerated edges, not all edges. Verify the response, stats, and graph UI afterward; do not restart the service for this operation.

## Backup and restore

- Distinguish the app's JSON backup/restore from PostgreSQL's full database backup. Use protected off-host backups for recovery.
- Test restores against a separate database or isolated schema, never production. Confirm backup and restore format compatibility before acting.
- Treat HTTP restore as a potentially destructive write and verify authorization and target before calling it.

## Pitfalls

- A successful build does not prove a deployment; verify the running unit and live endpoint.
- A socket-activated proxy may be inactive until traffic arrives. Test its configured listener before diagnosing failure.
- System-owned facts are refreshed automatically; deleting one may only cause it to return. Use its supported retire/update workflow.
- Live API probes create data. Record probe IDs and remove test records, then verify cleanup.
- If a change requires restarting the Hermes gateway to reload a plugin, provide the exact restart command to the user instead of restarting it yourself.
- The Hermes application installer does not automatically copy project skills. After cloning this repository, run `scripts/install-hermes-skill.sh`; start a new session if the skill is not discovered.

## Connect another agent harness

A different harness can use mem through the REST API or the starter stdio MCP bridge. Read `references/clients.md` before configuring a client, and review `templates/mem-mcp.py` before deployment. Keep credentials in environment injection or an approved secret manager. Untrusted identities should remain outside `MEM_STEWARD_SOURCE`; their writes go to the review inbox.

## Support files

- `scripts/verify-gate.sh` — optional live smoke test; checks health and API auth, creates probe rows for several writer identities, reports stats, and removes the probes. Review first and run only against the intended service.
- `references/clients.md` — setup, authentication, trust boundary, and verification notes for another harness.
- `templates/mem-mcp.py` — starter Python stdio MCP bridge; configure URL, identity, and token securely before use.

## Verification

A task is complete only when the relevant tests pass and the live service/API/UI reflects the requested change. For repository changes, run `git diff --check` and confirm only intended paths are staged.
