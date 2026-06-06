# Investigation: Remote VM for AI Agents

I'm creating this doc because I started this repo just to test a virtual machine on my macOS.

This virtual machine would run claude code with dangerously skipped permissions, and I could let it work without supervision.

I built a Docker image that included Claude, Git, Docker, and Mise. And since macOS runs a virtual machine for each container, I would have a separate environment for running claude code without permissions.

Then I started to think whether I could release this virtual machine— not the virtual machine itself, but this Docker image— into VPS providers and remotely access them.

In other words, I would create a remote environment for our development that I could access from my machine.

The idea is that I could have a Bash command that would spin up a virtual machine, and I could easily access it from my local claude command. I could also use his method of copying the local credentials and sending them to that machine, and combine that with a Visual Studio plugin.

My system would be responsible for spawning a virtual machine with a provider that lives only for the duration of the session, providing an OS image that includes the necessary tools such as the agent, Git, and Docker Compose for accessing files. I would also need to manage the state, persisting it across different sessions and VMs.

I could also customize different combinations of images with specific services that people would want, like different agent providers and different extra plugins, and so on.

The key insight is that this is **not Claude-specific**. The environment layer sits below any agent — Claude Code, OpenCode, Hermes, or whatever comes next. A developer can run different agents in different sandboxes, or compare agents on the same project, all with a consistent workflow and without touching their local machine.

---

## Investigation Results

### 1. Companies Providing This

There are several companies in the space of remote environments for AI agents:

- **Cua (trycua.com)** — YC X25. Most directly relevant. Provides a Docker-style container runtime that lets AI agents drive full OSes in lightweight, isolated VMs. Supports macOS, Linux, Windows, Android. Cloud-hosted or local. Uses Apple's Virtualization.Framework for near-native performance. Open source (MIT).

- **Modal** — Cloud platform used as a sandbox provider for AI agent execution. Serverless infrastructure for code execution.

- **Docker** — Docker Sandboxes (sbx) run AI coding agents in isolated microVM sandboxes. Each sandbox gets its own Docker daemon, filesystem, and network.

- **Beam (beam.cloud)** — YC W22. AI-Native Cloud Platform for inference, sandboxes, and agents.

- **Google Cloud Workstations** — Cloud-based development environments (more dev-focused).

- **GitHub Codespaces** — Cloud dev environments with VS Code integration.

- **Cedana (cedana.ai)** — YC S23. Live migration for CPU/GPU AI workloads, orchestration for AI workflows.

> Note: Cua is often cited as the closest analog, but the goal here is different. Cua sandboxes AI agents so they can *control* a computer. This project is about a managed environment layer for the developer — a consistent, isolated place to run any AI coding agent (Claude, OpenCode, Hermes, etc.), regardless of provider.

### 2. Open Source Projects

- **Cua (github.com/trycua/cua)** — MIT licensed. Agent-ready sandboxes for any OS. One API for any VM/container image — cloud or local. Includes CLI, Python SDK, and MCP server for Claude Code.

- **Alibaba OpenSandbox (github.com/alibaba/OpenSandbox)** — Production-grade sandbox platform for AI agents. Multi-language SDKs, Docker/Kubernetes runtimes for coding agents, GUI agents, agent evaluation, and RL training.

- **SmolVM (github.com/CelestoAI/SmolVM)** — Open-source AI sandbox giving agents disposable computers to browse, run code, and do real work.

- **Agent-Sandbox (Kubernetes SIG)** — Kubernetes-native runtime platform for AI agents. Isolated, stateful, multi-tenant sandboxes for code execution, browser/computer tasks, and shell workflows. Official Kubernetes SIG project.

- **OpenAI Agents SDK** — Has built-in sandbox agent support with persistent workspaces for agents to search documents, edit files, and run commands.

### 3. Y Combinator Backed (since 2023)

Using the unofficial YC companies API (yc-oss.github.io) and web search, these YC-backed companies are most relevant:

- **Cua (X25 / Spring 2025)** — Open-source infrastructure for computer-use agents. Docker-style container runtime for AI agents to control full operating systems in VMs. https://www.ycombinator.com/companies/cua

- **Invo (W25)** — "infra for computer use agents". https://www.ycombinator.com/companies/invo

- **Beam (W22)** — AI-Native Cloud Platform for inference, sandboxes, and agents. https://www.ycombinator.com/companies/beam

