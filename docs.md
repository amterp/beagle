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

- `beagle profile register <name> <config-path>`
- `beagle profile ls`
- `beagle profile use <name>`
- `beagle profile rm <name>`
- `beagle validate`
- `beagle apply`
- `beagle ls`
- `beagle status <job|profile:job>`
- `beagle logs <job|profile:job> [--stderr] [--tail N]`
- `beagle failures [--job <job|profile:job>] [--limit N]`
- `beagle run-now <job|profile:job>`
- `beagle enable <job|profile:job>`
- `beagle disable <job|profile:job>`
- `beagle doctor`

## Notes

- Use `--profile <name>` globally on config-backed commands.
- Command resolution precedence is `--config` then `--profile` then active profile then local `./beagle.yaml`.
- If you use `profile:job` it overrides `--profile` for that command target.
- Beagle hides scheduler implementation details from daily workflows.
- Job execution history and failures are persisted in `~/.local/share/beagle/beagle.db`.
- Job logs default to `~/.local/share/beagle/logs/<namespace>/<job>/`.
