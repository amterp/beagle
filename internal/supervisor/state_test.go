package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/amterp/beagle/internal/config"
)

// scheduleJobIn is scheduleJob with an explicit timezone, for the cases where
// the zone is the thing under test.
func scheduleJobIn(cron, catchUp, tz string) config.File {
	f := scheduleJob(cron, catchUp)
	j := f.Jobs["job_a"]
	j.Schedule.Timezone = tz
	f.Jobs["job_a"] = j
	return f
}

func TestDecodeStateRoundTrip(t *testing.T) {
	at := time.Date(2026, 8, 9, 6, 0, 0, 0, time.UTC)
	got := decodeState(encodeState("2026-08-09T07:00", "Europe/Lisbon", at))

	if got.Occurrence != "2026-08-09T07:00" || got.Zone != "Europe/Lisbon" {
		t.Fatalf("round trip lost fields: %+v", got)
	}
	if !got.FiredAt.Equal(at) {
		t.Fatalf("FiredAt = %v, want %v", got.FiredAt, at)
	}
}

// TestDecodeStateLegacy covers rows written before the zone was recorded. They
// must keep working rather than being read as a garbled occurrence, because a
// run-log schema bump is the only thing that would clear them and we
// deliberately avoided one.
func TestDecodeStateLegacy(t *testing.T) {
	got := decodeState("2026-08-09T07:00")
	if got.Occurrence != "2026-08-09T07:00" {
		t.Fatalf("Occurrence = %q, want the bare key", got.Occurrence)
	}
	if got.Zone != "" || !got.FiredAt.IsZero() {
		t.Fatalf("expected no zone and no instant, got %+v", got)
	}
	// And it must still dedup on wall clock, the pre-upgrade behavior. Both
	// directions matter: every job on an upgraded machine meets a legacy row at
	// its first tick, and getting this wrong either replays a run or wedges the
	// job until the format happens to be rewritten.
	fired := time.Date(2026, 8, 9, 7, 0, 0, 0, time.UTC)
	if !alreadyHandled(got, "2026-08-09T07:00", "Europe/Lisbon", fired) {
		t.Error("a legacy row must still suppress the occurrence it recorded")
	}
	if alreadyHandled(got, "2026-08-10T07:00", "Europe/Lisbon", fired.Add(24*time.Hour)) {
		t.Error("a legacy row must not suppress the following occurrence")
	}
}

// TestTickZoneChangeWestDoesNotSkip is the failure this whole mechanism exists
// to prevent. Carrying the machine west rewinds the wall clock, so the new
// 07:00 occurrence sorts equal to the one already recorded in the old zone. The
// old string compare read that as "handled" and dropped the run silently.
func TestTickZoneChangeWestDoesNotSkip(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	ctx := context.Background()

	lisbon, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// Fired at 07:00 Lisbon (06:00Z), recorded under Europe/Lisbon.
	firedAt := time.Date(2026, 8, 9, 7, 0, 0, 0, lisbon)
	seedFire(t, store, "job_a", encodeState("2026-08-09T07:00", "Europe/Lisbon", firedAt))

	// Now in Chicago at 08:00, three hours past that zone's own 07:00 (12:00Z),
	// which is six hours after the Lisbon fire. A genuinely new occurrence.
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, chicago)
	res, err := Tick(scheduleJobIn("0 7 * * *", "12h", "America/Chicago"), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 || res.Fired[0] != "job_a" {
		t.Fatalf("moving west must not swallow the new occurrence, got fired=%v errors=%v", res.Fired, res.Errors)
	}

	// The next tick in the same zone must not repeat it.
	res2, err := Tick(scheduleJobIn("0 7 * * *", "12h", "America/Chicago"),
		deps(store, runner, now.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Fired) != 0 {
		t.Fatalf("expected no repeat once the zone is stable again, got %v", res2.Fired)
	}

	got, _, err := store.GetScheduleFire(ctx, "job_a")
	if err != nil {
		t.Fatal(err)
	}
	if zone := decodeState(got).Zone; zone != "America/Chicago" {
		t.Fatalf("expected the new zone recorded, got %q", zone)
	}
}

// TestTickZoneChangeEastDoesNotDoubleFire is the other direction. Carrying the
// machine east jumps the wall clock forward, so that zone's 07:00 has already
// passed in absolute terms - earlier than the run that already happened. It
// must not run again.
func TestTickZoneChangeEastDoesNotDoubleFire(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}

	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	lisbon, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// Fired at 07:00 Chicago (12:00Z), recorded under America/Chicago.
	firedAt := time.Date(2026, 8, 9, 7, 0, 0, 0, chicago)
	seedFire(t, store, "job_a", encodeState("2026-08-09T07:00", "America/Chicago", firedAt))

	// Now in Lisbon at 19:00. Lisbon's own 07:00 today was 06:00Z, six hours
	// before the run that already happened.
	now := time.Date(2026, 8, 9, 19, 0, 0, 0, lisbon)
	res, err := Tick(scheduleJobIn("0 7 * * *", "12h", "Europe/Lisbon"), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 {
		t.Fatalf("moving east must not re-run an occurrence already handled, got %v", res.Fired)
	}
}

// TestTickLocalZoneResolvesAndFires covers the `local` value end to end: it
// must validate, resolve to a real zone, fire, and record the resolved IANA
// name rather than the literal "local" - otherwise a later move could not be
// detected.
func TestTickLocalZoneResolvesAndFires(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}

	_, wantZone := systemZoneForTest()
	loc := time.Local
	if l, err := time.LoadLocation(wantZone); err == nil {
		loc = l
	}

	now := time.Date(2026, 6, 18, 7, 0, 30, 0, loc)
	res, err := Tick(scheduleJobIn("0 7 * * *", "none", config.LocalZone), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 {
		t.Fatalf("expected the local-zone job to fire, got fired=%v errors=%v", res.Fired, res.Errors)
	}

	got, _, err := store.GetScheduleFire(context.Background(), "job_a")
	if err != nil {
		t.Fatal(err)
	}
	st := decodeState(got)
	if st.Zone == config.LocalZone || st.Zone == "" {
		t.Fatalf("expected a resolved zone name, got %q", st.Zone)
	}
	if st.Occurrence != "2026-06-18T07:00" {
		t.Fatalf("Occurrence = %q, want the wall-clock key in the resolved zone", st.Occurrence)
	}
}

func systemZoneForTest() (*time.Location, string) {
	loc, name, err := config.LoadZone(config.LocalZone)
	if err != nil {
		return time.Local, time.Local.String()
	}
	return loc, name
}
