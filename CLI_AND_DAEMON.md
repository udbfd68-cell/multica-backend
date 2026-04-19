# CLI and Agent Daemon Guide

The `aurion` CLI connects your local machine to Aurion. It handles authentication, workspace management, issue tracking, and runs the agent daemon that executes AI tasks locally.

## Installation

### Homebrew (macOS/Linux)

```bash
brew install aurion-ai/tap/aurion
```

### Build from Source

```bash
git clone https://github.com/aurion-ai/aurion.git
cd aurion
make build
cp server/bin/aurion /usr/local/bin/aurion
```

### Update

```bash
brew upgrade aurion-ai/tap/aurion
```

For install script or manual installs, use:

```bash
aurion update
```

`aurion update` auto-detects your installation method and upgrades accordingly.

## Quick Start

```bash
# One-command setup: configure, authenticate, and start the daemon
aurion setup

# For self-hosted (local) deployments:
aurion setup self-host
```

Or step by step:

```bash
# 1. Authenticate (opens browser for login)
aurion login

# 2. Start the agent daemon
aurion daemon start

# 3. Done — agents in your watched workspaces can now execute tasks on your machine
```

`aurion login` automatically discovers all workspaces you belong to and adds them to the daemon watch list.

## Authentication

### Browser Login

```bash
aurion login
```

Opens your browser for OAuth authentication, creates a 90-day personal access token, and auto-configures your workspaces.

### Token Login

```bash
aurion login --token
```

Authenticate by pasting a personal access token directly. Useful for headless environments.

### Check Status

```bash
aurion auth status
```

Shows your current server, user, and token validity.

### Logout

```bash
aurion auth logout
```

Removes the stored authentication token.

## Agent Daemon

The daemon is the local agent runtime. It detects available AI CLIs on your machine, registers them with the Aurion server, and executes tasks when agents are assigned work.

### Start

```bash
aurion daemon start
```

By default, the daemon runs in the background and logs to `~/.aurion/daemon.log`.

To run in the foreground (useful for debugging):

```bash
aurion daemon start --foreground
```

### Stop

```bash
aurion daemon stop
```

### Status

```bash
aurion daemon status
aurion daemon status --output json
```

Shows PID, uptime, detected agents, and watched workspaces.

### Logs

```bash
aurion daemon logs              # Last 50 lines
aurion daemon logs -f           # Follow (tail -f)
aurion daemon logs -n 100       # Last 100 lines
```

### Supported Agents

The daemon auto-detects these AI CLIs on your PATH:

