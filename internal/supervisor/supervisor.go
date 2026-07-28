// Package supervisor is beagle's scheduler. launchd keeps one supervisor agent
// ticking (on boot, on wake, and every minute); each tick evaluates the cron
// schedules itself and kicks the jobs that are due. Owning the clock - rather
// than handing each job to launchd's StartCalendarInterval - is what lets a job
// missed while the Mac was powered off run late, within a per-job catch-up
// window, instead of being silently dropped.
package supervisor

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/core"
	"github.com/amterp/beagle/internal/launchd"
	"github.com/amterp/beagle/internal/runlog"
)

// GraceWindow is how close to its scheduled minute a strict (catch_up: none)
// job must be evaluated to still fire. It absorbs tick jitter; schedule_state
// dedup prevents a double fire when consecutive ticks both see the occurrence.
const GraceWindow = 2 * time.Minute

// occurrenceLayout encodes a fire as a wall-clock identity. Two distinct
// instants that share a wall-clock minute (the repeated hour at DST fall-back)
// collapse to one occurrence, so the job fires once. The layout is sortable, so
// string comparison is chronological.
const occurrenceLayout = "2006-01-02T15:04"

// TickHeartbeatKey is the meta key the supervisor stamps each tick so doctor
// can tell the scheduler is alive.
const TickHeartbeatKey = "supervisor_tick"

type Deps struct {
	Store    *runlog.Store
	Runner   launchd.CommandRunner
	Username string
	UID      string
	Now      time.Time
	Log      io.Writer
}

type Result struct {
	Fired []string
	// Adopted lists jobs seen for the first time, whose most recent occurrence
	// was taken as a baseline rather than run.
	Adopted []string
	Errors  []string
}

// Tick evaluates every schedule job once and kicks those whose most recent
// occurrence is due, unhandled, and within the catch-up window. It is pure with
// respect to its Deps (clock, runner, store all injected) so it can be tested
// deterministically.
func Tick(cfg config.File, deps Deps) (Result, error) {
	var res Result
	resolved, err := config.Resolve(cfg)
	if err != nil {
		return res, err
	}
	ctx := context.Background()

	for _, j := range resolved {
		if j.Type != "schedule" || !j.Enabled {
			continue
		}

		loc := time.UTC
		if tz := j.Schedule.Timezone; tz != "" {
			l, err := time.LoadLocation(tz)
			if err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: bad timezone %q: %v", j.ID, tz, err))
				continue
			}
			loc = l
		}

		spec, err := launchd.ParseSpec(j.Schedule.Cron)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: bad cron: %v", j.ID, err))
			continue
		}

		// Strict jobs look back only a grace; catch-up jobs look back their
		// window. Either way PrevFire bounds the search, and "no match" means
		// nothing is due right now.
		window := j.CatchUp
		if window <= 0 {
			window = GraceWindow
		}
		prevFire, ok := spec.PrevFire(deps.Now, loc, window)
		if !ok {
			continue
		}

		occ := prevFire.In(loc).Format(occurrenceLayout)
		last, hadLast, err := deps.Store.GetScheduleFire(ctx, j.ID)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: read schedule_state: %v", j.ID, err))
			continue
		}
		if hadLast && occ <= last {
			continue // already handled (or a backward-clock stale occurrence)
		}

		// First sight: no state row means beagle has never been responsible for
		// this job, so an occurrence older than the grace window predates its
		// existence and was never owed. Adopt it as the baseline instead of
		// running it; catch-up works fully from the next occurrence on. Without
		// this, adding a job with a long catch_up would run it on the spot.
		//
		// This is the one place we prefer dropping a run to duplicating one (see
		// the kick-failure note below), because the run was never missed - it
		// simply happened before the job existed. The exception is narrow: it
		// applies only when there is no recorded state at all.
		//
		// The floor mirrors PrevFire's own bound rather than testing
		// Now.Sub(prevFire), which would be wrong: PrevFire measures from the
		// truncated minute, so at 07:02:59 a strict job legitimately returns the
		// 07:00 occurrence - 2m59s "late" against a 2m grace it never exceeded.
		if !hadLast && prevFire.Before(deps.Now.Truncate(time.Minute).Add(-GraceWindow)) {
			if err := deps.Store.RecordScheduleFire(ctx, j.ID, occ, deps.Now); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: adopt occurrence: %v", j.ID, err))
				continue
			}
			res.Adopted = append(res.Adopted, j.ID)
			if deps.Log != nil {
				fmt.Fprintf(deps.Log, "supervisor: %s: first sight - adopting %s as the baseline without running it, "+
					"since a job beagle has never seen has missed nothing. Catch-up applies from its next occurrence. "+
					"If this followed a beagle upgrade, a run-log schema change cleared schedule state, in which case one "+
					"genuinely missed run may have been skipped - check `beagle ls` if the job matters.\n", j.ID, occ)
			}
			continue
		}

		label := core.BuildLabel(deps.Username, j.ID)
		if err := launchd.Kick(deps.UID, label, false, deps.Runner); err != nil {
			// Leave schedule_state untouched so the next tick retries this
			// occurrence rather than losing it.
			res.Errors = append(res.Errors, fmt.Sprintf("%s: kick: %v", j.ID, err))
			continue
		}
		// Record only after a successful kick. A failed record (rare) risks one
		// duplicate next tick - deliberately biased that way: a duplicate run is
		// far less harmful than a silently dropped one.
		if err := deps.Store.RecordScheduleFire(ctx, j.ID, occ, deps.Now); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: record fire: %v", j.ID, err))
		}
		res.Fired = append(res.Fired, j.ID)
	}

	return res, nil
}
