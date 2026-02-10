package core

import "testing"

func TestBuildJobKey(t *testing.T) {
	if got := BuildJobKey("ns", "job"); got != "ns:job" {
		t.Fatalf("got %q", got)
	}
	if got := BuildJobKey("", "job"); got != "job" {
		t.Fatalf("got %q", got)
	}
}

func TestSplitJobKey(t *testing.T) {
	ns, job := SplitJobKey("ns:job")
	if ns != "ns" || job != "job" {
		t.Fatalf("got ns=%q job=%q", ns, job)
	}
	ns, job = SplitJobKey("job")
	if ns != "" || job != "job" {
		t.Fatalf("got ns=%q job=%q", ns, job)
	}
}

func TestSplitJobSelector(t *testing.T) {
	p, j := SplitJobSelector("profile:job")
	if p != "profile" || j != "job" {
		t.Fatalf("got p=%q j=%q", p, j)
	}
	p, j = SplitJobSelector("job")
	if p != "" || j != "job" {
		t.Fatalf("got p=%q j=%q", p, j)
	}
	p, j = SplitJobSelector("")
	if p != "" || j != "" {
		t.Fatalf("got p=%q j=%q", p, j)
	}
	// Empty parts should not split
	p, j = SplitJobSelector(":job")
	if p != "" || j != ":job" {
		t.Fatalf("got p=%q j=%q", p, j)
	}
	p, j = SplitJobSelector("profile:")
	if p != "" || j != "profile:" {
		t.Fatalf("got p=%q j=%q", p, j)
	}
}