- **Ubicloud (W24)** — Open source alternative to AWS. Provides elastic compute, block storage, virtual networking on bare metal providers. https://www.ycombinator.com/companies/ubicloud

- **Cedana (S23)** — Live migration for CPU/GPU AI workloads, orchestration for AI workflows. https://www.ycombinator.com/companies/cedana

- **Omnistrate (W23)** — Transform any docker image to multi-cloud SaaS. https://www.ycombinator.com/companies/omnistrate

- **Porter (S20)** — Easiest way to deploy on AWS/GCP/Azure. https://www.ycombinator.com/companies/porter

- **Flightcontrol (W22)** — PaaS that deploys to your own AWS account. https://www.ycombinator.com/companies/flightcontrol

### Data Sources

- YC Companies API: https://yc-oss.github.io/api/ (unofficial, updated daily)
- YC Directory: https://www.ycombinator.com/companies

---

# Design

## Core Concept

A managed environment layer that sits below any AI coding agent — Claude Code, OpenCode, Hermes, or others. Each environment runs on a fresh remote VM, isolated from the developer's local machine, with a consistent workflow regardless of which agent is in use.

The value is threefold:
- **Isolation** — nothing from the local machine leaks into the agent's environment; run with full permissions safely
- **Consistency** — same setup, same workflow, same state management across any agent
- **Comparability** — spin up two sandbox profiles on the same repo with different agents and compare results

Code state is managed by **git**. Agent state is managed by a **vault**. The VM is always ephemeral and disposable.

## Data Model

A **user** owns multiple **sandbox profiles**. Each sandbox profile is the unit of configuration and state — it defines the infrastructure, the projects to clone, the agent behavior, and holds its own vault.

```
User
├── Sandbox Profile "work-backend"
│     ├── Infrastructure config
│     ├── Projects: [api-repo, infra-repo, shared-libs]
│     ├── Agent config
│     └── Vault (Claude memory + credentials)
│
└── Sandbox Profile "personal-oss"
      ├── Infrastructure config
      ├── Projects: [my-cli, my-blog]
      ├── Agent config
      └── Vault (Claude memory + credentials)
```

The vault is scoped to the sandbox profile — not to a specific repo, not to the whole user account. This means the agent accumulates memory and context across all projects within a sandbox, which is the intended behavior.

## Delivery Model

The controller server is the workhorse. It runs in Docker and owns all heavy operations. The CLI is a thin HTTP client.

```
User's machine
└── agentsdx (CLI) ──HTTP──► Controller Server (Docker)
                                  ├── Packer (pre-installed in image)
                                  ├── Hetzner API (outbound calls)
                                  └── VBoxManage (host socket, for VirtualBox provider)
```

Self-hosting is a single `docker compose up` with env vars configured. The controller image ships with everything pre-installed — clients never install Packer or any other build tooling.

```yaml
# docker-compose.yml (client setup)
services:
  agentsdx:
    image: ghcr.io/duck-labs/agentsdx-server:latest
    volumes:
      - ./data:/data                  # profiles, vaults, sessions, images.json
    environment:
      AGENTSDX_VAULT_SECRET: "..."
      HETZNER_TOKEN: "..."            # omit if only using VirtualBox
    ports:
      - "8080:8080"
```

## Key Decisions

| Decision | MVP | Future |
|---|---|---|
| Deployment | Self-hosted (Docker) | SaaS layer on top for multi-tenant |
| VM providers | VirtualBox (local) | Hetzner + additional cloud providers |
| Image building | Packer runs inside controller container, triggered via API; one image per agent | — |
| Agent auth | Subscription-based; `credentials set` copies local `.claude/` + `.claude.json` into vault | — |
| User connection | CLI-mediated SSH using vault-managed key | — |
| Secret store | AES-256-GCM encrypted files + env var key | OpenBao (Transit engine) or cloud KMS |

## VM Providers

Two providers are supported from the start, which keeps the `VMProvider` interface honest about what is truly shared vs provider-specific.

| | Hetzner | VirtualBox |
|---|---|---|
| Use case | Remote / cloud | Fully local / air-gapped |
| User-data delivery | Hetzner metadata API | NoCloud ISO mounted at boot |
| IP address | Public IP from API | Host-only or bridged (local) |
| Image format | Snapshot ID | `.ova` file |
| VM lifecycle | Hetzner REST API | `VBoxManage` CLI |
| Image build | Packer `hetzner-iso` builder | Packer `virtualbox-iso` builder |

