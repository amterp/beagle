package supervisor

import (
	"strings"
	"time"
)

// stateSep separates the fields packed into schedule_state.last_fire. The
// column is documented as an opaque string owned by this package, so the
// format can grow without a run-log schema change - which matters, because a
// schema version bump drops every table and costs one missed run per job.
const stateSep = "|"

// scheduleState is what the supervisor remembers about a job's last fire.
//
// Occurrence alone was enough while every job stayed in one zone. It is a
// wall-clock identity ("2006-01-02T15:04") with no offset, which is what makes
// DST fall-back fire exactly once: the repeated hour yields one key, not two.
// But wall-clock keys from different zones are not comparable, and with
// `timezone: local` a job changes zone whenever the machine moves. Zone and
// FiredAt exist to detect that and fall back to comparing absolute instants.
type scheduleState struct {
	Occurrence string
	Zone       string
	FiredAt    time.Time
}

// encodeState packs a fire into the opaque column value.
func encodeState(occurrence, zone string, firedAt time.Time) string {
	return strings.Join([]string{
		occurrence,
		zone,
		firedAt.UTC().Format(time.RFC3339),
	}, stateSep)
}

// decodeState unpacks a column value. Rows written before this format existed
// hold a bare occurrence key; they decode with an empty Zone and a zero
// FiredAt, which alreadyHandled reads as "compare wall clock, as before".
func decodeState(raw string) scheduleState {
	parts := strings.Split(raw, stateSep)
	if len(parts) != 3 {
		return scheduleState{Occurrence: raw}
	}
	st := scheduleState{Occurrence: parts[0], Zone: parts[1]}
	if at, err := time.Parse(time.RFC3339, parts[2]); err == nil {
		st.FiredAt = at
	}
	return st
}

// alreadyHandled reports whether the supervisor has dealt with this occurrence.
//
// Within one zone this is the original test: occurrence keys sort
// chronologically, so anything at or below the stored key is done.
//
// Across a zone change that test breaks, and breaks in the direction beagle
// least tolerates. Carry the machine west and the wall clock rewinds, so a
// genuinely new occurrence sorts below the stored one and is skipped - not
// once, but until the clock catches up. Carry it east and an occurrence that
// already ran sorts above and runs twice. Absolute instants have neither
// problem, so when the zone changed they decide instead. That keeps the DST
// collapse intact for the common case, where the zone is stable.
func alreadyHandled(prev scheduleState, occurrence, zone string, occurredAt time.Time) bool {
	if prev.Zone != "" && zone != "" && prev.Zone != zone && !prev.FiredAt.IsZero() {
		return !occurredAt.After(prev.FiredAt)
	}
	return occurrence <= prev.Occurrence
}
