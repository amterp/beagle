# Beagle v1 Quickstart

## Example config

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
    command: ["/usr/local/bin/report", "--send"]
    schedule:
      cron: "0 5 1 * *"
      timezone: America/Chicago
    restart: never

  worker_a:
    type: service
    command: ["/usr/local/bin/worker-a"]
    restart: on-failure
```

## Commands

- `beagle validate`
- `beagle apply`
- `beagle ls`
- `beagle status <job>`
- `beagle logs <job> [--stderr] [--tail N]`
- `beagle failures [--job <job>] [--limit N]`
- `beagle run-now <job>`
- `beagle enable <job>`
- `beagle disable <job>`
- `beagle doctor`

## Notes

- Beagle hides scheduler implementation details from daily workflows.
- Job execution history and failures are persisted in `~/.local/share/beagle/beagle.db`.
- Job logs default to `~/.local/share/beagle/logs/<job>/`.
