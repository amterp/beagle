# Beagle

Beagle is a macOS CLI that manages scheduled and always-on background jobs. You define jobs in one file -
`~/.beagle/jobs.yaml` - and beagle handles the rest: installing them into macOS's launchd, tracking run history,
capturing logs, and tripping circuit breakers when things go wrong.

You never touch plist files or launchctl directly.

## Who It's For

Anyone running recurring tasks or long-lived services on macOS who wants something simpler than hand-rolling launchd
plists. Developers with cron jobs, backup scripts, local workers, periodic reports - if it runs in the background on
your Mac, beagle can manage it.

## Quick Start

```sh
go install github.com/amterp/beagle/cmd/beagle@latest
go install github.com/amterp/beagle/cmd/beagle-run@latest
```

Create `~/.beagle/jobs.yaml`:

```yaml
version: 1
defaults:
  timezone: America/Chicago
  throttle_seconds: 30
  circuit_breaker:
    max_failures: 5
    window_seconds: 600
    cooldown_seconds: 1800
jobs:
  monthly_report:
    type: schedule
    command: [ "/usr/local/bin/report", "--send" ]
    schedule:
      cron: "0 5 1 * *"
    catch_up: 6h        # if the Mac was off at 5am, still run within 6 hours

  worker_a:
    type: service
    command: [ "/usr/local/bin/worker-a" ]
    restart: on-failure
```

Validate and apply:

```sh
beagle validate
beagle apply
```

That's it. Your jobs are now managed.

## How It Works

Beagle reads `~/.beagle/jobs.yaml` and reconciles it into launchd. Each job becomes a launchd agent labelled
`com.beagle.<user>.<job>`. A small wrapper binary (`beagle-run`) sits between launchd and your actual command,
recording each run's start time, exit code, and duration in a local SQLite database. This gives you run history,
failure tracking, and circuit breaker behavior with no external dependencies.

**Beagle owns scheduling.** Rather than handing each scheduled job to launchd's calendar timer, beagle installs a
single **supervisor** agent that wakes up every minute (and on boot and on wake-from-sleep) and decides which jobs are
due, triggering them itself. This is what makes catch-up possible (see below). launchd's job is reduced to keeping the
one supervisor ticking; the supervisor schedules everything else.

**Scheduled jobs** run on a cron schedule. **Service jobs** run continuously under launchd and restart according to
their `restart` policy (`never`, `on-failure`, or `always`).

### Catch-up (missed runs)

launchd's native behavior for a missed scheduled run is awkward: if the Mac is asleep at fire time it runs on wake, but
if the Mac is **powered off** the run is silently lost - with no control either way. Because beagle owns scheduling, it
can do better. Each scheduled job has a `catch_up` window:

- `catch_up: none` (the default) - strict. The job only runs at its scheduled minute; a miss is a miss.
- `catch_up: 6h` (any duration in `h`/`m`/`s`, up to `168h`) - if the job's scheduled time was missed (e.g. the Mac was
  off) and you're still within the window when it next comes up, beagle runs it once. Multiple missed occurrences
  coalesce into a single catch-up run.

### Circuit Breaker

If a job fails repeatedly, beagle trips a circuit breaker and stops running it until a cooldown period expires. This
prevents a broken job from flooding your logs or hammering an external service. Configure thresholds per-job or set
defaults globally.

### Throttling

`throttle_seconds` sets a minimum interval between runs, preventing a job from being triggered more frequently than
intended.

## Commands

```
beagle validate                             Validate config
beagle apply                                Install/update/remove jobs in launchd
beagle ls                                   List jobs, their state, and last-run health
beagle status <job>                         Detailed job status
beagle logs <job> [--stderr] [--tail N]     View job output
beagle failures [--job <job>] [--limit N]   Recent failure history
beagle run-now <job>                        Trigger an immediate run
beagle enable <job>                         Enable a job
beagle disable <job>                        Disable a job
beagle doctor                               Environment diagnostics (incl. supervisor health)
```

By default every command operates on `~/.beagle/jobs.yaml`. Pass `--config <path>` to point at a different file (handy
for testing).

## Configuration Reference

### Job Fields

| Field               | Type                    | Description                                                                    |
|---------------------|-------------------------|--------------------------------------------------------------------------------|
| `type`              | `schedule` or `service` | Required. Schedule jobs need a cron expression; service jobs run continuously. |
| `command`           | string list             | Required. First element must be an absolute path.                              |
| `schedule.cron`     | string                  | 5-field cron expression. Required for schedule, forbidden for service.         |
| `schedule.timezone` | string                  | IANA timezone. Overrides `defaults.timezone` for this job.                     |
| `catch_up`          | string                  | `none` (default) or a duration like `6h`. How late a missed run may still go.  |
| `restart`           | string                  | `never`, `on-failure`, or `always`.                                            |
| `enabled`           | bool                    | Default `true`. Set `false` to skip during apply.                              |
| `working_dir`       | string                  | Absolute path. Overrides `defaults.working_dir`.                               |
| `env`               | map                     | Environment variables, merged with `defaults.env`.                             |
| `throttle_seconds`  | int                     | Minimum seconds between runs. Overrides default.                               |
| `circuit_breaker`   | object                  | `max_failures`, `window_seconds`, `cooldown_seconds`. Overrides default.       |

### Defaults

All job fields above (except `type`, `command`, `schedule`, and `enabled`) can be set under `defaults` to apply
globally. Jobs override defaults where specified.

### Validation

- Job IDs: `^[a-z0-9][a-z0-9_-]{1,63}$` (the id `supervisor` is reserved).
- All paths (`command[0]`, `working_dir`) must be absolute.
- Timezones must be valid IANA names.
- `catch_up` must be `none` or a positive `h`/`m`/`s` duration <= `168h` (days like `1d` aren't accepted - use `24h`).
- Numeric fields (`throttle_seconds`, circuit breaker values) must be >= 0.

## Where Things Live

| What             | Path                                          |
|------------------|-----------------------------------------------|
| Config           | `~/.beagle/jobs.yaml`                          |
| Run history + DB | `~/.beagle/beagle.db`                          |
| Job logs         | `~/.beagle/logs/<job>/`                        |
| Launchd plists   | `~/Library/LaunchAgents/com.beagle.<user>.*`   |
