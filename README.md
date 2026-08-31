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
- `catch_up: 6h` (any duration in `h`/`m`/`s`/`d`/`w`, up to `366d`) - if the job's scheduled time was missed (e.g. the
  Mac was off) and you're still within the window when it next comes up, beagle runs it once. Multiple missed
  occurrences coalesce into a single catch-up run, however many were missed.

A job beagle has not seen before adopts its most recent occurrence as a baseline instead of running it, so adding a job
never triggers a retroactive run on the spot. Catch-up applies from its next occurrence onward.

### Timezones

`schedule.timezone` takes an IANA name (`America/Chicago`), or the literal `local`.

An **IANA name pins the job to a place**. Use it when the schedule is about somewhere: a market close, a ticket on-sale
time, a provider's business day. `finlab_harvest` runs at 18:00 `America/New_York` because that is after the US market
closes, and it should still be after the close when you are in Lisbon.

**`local` pins the job to you.** The zone is re-resolved on every supervisor tick, so the schedule moves with the
machine: a `local` job at 07:00 fires at 07:00 wherever you wake up. Use it for jobs whose point is to reach you at a
civilised hour.

Leaving `timezone` unset means **UTC**, not machine-local. That is a wart, kept because changing it would silently
reschedule every job that omits the field. `beagle ls` marks any job whose zone differs from the machine's, so an
accidental UTC schedule shows up as a `UTC` tag rather than staying invisible.

When the machine does change zone, beagle notices. Each fire is recorded with the zone it was read in, so a move is
detected and the "have we already run this?" check falls back to comparing absolute instants instead of wall clocks.
Without that, carrying a laptop west would rewind the wall clock and silently skip every occurrence until it caught up -
six hours of a `*/15` job is 24 dropped runs. The trade is beagle's usual one: a moved machine may run one occurrence
twice, but will not silently drop one.

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
beagle ls                                   List jobs, split into services and schedules
beagle status <job>                         Detailed job status
beagle logs <job> [--stderr] [--tail N]     View job output
beagle failures [--job <job>] [--limit N]   Recent failure history
beagle restart <job> [--force]              Stop the running instance, start a fresh one
beagle run-now <job> [--force]              Run a job now, outside its schedule
beagle start <job>                          Start a stopped job
beagle stop <job>                           Stop a job until the next apply
beagle doctor                               Environment diagnostics (incl. supervisor health)
```

By default every command operates on `~/.beagle/jobs.yaml`. Pass `--config <path>` to point at a different file (handy
for testing).

## Restarting, Stopping, and Rerunning

`beagle apply` reconciles config into launchd. It does not touch a job whose config hasn't changed, so rebuilding a
binary leaves the old process running - the plist still says the same thing. Bouncing a job is a separate command:

```sh
beagle restart iris_serve     # service picks up the rebuilt binary
beagle run-now mail_sync      # scheduled job runs now, off its schedule
```

Both do the same thing - kill any in-flight instance, start a fresh one - under the two names people reach for. Either
works on either job type; they differ only in which one reads right at the terminal. A stopped job is loaded back into
launchd first, so restarting something you stopped works without an intervening `apply`.

`stop` and `start` control whether a job is loaded at all:

```sh
beagle stop kan_serve         # service's process ends; scheduled job stops firing
beagle start kan_serve
```

**`stop` is not durable.** It unloads the launchd agent, and the next `beagle apply` or reboot brings the job back,
because `jobs.yaml` is the single source of truth. To keep a job down, set `enabled: false` in the config and apply. A
stopped *scheduled* job also makes the supervisor log an error each time that job comes due, since there's no agent to
trigger.

`beagle enable` and `beagle disable` are the former names of `start` and `stop`. They still work and print the new name.

### When the circuit breaker is open

A tripped breaker makes `beagle-run` record a run as `skipped` without executing the command, so `restart` refuses
rather than reporting a success that didn't happen:

```
$ beagle restart helm_refresh
restart failed: circuit breaker is open until 2026-07-29 18:42:11 (12m0s from now).
  5 failures in the last 10m0s tripped it, so this run would be recorded as skipped and the command would never execute.
  Fix the cause (`beagle logs helm_refresh --stderr`, `beagle failures --job helm_refresh`), or pass --force to clear
  the breaker and run anyway.
```

`--force` clears the breaker and runs. It resets the failure count, so the next failure starts a fresh window.

### Restarting the scheduler

If `beagle doctor` reports the supervisor loaded but not ticking, no scheduled job is firing and `apply` won't fix it -
apply sees a loaded agent whose plist matches and calls it unchanged. Re-arm it:

```sh
beagle restart supervisor
```

Doctor reads launchd's own registration for the supervisor, not just beagle's heartbeat, and reports three failures the
heartbeat cannot see: a program path that no longer exists, launchd's penalty box after repeated spawn failures, and a
non-zero exit from the most recent tick. The heartbeat records that a tick once succeeded, so on its own it keeps
reading "ticking" for three minutes after the supervisor dies.

The stale-path case is worth naming, because a package upgrade used to cause it. The supervisor plist holds whichever
absolute `beagle` path applied it, so an `apply` run through `/opt/homebrew/bin/beagle` records that symlink rather than
the versioned Cellar file it points at. Recording the resolved target instead would strand the agent on the next
upgrade: the path disappears, launchd cannot spawn the supervisor, and it exits `EX_CONFIG` into the penalty box with
nothing written to any log. `beagle apply` re-points a plist in that state; `beagle restart supervisor` does not, since
it reloads the same stale agent.

## Configuration Reference

### Job Fields

| Field               | Type                    | Description                                                                    |
|---------------------|-------------------------|--------------------------------------------------------------------------------|
| `type`              | `schedule` or `service` | Required. Schedule jobs need a cron expression; service jobs run continuously. |
| `command`           | string list             | Required. First element must be an absolute path.                              |
| `schedule.cron`     | string                  | 5-field cron expression. Required for schedule, forbidden for service.         |
| `schedule.timezone` | string                  | IANA timezone, or `local` to follow the machine. Overrides `defaults.timezone`. |
| `catch_up`          | string                  | `none` (default) or a duration like `6h`, `3d`, `2w`. How late a missed run may go. |
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

- Job IDs: `^[a-z0-9][a-z0-9_-]{0,63}$` (the id `supervisor` is reserved).
- All paths (`command[0]`, `working_dir`) must be absolute.
- Timezones must be valid IANA names, or the literal `local`.
- `catch_up` must be `none` or a positive duration <= `366d`, written in `h`/`m`/`s` plus `d` (days) and `w` (weeks):
  `6h`, `90m`, `3d`, `2w`, `1d12h`. Days and weeks are fixed 24h and 168h spans, not calendar offsets.
- Numeric fields (`throttle_seconds`, circuit breaker values) must be >= 0.

## Where Things Live

| What             | Path                                          |
|------------------|-----------------------------------------------|
| Config           | `~/.beagle/jobs.yaml`                          |
| Run history + DB | `~/.beagle/beagle.db`                          |
| Job logs         | `~/.beagle/logs/<job>/`                        |
| Launchd plists   | `~/Library/LaunchAgents/com.beagle.<user>.*`   |
