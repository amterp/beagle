# Beagle v1 Quickstart

## Example config (`~/.beagle/jobs.yaml`)

```yaml
version: 1
defaults:
  timezone: America/Chicago   # IANA name, or `local` to follow this machine
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
- `beagle restart <job> [--force]`
- `beagle run-now <job> [--force]`
- `beagle start <job>`
- `beagle stop <job>`
- `beagle doctor`

## Notes

- There is one config: `~/.beagle/jobs.yaml`. Pass `--config <path>` to override it (e.g. for testing).
- Beagle owns scheduling: a single supervisor agent ticks every minute (and on boot/wake) and triggers due jobs. This
  is what enables `catch_up` for runs missed while the Mac was powered off. `beagle doctor` reports whether the
  supervisor is loaded and actually ticking.
- `catch_up` is `none` (default, strict) or a duration in `h`/`m`/`s`/`d`/`w` up to `366d`. Missed occurrences coalesce
  into a single catch-up run. A job beagle has not seen before adopts its last occurrence as a baseline rather than
  running it, so adding a job never fires it retroactively.
- `apply` skips a job whose config hasn't changed, so it won't bounce a service onto a rebuilt binary. `beagle restart
  <job>` does that; `run-now` is the same operation named for scheduled jobs.
- `stop` unloads a job and is undone by the next `apply` or reboot. `enabled: false` in the config is the durable off
  switch. `enable`/`disable` are the old names of `start`/`stop` and still work.
- `restart`/`run-now` refuse when the circuit breaker is open, since the run would be recorded as skipped without
  executing. `--force` clears the breaker.
- `beagle restart supervisor` re-arms the scheduler when doctor reports it loaded but not ticking - `apply` cannot, since
  it sees the agent as unchanged.
- Doctor reads launchd's registration for the supervisor as well as beagle's heartbeat, and reports a program path that
  no longer exists, launchd's penalty box, and a non-zero exit from the last tick. The heartbeat alone reads "ticking"
  for three minutes after the supervisor dies. A stale program path needs `beagle apply`, which rewrites the plist;
  `restart supervisor` reloads the same stale agent.
- Beagle hides scheduler implementation details from daily workflows.
- Everything lives under `~/.beagle/`: config (`jobs.yaml`), run history (`beagle.db`), and logs (`logs/<job>/`).
- `beagle ls` splits jobs into SERVICES (uptime, pid) and SCHEDULES (schedule, next fire, last-run outcome and
  duration), so a "loaded but failing" job is visible and so is one whose next run is further off than you expected.
- `schedule.timezone` accepts `local`, meaning "re-resolve to whatever zone this machine is in". Fixed IANA names pin a
  job to a place (a market close); `local` pins it to you (a morning digest). An unset timezone means UTC, not
  machine-local - `ls` tags any zone that differs from the machine's so that surprise is visible.