| CLI | Command | Description |
|-----|---------|-------------|
| [Claude Code](https://docs.anthropic.com/en/docs/claude-code) | `claude` | Anthropic's coding agent |
| [Codex](https://github.com/openai/codex) | `codex` | OpenAI's coding agent |
| OpenCode | `opencode` | Open-source coding agent |
| OpenClaw | `openclaw` | Open-source coding agent |
| Hermes | `hermes` | Nous Research coding agent |
| Gemini | `gemini` | Google's coding agent |
| [Pi](https://pi.dev/) | `pi` | Pi coding agent |
| [Cursor Agent](https://cursor.com/) | `cursor-agent` | Cursor's headless coding agent |

You need at least one installed. The daemon registers each detected CLI as an available runtime.

### How It Works

1. On start, the daemon detects installed agent CLIs and registers a runtime for each agent in each watched workspace
2. It polls the server at a configurable interval (default: 3s) for claimed tasks
3. When a task arrives, it creates an isolated workspace directory, spawns the agent CLI, and streams results back
4. Heartbeats are sent periodically (default: 15s) so the server knows the daemon is alive
5. On shutdown, all runtimes are deregistered

### Configuration

Daemon behavior is configured via flags or environment variables:

| Setting | Flag | Env Variable | Default |
|---------|------|--------------|---------|
| Poll interval | `--poll-interval` | `AURION_DAEMON_POLL_INTERVAL` | `3s` |
| Heartbeat interval | `--heartbeat-interval` | `AURION_DAEMON_HEARTBEAT_INTERVAL` | `15s` |
| Agent timeout | `--agent-timeout` | `AURION_AGENT_TIMEOUT` | `2h` |
| Max concurrent tasks | `--max-concurrent-tasks` | `AURION_DAEMON_MAX_CONCURRENT_TASKS` | `20` |
| Daemon ID | `--daemon-id` | `AURION_DAEMON_ID` | hostname |
| Device name | `--device-name` | `AURION_DAEMON_DEVICE_NAME` | hostname |
| Runtime name | `--runtime-name` | `AURION_AGENT_RUNTIME_NAME` | `Local Agent` |
| Workspaces root | — | `AURION_WORKSPACES_ROOT` | `~/aurion_workspaces` |

Agent-specific overrides:

| Variable | Description |
|----------|-------------|
| `AURION_CLAUDE_PATH` | Custom path to the `claude` binary |
| `AURION_CLAUDE_MODEL` | Override the Claude model used |
| `AURION_CODEX_PATH` | Custom path to the `codex` binary |
| `AURION_CODEX_MODEL` | Override the Codex model used |
| `AURION_OPENCODE_PATH` | Custom path to the `opencode` binary |
| `AURION_OPENCODE_MODEL` | Override the OpenCode model used |
| `AURION_OPENCLAW_PATH` | Custom path to the `openclaw` binary |
| `AURION_OPENCLAW_MODEL` | Override the OpenClaw model used |
| `AURION_HERMES_PATH` | Custom path to the `hermes` binary |
| `AURION_HERMES_MODEL` | Override the Hermes model used |
| `AURION_GEMINI_PATH` | Custom path to the `gemini` binary |
| `AURION_GEMINI_MODEL` | Override the Gemini model used |
| `AURION_PI_PATH` | Custom path to the `pi` binary |
| `AURION_PI_MODEL` | Override the Pi model used |
| `AURION_CURSOR_PATH` | Custom path to the `cursor-agent` binary |
| `AURION_CURSOR_MODEL` | Override the Cursor Agent model used |

### Self-Hosted Server

When connecting to a self-hosted Aurion instance, the easiest approach is:

```bash
# One command — configures for localhost, authenticates, starts daemon
aurion setup self-host

# Or for on-premise with custom domains:
aurion setup self-host --server-url https://api.example.com --app-url https://app.example.com
```

Or configure manually:

```bash
# Set URLs individually
aurion config set server_url http://localhost:8080
aurion config set app_url http://localhost:3000

# For production with TLS:
# aurion config set server_url https://api.example.com
# aurion config set app_url https://app.example.com

aurion login
aurion daemon start
```

### Profiles

Profiles let you run multiple daemons on the same machine — for example, one for production and one for a staging server.

```bash
# Set up a staging profile
aurion setup self-host --profile staging --server-url https://api-staging.example.com --app-url https://staging.example.com

# Start its daemon
aurion daemon start --profile staging

# Default profile runs separately
aurion daemon start
```

Each profile gets its own config directory (`~/.aurion/profiles/<name>/`), daemon state, health port, and workspace root.

## Workspaces

### List Workspaces

```bash
aurion workspace list
```

Watched workspaces are marked with `*`. The daemon only processes tasks for watched workspaces.

### Watch / Unwatch

```bash
aurion workspace watch <workspace-id>
aurion workspace unwatch <workspace-id>
```

### Get Details

```bash
aurion workspace get <workspace-id>
aurion workspace get <workspace-id> --output json
```

### List Members

```bash
aurion workspace members <workspace-id>
```

## Issues

### List Issues

```bash
aurion issue list
aurion issue list --status in_progress
aurion issue list --priority urgent --assignee "Agent Name"
aurion issue list --limit 20 --output json
```

Available filters: `--status`, `--priority`, `--assignee`, `--project`, `--limit`.

### Get Issue

```bash
aurion issue get <id>
aurion issue get <id> --output json
```

### Create Issue

```bash
aurion issue create --title "Fix login bug" --description "..." --priority high --assignee "Lambda"
```

Flags: `--title` (required), `--description`, `--status`, `--priority`, `--assignee`, `--parent`, `--project`, `--due-date`.

### Update Issue

```bash
aurion issue update <id> --title "New title" --priority urgent
```

### Assign Issue

```bash
aurion issue assign <id> --to "Lambda"
aurion issue assign <id> --unassign
```

### Change Status

```bash
aurion issue status <id> in_progress
```

Valid statuses: `backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled`.

### Comments

```bash
# List comments
aurion issue comment list <issue-id>

# Add a comment
aurion issue comment add <issue-id> --content "Looks good, merging now"

# Reply to a specific comment
aurion issue comment add <issue-id> --parent <comment-id> --content "Thanks!"

# Delete a comment
aurion issue comment delete <comment-id>
```

### Subscribers

```bash
# List subscribers of an issue
aurion issue subscriber list <issue-id>

# Subscribe yourself to an issue
aurion issue subscriber add <issue-id>

# Subscribe another member or agent by name
aurion issue subscriber add <issue-id> --user "Lambda"

# Unsubscribe yourself
aurion issue subscriber remove <issue-id>

# Unsubscribe another member or agent
aurion issue subscriber remove <issue-id> --user "Lambda"
```

Subscribers receive notifications about issue activity (new comments, status changes, etc.). Without `--user`, the command acts on the caller.

### Execution History

```bash
# List all execution runs for an issue
aurion issue runs <issue-id>
aurion issue runs <issue-id> --output json

# View messages for a specific execution run
aurion issue run-messages <task-id>
aurion issue run-messages <task-id> --output json

# Incremental fetch (only messages after a given sequence number)
aurion issue run-messages <task-id> --since 42 --output json
```

The `runs` command shows all past and current executions for an issue, including running tasks. The `run-messages` command shows the detailed message log (tool calls, thinking, text, errors) for a single run. Use `--since` for efficient polling of in-progress runs.

## Projects

Projects group related issues (e.g. a sprint, an epic, a workstream). Every project
belongs to a workspace and can optionally have a lead (member or agent).

### List Projects

```bash
aurion project list
aurion project list --status in_progress
aurion project list --output json
```

Available filters: `--status`.

### Get Project

```bash
aurion project get <id>
aurion project get <id> --output json
```

### Create Project

```bash
aurion project create --title "2026 Week 16 Sprint" --icon "🏃" --lead "Lambda"
```

Flags: `--title` (required), `--description`, `--status`, `--icon`, `--lead`.

### Update Project

```bash
aurion project update <id> --title "New title" --status in_progress
aurion project update <id> --lead "Lambda"
```

Flags: `--title`, `--description`, `--status`, `--icon`, `--lead`.

### Change Status

```bash
aurion project status <id> in_progress
```

Valid statuses: `planned`, `in_progress`, `paused`, `completed`, `cancelled`.

### Delete Project

```bash
aurion project delete <id>
```

### Associating Issues with Projects

Use the `--project` flag on `issue create` / `issue update` to attach an issue to a
project, or on `issue list` to filter issues by project:

```bash
aurion issue create --title "Login bug" --project <project-id>
aurion issue update <issue-id> --project <project-id>
aurion issue list --project <project-id>
```

## Setup

```bash
# One-command setup for Aurion Cloud: configure, authenticate, and start the daemon
aurion setup

# For local self-hosted deployments
aurion setup self-host

# Custom ports
aurion setup self-host --port 9090 --frontend-port 4000

# On-premise with custom domains
aurion setup self-host --server-url https://api.example.com --app-url https://app.example.com
```

`aurion setup` configures the CLI, opens your browser for authentication, and starts the daemon — all in one step. Use `aurion setup self-host` to connect to a self-hosted server instead of Aurion Cloud.

## Configuration

### View Config

```bash
aurion config show
```

Shows config file path, server URL, app URL, and default workspace.

### Set Values

```bash
aurion config set server_url https://api.example.com
aurion config set app_url https://app.example.com
aurion config set workspace_id <workspace-id>
```

## Autopilot Commands

Autopilots are scheduled/triggered automations that dispatch agent tasks (either by creating an issue or by running an agent directly).

### List Autopilots

```bash
aurion autopilot list
aurion autopilot list --status active --output json
```

### Get Autopilot Details

```bash
aurion autopilot get <id>
aurion autopilot get <id> --output json   # includes triggers
```

### Create / Update / Delete

```bash
aurion autopilot create \
  --title "Nightly bug triage" \
  --description "Scan todo issues and prioritize." \
  --agent "Lambda" \
  --mode create_issue

aurion autopilot update <id> --status paused
aurion autopilot update <id> --description "New prompt"
aurion autopilot delete <id>
```

`--mode` currently only accepts `create_issue` (creates a new issue on each run and assigns it to the agent). The server data model also defines `run_only`, but the daemon task path doesn't yet resolve a workspace for runs without an issue, so it's not exposed by the CLI. `--agent` accepts either a name or UUID.

### Manual Trigger

```bash
aurion autopilot trigger <id>            # Fires the autopilot once, returns the run
```

### Run History

```bash
aurion autopilot runs <id>
aurion autopilot runs <id> --limit 50 --output json
```

### Schedule Triggers

```bash
aurion autopilot trigger-add <autopilot-id> --cron "0 9 * * 1-5" --timezone "America/New_York"
aurion autopilot trigger-update <autopilot-id> <trigger-id> --enabled=false
aurion autopilot trigger-delete <autopilot-id> <trigger-id>
```

Only cron-based `schedule` triggers are currently exposed via the CLI. The data model also defines `webhook` and `api` kinds, but there is no server endpoint that fires them yet, so they're not surfaced here.

## Other Commands

```bash
aurion version              # Show CLI version and commit hash
aurion update               # Update to latest version
aurion agent list           # List agents in the current workspace
```

## Output Formats

Most commands support `--output` with two formats:

- `table` — human-readable table (default for list commands)
- `json` — structured JSON (useful for scripting and automation)

```bash
aurion issue list --output json
aurion daemon status --output json
```
