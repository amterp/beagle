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
   scheduled job fires.
5. `beagle ls` - spot-check that jobs still report sane state and last-run
   health.

The same applies after bumping the installed beagle version for any reason.
The run-log DB tracks a schema version and wipes a foreign schema on open, so
a version jump can reset run history - that's expected, but verify jobs still
fire afterward.
