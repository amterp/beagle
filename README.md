# Beagle

Beagle is a macOS CLI that manages scheduled and always-on background jobs. You define jobs in a `beagle.yaml` file and
beagle handles the rest - installing them into macOS's launchd, tracking run history, capturing logs, and tripping
circuit breakers when things go wrong.

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

Create a `beagle.yaml` in your project:

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

That's it. Your jobs are now running under launchd.

## How It Works

Beagle reads your `beagle.yaml`, generates launchd plists, and uses `launchctl` to load/unload them. A small wrapper
binary (`beagle-run`) sits between launchd and your actual command, recording each run's start time, exit code, and
duration in a local SQLite database. This gives you run history, failure tracking, and circuit breaker behavior without
any external dependencies.

**Scheduled jobs** run on a cron schedule. **Service jobs** run continuously and restart according to their `restart`
policy (`never`, `on-failure`, or `always`).

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
beagle ls                                   List jobs and their state
beagle status <job>                         Detailed job status
beagle logs <job> [--stderr] [--tail N]     View job output
beagle failures [--job <job>] [--limit N]   Recent failure history
beagle run-now <job>                        Trigger an immediate run
beagle enable <job>                         Enable a job
beagle disable <job>                        Disable a job
beagle doctor                               Environment diagnostics
```

## Multi-Repo Profiles

Beagle supports named profiles so you can manage jobs across multiple repositories from a single CLI install. Each
profile points to a different `beagle.yaml` and gets its own namespace, keeping launchd labels and logs isolated.

```sh
beagle profile register myapp /path/to/myapp/beagle.yaml
beagle profile register infra /path/to/infra/beagle.yaml
beagle profile use myapp
beagle ls                          # shows myapp's jobs
beagle status infra:my_worker      # reach into another profile
```

Config resolution: `--config` > `--profile` > active profile > `./beagle.yaml`.

## Configuration Reference

### Job Fields

| Field               | Type                    | Description                                                                    |
|---------------------|-------------------------|--------------------------------------------------------------------------------|
| `type`              | `schedule` or `service` | Required. Schedule jobs need a cron expression; service jobs run continuously. |
| `command`           | string list             | Required. First element must be an absolute path.                              |
| `schedule.cron`     | string                  | 5-field cron expression. Required for schedule, forbidden for service.         |
| `schedule.timezone` | string                  | IANA timezone. Overrides `defaults.timezone` for this job.                     |
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

- Job IDs: `^[a-z0-9][a-z0-9_-]{1,63}$`
- All paths (`command[0]`, `working_dir`) must be absolute
- Timezones must be valid IANA names
- Numeric fields (`throttle_seconds`, circuit breaker values) must be >= 0

## Where Things Live

| What             | Path                                            |
|------------------|-------------------------------------------------|
| Project config   | `beagle.yaml`                                   |
| Profile registry | `~/.config/beagle/profiles.yaml`                |
| Run history      | `~/.local/share/beagle/beagle.db`               |
| Job logs         | `~/.local/share/beagle/logs/<namespace>/<job>/` |
| Launchd plists   | `~/Library/LaunchAgents/com.beagle.*.plist`     |
