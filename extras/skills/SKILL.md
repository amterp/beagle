---
name: beagle
description: Manage scheduled and always-on jobs on macOS with beagle. Write and edit the global ~/.beagle/jobs.yaml config, apply and validate config, check job status and logs, view failures, enable or disable jobs, configure catch-up windows for missed runs, troubleshoot with doctor, and tune circuit breaker and throttle policies.
---

# Beagle

Beagle is a macOS job orchestrator. You define jobs in one global file, `~/.beagle/jobs.yaml`, and beagle manages them through launchd, abstracting away plist files and launchctl commands. Beagle owns scheduling itself via a single supervisor agent (see below), which is what lets missed runs catch up.

## Configuration Format

Config file: `~/.beagle/jobs.yaml` (override with `--config <path>`).

```yaml
version: 1
defaults:
  timezone: America/Chicago       # IANA timezone for scheduled jobs
  working_dir: /absolute/path     # must be absolute
  throttle_seconds: 30            # minimum seconds between runs (>= 0)
  catch_up: none                  # default catch-up window (none, or e.g. 6h)
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
    catch_up: 6h                  # run within 6h if the scheduled time was missed
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
- Job IDs must match `^[a-z0-9][a-z0-9_-]{1,63}$`. The id `supervisor` is reserved.
- `command[0]` must be an absolute path. `command` must be non-empty.
- `working_dir` (both defaults and per-job) must be absolute if set.
- `schedule.cron` is required for `schedule` jobs and forbidden for `service` jobs.
- `schedule.cron` must have exactly 5 fields.
- `timezone` values must be valid IANA timezone names.
- `catch_up` must be `none` (or empty) or a positive `h`/`m`/`s` duration <= `168h`. Day units (`1d`) are not accepted - use `24h`.
- `throttle_seconds` and all `circuit_breaker` fields must be >= 0.
- `restart` must be one of: `never`, `on-failure`, `always`.

## Scheduling and Catch-up

Beagle does not hand scheduled jobs to launchd's calendar timer. Instead it installs one **supervisor** agent that
launchd keeps ticking every minute (and on boot and wake-from-sleep). Each tick, the supervisor evaluates the cron
schedules itself and triggers any job that is due. Scheduled jobs themselves sit loaded as on-demand launchd agents.

This is what makes `catch_up` work. launchd alone loses a scheduled run if the Mac was powered off at fire time;
beagle's supervisor notices the missed occurrence on the next tick and, if it's within the job's `catch_up` window,
runs it once (multiple missed occurrences coalesce into a single run).

- `catch_up: none` (default) - strict; only fire at the scheduled minute.
- `catch_up: 6h` - allow a missed run to execute up to 6 hours late.

Service jobs are unaffected: they run continuously under launchd's `KeepAlive` per their `restart` policy.

## Commands

| Command | Description |
|---|---|
| `beagle validate` | Validate config file |
| `beagle apply` | Reconcile managed jobs (and the supervisor) with launchd |
| `beagle ls` | List configured jobs, their state, and last-run health |
| `beagle status <job>` | Show detailed status for a job |
| `beagle logs <job> [--stderr] [--tail N]` | Show job stdout (or stderr) logs |
| `beagle failures [--job <job>] [--limit N]` | Show recent failures |
| `beagle run-now <job>` | Trigger an immediate run |
| `beagle enable <job>` | Enable a job |
| `beagle disable <job>` | Disable a job |
| `beagle doctor` | Diagnostics, incl. whether the supervisor is loaded and ticking |

Global flag: `--config <path>` (defaults to `~/.beagle/jobs.yaml`).

(`beagle supervise` exists but is internal - it is the per-minute tick launchd invokes; you don't run it by hand.)

## Key Paths

| What | Path |
|---|---|
| Config | `~/.beagle/jobs.yaml` |
| Run history DB | `~/.beagle/beagle.db` |
| Job logs | `~/.beagle/logs/<job>/stdout.log` and `stderr.log` |
| Launchd plists | `~/Library/LaunchAgents/com.beagle.<user>.<job>.plist` |
| Supervisor plist | `~/Library/LaunchAgents/com.beagle.<user>.supervisor.plist` |

## Common Workflows

### Adding a Scheduled Job

1. Add a `schedule` type job to `~/.beagle/jobs.yaml` with a `cron` expression (and optionally a `catch_up` window).
2. Run `beagle validate` to check the config.
3. Run `beagle apply` to install the job.
4. Verify with `beagle ls` and `beagle status <job>`.

### Adding a Service

1. Add a `service` type job (no `cron` field).
2. Set `restart: on-failure` or `restart: always` as appropriate.
3. Run `beagle validate` then `beagle apply`.

### Debugging a Failing Job

1. `beagle ls` - the last-run column shows a failing job at a glance.
2. `beagle failures --job <job>` for recent failure history with exit codes.
3. `beagle logs <job>` and `beagle logs <job> --stderr` to inspect output.
4. `beagle status <job>` to check whether the job is loaded and enabled.
5. `beagle doctor` to verify the environment - including that the supervisor is loaded and ticking (if it isn't, no
   scheduled job will fire).
6. `beagle run-now <job>` to trigger a manual run and observe behavior.

### After Upgrading or Changing Beagle

After installing a new beagle version (or otherwise changing beagle itself), re-check the existing install before
trusting it. An API, config-schema, plist, or DB-schema change can leave already-installed jobs stale or silently not
firing:

1. `beagle validate` - confirm `~/.beagle/jobs.yaml` still parses under the new rules.
2. `beagle apply` - re-reconcile the jobs and the supervisor (plists embed absolute binary paths, so a rebuilt or moved
   binary needs a fresh `apply` to re-point them).
3. `beagle doctor` - confirm the supervisor is loaded and ticking.
4. `beagle ls` - spot-check job state and last-run health.
