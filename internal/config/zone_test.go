package config

import (
	"testing"
	"time"
)

func TestLoadZone(t *testing.T) {
	tests := []struct {
		name     string
		tz       string
		wantName string
		wantErr  bool
	}{
		{"iana", "America/Chicago", "America/Chicago", false},
		{"utc", "UTC", "UTC", false},
		{"empty means utc, not machine local", "", "UTC", false},
		{"lowercase local", "local", "", false},
		{"capitalised local", "Local", "", false},
		{"shouty local", "LOCAL", "", false},
		{"bogus", "Mars/Olympus_Mons", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loc, name, err := LoadZone(tc.tz)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.tz)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadZone(%q) errored: %v", tc.tz, err)
			}
			if loc == nil {
				t.Fatal("expected a location")
			}
			if tc.wantName != "" && name != tc.wantName {
				t.Fatalf("name = %q, want %q", name, tc.wantName)
			}
		})
	}
}

// TestLoadZoneLocalNamesTheZone pins the property the move detection depends
// on: `local` must report a concrete zone name, never the literal "local" and
// never Go's "Local", or two different machine zones would compare equal and a
// move would go unnoticed.
func TestLoadZoneLocalNamesTheZone(t *testing.T) {
	_, name, err := LoadZone(LocalZone)
	if err != nil {
		t.Fatal(err)
	}
	if name == LocalZone {
		t.Fatal("local must resolve to a concrete zone name, not the literal value")
	}
	if name == "" {
		t.Fatal("expected a non-empty zone name")
	}
	if _, err := time.LoadLocation(name); err != nil && name != time.Local.String() {
		t.Fatalf("resolved name %q is not loadable", name)
	}
}

func TestZoneLabel(t *testing.T) {
	tests := map[string]string{
		"America/Chicago":  "Chicago",
		"America/New_York": "New_York",
		"Europe/Lisbon":    "Lisbon",
		"UTC":              "UTC",
		"":                 "UTC",
		"local":            "local",
		"Local":            "local",
	}
	for tz, want := range tests {
		if got := ZoneLabel(tz); got != want {
			t.Errorf("ZoneLabel(%q) = %q, want %q", tz, got, want)
		}
	}
}

func TestValidateAcceptsLocalAndRejectsBogusZones(t *testing.T) {
	base := func(tz string) File {
		return File{
			Version: CurrentVersion,
			Jobs: Jobs{
				"job_a": {
					Type:     "schedule",
					Command:  []string{"/bin/true"},
					Schedule: Schedule{Cron: "0 7 * * *", Timezone: tz},
				},
			},
		}
	}

	for _, tz := range []string{"local", "Local", "America/Chicago", ""} {
		if err := Validate(base(tz)); err != nil {
			t.Errorf("Validate with timezone %q should pass, got %v", tz, err)
		}
	}
	if err := Validate(base("Mars/Olympus_Mons")); err == nil {
		t.Error("Validate should reject an unknown timezone")
	}

	// defaults.timezone takes the same values.
	f := base("")
	f.Defaults.Timezone = "local"
	if err := Validate(f); err != nil {
		t.Errorf("defaults.timezone: local should pass, got %v", err)
	}
	f.Defaults.Timezone = "Mars/Olympus_Mons"
	if err := Validate(f); err == nil {
		t.Error("defaults.timezone should reject an unknown timezone")
	}
}
