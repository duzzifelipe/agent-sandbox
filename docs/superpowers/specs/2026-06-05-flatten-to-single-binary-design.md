# Flatten to Single Binary Design

**Date:** 2026-06-05
**Status:** Approved

---

## Problem

The current architecture splits the codebase into three Go modules (`cli`, `server`, `shared`). The CLI is a thin HTTP client that always delegates to `agentsdxd`. "Local" profiles don't actually run locally — they run wherever the daemon process is running, which is confusing and breaks the expectation that local means on the user's machine.

---

## Decision

Collapse everything into a single Go module and binary. Delete the daemon. The CLI calls all business logic directly. A session is the lifetime of the `agentsdx profiles run` process.

---

## Repo Layout

```
agent-sandbox/
├── cmd/agentsdx/          # single binary
├── internal/
│   ├── profile/           # profile YAML store
│   ├── session/           # vm create → ssh → destroy on exit
│   ├── vault/             # AES-256-GCM vault
│   ├── vm/                # VMProvider interface + provider impls
│   └── builder/           # image builder (Packer)
├── types/                 # plain data types (merged from shared/)
└── go.mod                 # single module: github.com/duck-labs/agentsdx
```

**Deleted:**
- `server/` — daemon, HTTP handlers, session store, builder HTTP wrapper
- `shared/` — merged into `types/`
- `cli/internal/client/` — HTTP client layer, nothing to talk to
- `cli/internal/state/` — server URL config, no longer needed
- `db/` package and SQLite dependency
- `agentsdxd` binary

---

## Data Directory

Moves from `./data/` (relative to daemon working directory) to `~/.agentsdx/` (user's home):

```
~/.agentsdx/
├── profiles/
│   └── <name>.yaml
└── vault/
    └── <name>.enc
```

---

## Session Lifecycle

`agentsdx profiles run <name>` owns the full session lifecycle:

1. Load profile from `~/.agentsdx/profiles/<name>.yaml`
2. Load vault from `~/.agentsdx/vault/<name>.enc`
3. Call `vm.Provider.CreateVM(...)` directly
4. Poll until VM is reachable over SSH
5. Exec into SSH
6. On SSH exit or SIGINT/SIGTERM: call `vm.Provider.DestroyVM(...)` and exit

No separate `stop` command. No session state written to disk. The process is the session.

---

## Vault-Sync

Removed entirely. Credentials are written to the VM at init time via cloud-init user-data only. Nothing is synced back on session end.

`AGENTSDX_SERVER_URL` and `AGENTSDX_SESSION_ID` are removed from the user-data template.

---

## CLI Commands

```bash
agentsdx profiles list
agentsdx profiles create
agentsdx profiles run <name>        # create VM, SSH, destroy on exit
agentsdx profiles build <name>      # trigger Packer image build
agentsdx profiles repo add <name>   # add a git repo to a profile

agentsdx secrets set <profile> <key> <value>
agentsdx secrets delete <profile> <key>
agentsdx secrets list <profile>
```

---

## SaaS Path (future, not in scope)

This design does not close the door on SaaS. When that becomes relevant, the natural seam is a `Backend` interface on the CLI with a `RemoteBackend` implementation that points at a hosted server. The `internal/` packages would become the `LocalBackend`. Nothing in the current design prevents this.

---

## Key Decisions

| Area | Decision |
|---|---|
| Module count | 1 (down from 3) |
| Daemon | Deleted |
| Session state | In-process only (no DB, no disk) |
| Data location | `~/.agentsdx/` |
| Vault-sync | Removed |
| Stop command | Removed — destroy happens on process exit |
| SaaS | Out of scope; Backend interface is the future seam |
