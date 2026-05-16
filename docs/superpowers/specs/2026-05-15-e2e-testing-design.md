# E2E Testing Environment Design

**Date:** 2026-05-15  
**Scope:** Full end-to-end test suite for the agentsdx CLI ↔ agentsdxd server HTTP contract, including real VirtualBox VM flows.

---

## Goals

- Exercise the full stack: CLI subprocess → HTTP → server → VirtualBox/Packer
- Run locally on a developer workstation with VirtualBox and Packer installed
- Keep VM-heavy tests opt-in so fast feedback is always available
- Fit naturally into the existing multi-module Go repo structure

---

## Repository Layout

```
agent-sandbox/
└── e2e/
    ├── go.mod              # module: github.com/duck-labs/agentsdx-e2e
    ├── go.sum
    ├── main_test.go        # TestMain: build bins, start server, teardown
    ├── helpers_test.go     # runCLI helper, assertion helpers
    ├── profiles_test.go    # //go:build e2e
    ├── credentials_test.go # //go:build e2e
    ├── images_test.go      # //go:build e2e,vm
    └── sessions_test.go    # //go:build e2e,vm
```

### Build tags

| Tag | Meaning |
|---|---|
| `e2e` | Required for all e2e tests; excluded from regular `go test ./...` inside any module |
| `e2e,vm` | Additionally required for tests that boot real VirtualBox VMs |

### Run commands

```bash
# Fast tests only (profiles, credentials) — no VMs
go test -tags e2e ./e2e/...

# All tests including real VM flows (~15 min for image build)
go test -tags e2e,vm -timeout 30m ./e2e/...
```

---

## TestMain and Harness (`main_test.go`)

`TestMain` runs in order:

1. **Build binaries** — `go build ../server/cmd/agentsdxd` and `go build ../cli/cmd/agentsdx` into a shared temp dir (`/tmp/agentsdx-e2e-<pid>/`).
2. **Create isolated data dirs** — separate temp dirs for `AGENTSDX_DATA_DIR`; `AGENTSDX_VM_DIR` points at the real `vm/` directory in the repo.
3. **Pick a free port** — bind a listener on `:0`, record the port, close the listener.
4. **Start server subprocess** — spawn `agentsdxd serve` with env:
   - `AGENTSDX_VAULT_SECRET` = random 32-byte hex
   - `AGENTSDX_SERVER_URL` = `http://127.0.0.1:<port>`
   - `AGENTSDX_ADDR` = `:<port>`
   - `AGENTSDX_DATA_DIR` = temp dir
5. **Wait for readiness** — poll `GET /profiles` with 100ms interval, 10s timeout.
6. **Run tests** — `m.Run()`.
7. **Teardown** — kill server process, remove temp dirs.

---

## Helpers (`helpers_test.go`)

```go
// runCLI runs the agentsdx binary with the given args and captures output.
func runCLI(args ...string) (stdout, stderr string, exitCode int)

// Assertion helpers
func assertExitCode(t *testing.T, got, want int)
func assertContains(t *testing.T, s, substr string)
func assertJSONField(t *testing.T, body, key, want string)
```

`runCLI` injects `AGENTSDX_SERVER=http://127.0.0.1:<port>` automatically.

---

## Test Cases

### `profiles_test.go` (`//go:build e2e`)

| Subtest | Action | Assertion |
|---|---|---|
| CreateProfile | `agentsdx create` with a synthetic profile | exit 0; profile appears in `GET /profiles` |
| ListProfiles | `agentsdx profiles` | stdout contains profile name |
| DeleteProfile | `agentsdx` (delete command) | exit 0; profile absent from list |
| DuplicateCreate | create same profile twice | non-zero exit |

### `credentials_test.go` (`//go:build e2e`)

| Subtest | Action | Assertion |
|---|---|---|
| SetCredentials | `agentsdx credentials set <profile>` with synthetic tarball | exit 0; `GET /sessions/{id}/agent-state` returns the tarball |

### `images_test.go` (`//go:build e2e,vm`)

| Subtest | Action | Assertion |
|---|---|---|
| BuildImage | `agentsdx images build <profile>` | server responds 202; OVA appears in image store within timeout |

### `sessions_test.go` (`//go:build e2e,vm`)

| Subtest | Action | Assertion |
|---|---|---|
| RunSession | `agentsdx run <profile>` | session reaches `running` state |
| StopSession | `agentsdx stop <profile>` | session destroyed; VM absent from VBoxManage |

Tests within a file share a profile name derived from `t.Name()` to avoid collisions between parallel runs.

---

## Error Handling and Timeouts

- Image build tests use `-timeout 30m` to accommodate the ~15-minute Packer build.
- `runCLI` captures both stdout and stderr; failures print both for easy debugging.
- Server readiness poll times out after 10s with a clear error message if the binary fails to start.

---

## Non-Goals

- CI/CD integration (local only for now)
- Mocking VirtualBox or Packer
- Testing internal packages directly (this is black-box CLI testing only)
