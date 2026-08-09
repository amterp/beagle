# AGENTS.md

Guidance for AI agents working in the beagle codebase.

## Doctor the local install after beagle changes

Beagle isn't just a library we edit - there's a live install on this machine:
a built binary on `PATH`, generated launchd plists under
`~/Library/LaunchAgents/`, a SQLite run-log DB, and real jobs defined in
`~/.beagle/jobs.yaml`. Those artifacts were produced by whatever version of
beagle was current when we last ran `apply`, so our code changes can leave
them stale or silently broken - the worst case being a scheduled job that
just stops firing with no error.

After changing beagle - **especially** anything touching the config schema,
the plist format, the run-log DB schema, label naming, or the command-line
surface - re-doctor the local install before calling the work done:

1. Rebuild/reinstall the binary so the install matches our code. Plists embed
   absolute `beagle`/`beagle-run` paths, so a moved or rebuilt binary may need
   a fresh `apply` to re-point them.
2. `beagle validate` - confirm the existing `~/.beagle/jobs.yaml` still parses
   under the new rules.
3. `beagle apply` - re-reconcile the jobs and the supervisor plist.
4. `beagle doctor` - confirm the home dir, runner, and **supervisor (loaded
   and ticking)** are healthy. A loaded-but-not-ticking supervisor means no
   scheduled job fires; `beagle restart supervisor` re-arms it, and `apply`
   cannot, since it sees a loaded agent whose plist matches.
5. `beagle ls` - spot-check that jobs still report sane state and last-run
   health.
6. `beagle restart <job>` for each running service, if `beagle-run` or anything
   it links changed. Scheduled jobs exec a fresh `beagle-run` every run, but a
   long-running service is still executing the wrapper binary it started with -
   and `apply` will not replace it, since the job's plist is unchanged. Skip this
   only when the change cannot affect the runner.

The same applies after bumping the installed beagle version for any reason.
The run-log DB tracks a schema version and wipes a foreign schema on open, so
a version jump can reset run history - that's expected, but verify jobs still
fire afterward.

A wipe also clears `schedule_state`, the per-job record of which occurrence was
last handled. The supervisor then treats every schedule job as newly seen and
adopts its most recent occurrence as a baseline rather than running it, so one
genuinely missed run per catch-up job can be dropped across such an upgrade.
Carrying `schedule_state` across a schema rebuild would remove that hazard and
is worth doing before the next schema change.

## `schedule_state.last_fire` is structured, and the schema hides that

The column's own doc comment calls it "an opaque string owned by the caller",
which is now carrying real weight: the supervisor packs three fields into it as
`<wall-clock>|<zone>|<RFC3339 instant>`. Read it only through
`decodeState`/`encodeState` in `internal/supervisor/state.go`. Comparing the
raw column value - which is what the code did before the fields existed - sorts
on the zone name and mis-deduplicates, and nothing in the DDL would warn you.

The format is deliberately a packed string rather than new columns, because
adding columns means bumping the schema version, and that wipes the table along
with the missed run described above. A value with no `|` is a pre-upgrade row
and must keep working; `decodeState` handles it.