**Image building with Packer:**

Hetzner does not allow direct image uploads — a snapshot must be built in-place. Both providers use Packer to automate this:

1. Packer boots a base VM from a stock OS image
2. Runs `vm/base/provision.sh` — installs base tools (git, curl, etc.)
3. Runs `vm/tooling/{tool}/provision.sh` for each tool declared in the profile (e.g. mise, docker, gh)
4. Runs `vm/agents/{agent}/provision.sh` — installs the selected agent binary and dependencies
5. Bakes `vm/agents/{agent}/entrypoint.sh` and `vm/vault-sync.sh` into the image
6. Powers off the VM and captures the image (Hetzner snapshot ID / VirtualBox `.ova`)
7. Stores the image reference keyed by profile name in `images.json`

The image builder reads the profile's `infrastructure.tooling` list and composes the provisioning steps — base + each declared tool + agent. Each tool and agent has its own isolated `provision.sh` under `vm/`:

```
vm/
├── virtualbox.pkr.hcl        # Packer template, driven by build manifest from server
├── hetzner.pkr.hcl           # placeholder for future use
├── base/
│   └── provision.sh          # minimal base (git, curl, ssh)
├── tooling/
│   ├── mise/
│   │   └── provision.sh
│   ├── docker/
│   │   └── provision.sh
│   ├── docker-compose/
│   │   └── provision.sh
│   └── gh/
│       └── provision.sh
├── agents/
│   ├── claude/
│   │   ├── provision.sh      # installs claude binary
│   │   └── entrypoint.sh     # restores vault state (.claude/ + .claude.json), execs claude
│   └── _template/            # documented template for new agents
└── vault-sync.sh             # tars vault paths, POSTs to server on session end
```

The profile spec declares the base OS, tooling, and agent. The built image is stored internally keyed by profile name — no separate output name needed:

```yaml
infrastructure:
  provider: virtualbox
  image: ubuntu-24.04              # base OS Packer starts from
  tooling:
    - mise
    - docker
    - gh
```

## SSH Access

VM access is fully mediated by the CLI — users never handle SSH keys or IP addresses directly.

The server generates **two key pairs** per sandbox profile:

1. **Git key** — used by the VM to clone repos. Public key is shown to the user once to add to GitHub/GitLab. Private key stored in vault.
2. **VM access key** — used by the CLI to SSH into the VM. Public key registered with the provider at VM creation (Hetzner API / VirtualBox authorized_keys via NoCloud ISO). Private key stored in vault, never exposed to the user.

**Session flow:**
1. `agentsdx run <profile>` calls the server to start the VM
2. CLI fetches the VM access private key from the server (authenticated request)
3. CLI writes the key to a temp file and execs `ssh -i <tempfile> root@<IP>` — user lands in the VM transparently
4. The VM accepts no other keys and has no password auth

## Secret Store

**MVP:** Vault files are encrypted with AES-256-GCM on the server filesystem. The encryption key is a 32-byte secret set via environment variable (`AGENTSDX_VAULT_SECRET`). Simple, no dependencies, suitable for self-hosted single-user use.

**Future (SaaS):** Envelope encryption backed by **OpenBao** (open source fork of HashiCorp Vault, MPL licensed, maintained by the Linux Foundation) or a cloud KMS (AWS KMS, GCP KMS). The master key never leaves the KMS — only encrypted data keys are stored alongside vault files. The vault encryption backend is abstracted behind an interface so the switch requires no changes to the rest of the server.

## Git Authentication

Each sandbox profile has its own SSH key pair, stored encrypted in the vault. At session start the private key is written to `~/.ssh/` in the VM before repos are cloned.

**Setup flow:**
1. User creates a sandbox profile
2. Server generates an SSH key pair for that profile
3. CLI displays the public key — user adds it to GitHub/GitLab as a deploy key or account SSH key
4. Private key is stored in the vault, never leaves the server unencrypted

This keeps each sandbox fully isolated — compromising one profile's key does not affect others.

**Future:** a reusable key pool where a user manages named SSH keys and associates them to sandbox profiles, avoiding the need to add a new key to GitHub for every profile.

## Sandbox Profile Spec (Declarative)

