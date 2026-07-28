# Beagle v1 Quickstart

## Example config (`~/.beagle/jobs.yaml`)

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
    catch_up: 6h          # run within 6h if the Mac was off at 5am; omit/none = strict
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

- There is one config: `~/.beagle/jobs.yaml`. Pass `--config <path>` to override it (e.g. for testing).
- Beagle owns scheduling: a single supervisor agent ticks every minute (and on boot/wake) and triggers due jobs. This
  is what enables `catch_up` for runs missed while the Mac was powered off. `beagle doctor` reports whether the
  supervisor is loaded and actually ticking.
- `catch_up` is `none` (default, strict) or a duration in `h`/`m`/`s`/`d`/`w` up to `366d`. Missed occurrences coalesce
  into a single catch-up run. A job beagle has not seen before adopts its last occurrence as a baseline rather than
  running it, so adding a job never fires it retroactively.
- Beagle hides scheduler implementation details from daily workflows.
- Everything lives under `~/.beagle/`: config (`jobs.yaml`), run history (`beagle.db`), and logs (`logs/<job>/`).
- `beagle ls` and `beagle status` show each job's last-run outcome, so a "loaded but failing" job is visible.
