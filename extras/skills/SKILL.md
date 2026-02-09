---
name: beagle
description: Manage scheduled and always-on jobs on macOS with beagle. Write and edit beagle.yaml config, set up profile-based multi-repo workflows, apply and validate config, check job status and logs, view failures, enable or disable jobs, troubleshoot with doctor, and configure circuit breaker and throttle policies.
---

# Beagle

Beagle is a macOS job orchestrator. You define jobs in `beagle.yaml` and beagle manages them through launchd, abstracting away plist files and launchctl commands.

## Configuration Format

Config file: `beagle.yaml`

```yaml
version: 1
defaults:
  timezone: America/Chicago       # IANA timezone for scheduled jobs
  working_dir: /absolute/path     # must be absolute
  logs_dir: /absolute/path        # override default log location
  throttle_seconds: 30            # minimum seconds between runs (>= 0)
  env:
    KEY: value                    # environment variables for all jobs
  circuit_breaker:
    max_failures: 5               # failures before tripping (>= 0)
    window_seconds: 600           # failure counting window (>= 0)
    cooldown_seconds: 1800        # cooldown before retrying (>= 0)
jobs:
  my_job:
    type: schedule                # "schedule" or "service"
    command: ["/absolute/path/to/binary", "--flag"]
    schedule:
      cron: "0 5 1 * *"          # 5-field cron (required for schedule)
      timezone: America/New_York  # per-job timezone override
    restart: never                # "never", "on-failure", or "always"
    enabled: true                 # default true; set false to skip
    working_dir: /absolute/path   # per-job override
    env:
      KEY: value                  # merged with defaults.env
    throttle_seconds: 60          # per-job override
    circuit_breaker:              # per-job override
      max_failures: 3
      window_seconds: 300
      cooldown_seconds: 900
```

### Validation Rules

- `version` must be `1`.
- At least one job is required.
- Job IDs must match `^[a-z0-9][a-z0-9_-]{1,63}$`.
- `command[0]` must be an absolute path. `command` must be non-empty.
- `working_dir` (both defaults and per-job) must be absolute if set.
- `schedule.cron` is required for `schedule` jobs and forbidden for `service` jobs.
- `schedule.cron` must have exactly 5 fields.
- `timezone` values must be valid IANA timezone names.
- `throttle_seconds` and all `circuit_breaker` fields must be >= 0.
- `restart` must be one of: `never`, `on-failure`, `always`.

## Commands

| Command | Description |
|---|---|
| `beagle validate` | Validate config file |
| `beagle apply` | Reconcile managed jobs with launchd |
| `beagle ls` | List configured jobs and their state |
| `beagle status <job>` | Show detailed status for a job |
| `beagle logs <job> [--stderr] [--tail N]` | Show job stdout (or stderr) logs |
| `beagle failures [--job <job>] [--limit N]` | Show recent failures |
| `beagle run-now <job>` | Trigger an immediate run |
| `beagle enable <job>` | Enable a job |
| `beagle disable <job>` | Disable a job |
| `beagle doctor` | Run environment diagnostics |
| `beagle profile register <name> <config-path>` | Register a named profile |
| `beagle profile ls` | List all profiles |
| `beagle profile use <name>` | Set the active profile |
| `beagle profile rm <name>` | Remove a profile |

Global flags: `--config <path>`, `--profile <name>`.

Where `<job>` appears above, you can use a bare job ID or `<profile>:<job>` selector syntax.

## Profile Workflow

Profiles let you manage multiple repos, each with their own `beagle.yaml`.

```sh
# Register profiles pointing at different config files
beagle profile register myapp /Users/me/src/myapp/beagle.yaml
beagle profile register infra /Users/me/src/infra/beagle.yaml

# List and switch
beagle profile ls
beagle profile use infra

# Target a specific profile's job without switching
beagle status myapp:daily_backup

# Remove a profile
beagle profile rm infra
```

Profile names must match `^[a-z0-9][a-z0-9_-]{0,63}$` (min 1 char). Job IDs use `{1,63}` (min 2 chars).

### Config Resolution Precedence

1. `--config <path>` (explicit path)
2. `--profile <name>` (named profile)
3. Active profile (set via `beagle profile use`)
4. `./beagle.yaml` in the current directory

The `profile:job` selector syntax in a job argument overrides `--profile` for that command.

Each profile gets its own namespace, which isolates launchd labels and log directories so jobs from different repos never collide.

## Key Paths

| What | Path |
|---|---|
| Project config | `beagle.yaml` (in project root) |
| Profile registry | `~/.config/beagle/profiles.yaml` |
| Run history DB | `~/.local/share/beagle/beagle.db` |
| Job logs | `~/.local/share/beagle/logs/<namespace>/<job>/stdout.log` |
| Job stderr | `~/.local/share/beagle/logs/<namespace>/<job>/stderr.log` |
| Launchd plists | `~/Library/LaunchAgents/com.beagle.<user>.<namespace>.<job>.plist` |

## Common Workflows

### Adding a Scheduled Job

1. Add a `schedule` type job to `beagle.yaml` with a `cron` expression.
2. Run `beagle validate` to check the config.
3. Run `beagle apply` to install the job into launchd.
4. Verify with `beagle ls` and `beagle status <job>`.

### Adding a Service

1. Add a `service` type job to `beagle.yaml` (no `cron` field).
2. Set `restart: on-failure` or `restart: always` as appropriate.
3. Run `beagle validate` then `beagle apply`.

### Setting Up Multi-Repo Profiles

1. Register each repo: `beagle profile register <name> /path/to/repo/beagle.yaml`.
2. Switch context: `beagle profile use <name>`.
3. All commands now target that profile's config. Use `profile:job` to reach across profiles without switching.

### Debugging a Failing Job

1. `beagle failures --job <job>` to see recent failure history with exit codes.
2. `beagle logs <job>` and `beagle logs <job> --stderr` to inspect output.
3. `beagle status <job>` to check whether the job is loaded and enabled.
4. `beagle doctor` to verify the environment (home dir, launchd backend).
5. `beagle run-now <job>` to trigger a manual run and observe behavior.