A sandbox profile is defined as a config file. At MVP, it is fully declarative. Layered/inherited profiles (e.g. a team base profile extended by individual sandboxes) are a future improvement.

```yaml
name: work-backend

infrastructure:
  provider: virtualbox              # virtualbox | hetzner
  image: ubuntu-24.04              # base OS Packer starts from
  tooling:
    - mise
    - docker
    - docker-compose
    - gh

projects:
  - repo: git@github.com:org/api-repo.git
    path: ~/projects/api
  - repo: git@github.com:org/infra-repo.git
    path: ~/projects/infra

agent:
  provider: claude              # claude | opencode | hermes | ...
  skills:
    - superpowers/brainstorming
    - superpowers/tdd
  # future: system_prompt_append, agent_config_override, layered profiles
```

## Session Lifecycle

**Start:**
1. Server spins up a fresh VM via the configured provider (Hetzner or VirtualBox), registering the VM access public key at creation
2. Vault is pulled and hydrated (agent auth state, git SSH key)
3. VM entrypoint script runs: clones configured repos, configures git SSH, launches agent
4. CLI fetches VM access private key from server and opens SSH session transparently

**End:**
1. User runs `agentsdx stop` or session times out
2. Entrypoint script syncs vault back to server (agent memory, updated settings)
3. Code changes are pushed to git by the user/agent before stopping
4. VM is destroyed

Each agent has its own **entrypoint script** baked into its image. It handles vault hydration (restoring `.claude/` and `.claude.json`), repo cloning, and agent launch. Adding a new agent means a new `agents/{name}/` directory — no changes to shared scripts or server code.

## Agent Authentication

Claude Code authenticates via subscription, not API key. Auth state lives in `.claude/` and `.claude.json` on the local machine after `claude login`. These files are persisted in the vault and restored into the VM at each session start.

On first use, run `agentsdx credentials set <profile>` to copy local auth state into the vault. Subsequent session syncs keep it up to date automatically.

## CLI

```bash
agentsdx run work-backend           # spin up VM and print SSH connection details
agentsdx stop work-backend          # sync vault, destroy VM
agentsdx profiles                   # list sandbox profiles
agentsdx create work-backend        # create a new profile interactively
agentsdx credentials set <profile>  # copy local agent auth state (.claude/ + .claude.json) into vault
agentsdx sync <profile> <file>      # push a local file into the running VM via SCP
```

---

# MVP Feature Set

**Controller server (Docker image, ships with Packer pre-installed):**
- Spawn and destroy VMs on demand via pluggable `VMProvider` (VirtualBox, Hetzner)
- Watch for session timeout and expire idle VMs
- Manage encrypted vault storage per sandbox profile
- Run Packer builds internally when triggered via API (parameterized by agent), store resulting image references in `images.json`

**VM entrypoint script (baked into agent-specific image):**
- Hydrate vault contents into the VM on start (agent auth state + git SSH key)
- Clone configured repos using profile SSH key
- Launch the agent
- Sync vault back to server on session end

**CLI (thin HTTP client, no heavy dependencies):**
- `agentsdx run <profile>` — start session, open SSH transparently
- `agentsdx stop <profile>` — sync vault and destroy VM
- `agentsdx credentials set <profile>` — copy local agent auth state into vault
- `agentsdx sync <profile> <file>` — push local files into a running VM via SCP
- `agentsdx images build <profile>` — trigger image build for a profile (reads its image, tooling, agent)
- Profile management (create, list, edit)

---

# Future Improvements

- Layered sandbox profiles — a base profile that other profiles can extend (useful for teams sharing a standard config)
- Reusable SSH key pool — named keys managed at the user level and associated to sandbox profiles, so one GitHub key covers multiple sandboxes
- Secret store upgrade — replace env var encryption key with OpenBao (Transit engine) or cloud KMS (AWS KMS, GCP KMS) for envelope encryption; abstracted behind a `Encryptor` interface so no other code changes
- Code Server / VS Code Remote access from any device including mobile
- Easy browser automation inside the sandbox
- Running local models on specialized hardware attached to the VM
- Community-contributed profile templates
- Additional VM providers (AWS EC2, GCP Compute Engine, DigitalOcean)
- App that renders the terminal on mobile and provides a way to even speech to interact to it
- Use golang migrate and sqlc to manage server queries and even allow the usage of pgsql to replace sqlite;