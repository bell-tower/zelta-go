package match

import (
	"strings"
	"testing"
)

func TestGlobToRegexpDSCascade(t *testing.T) {
	re, err := globToRegexp("/minus", "(/.*)?")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"/minus", "/minus/two", "/minus/two/one"} {
		if !re.MatchString(s) {
			t.Errorf("expected match %q", s)
		}
	}
	if re.MatchString("/vol1") {
		t.Error("should not match /vol1")
	}
}

func TestGlobLift(t *testing.T) {
	re, err := globToRegexp("*/lift*", "(/.*)?")
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("/minus/two/one/0/lift off") {
		t.Fatal("lift off")
	}
	if re.MatchString("/minus") {
		t.Fatal("minus")
	}
}

func TestParseFilterExcludeVol1(t *testing.T) {
	f := ParseFilter(nil, []string{"/vol1"})
	if !f.keepDataset("apool/treetop", "") {
		t.Fatal("root")
	}
	if f.keepDataset("apool/treetop/vol1", "/vol1") {
		t.Fatal("vol1 should drop")
	}
	if !f.keepDataset("apool/treetop/minus", "/minus") {
		t.Fatal("minus keep")
	}
}

func TestParseFilterExcludeExactSourceOnly(t *testing.T) {
	f := ParseFilter(nil, []string{"apool/treetop/vol1"})
	if f.keepDataset("apool/treetop/vol1", "/vol1") {
		t.Fatal("src vol1")
	}
	if !f.keepDataset("bpool/bleetop/vol1", "/vol1") {
		t.Fatal("tgt vol1 stays (different full name)")
	}
}

func TestParseFilterIncludeMinus(t *testing.T) {
	f := ParseFilter([]string{"/minus"}, nil)
	if f.keepDataset("apool/treetop", "") {
		t.Fatal("root out")
	}
	if !f.keepDataset("apool/treetop/minus", "/minus") {
		t.Fatal("minus in")
	}
	if !f.keepDataset("apool/treetop/minus/two", "/minus/two") {
		t.Fatal("child in")
	}
	if f.keepDataset("apool/treetop/vol1", "/vol1") {
		t.Fatal("vol1 out")
	}
}

func TestSnapExclude(t *testing.T) {
	f := ParseFilter(nil, []string{"@zelta_2026*"})
	if f.keepSourceSnap("@zelta_2026-01-04_04.09.17", "apool/treetop", "") {
		t.Fatal("excluded snap")
	}
	if !f.keepSourceSnap("@zelta_2025-12-22_16.25.11", "apool/treetop", "") {
		t.Fatal("kept snap")
	}
}

func TestSnapIncludeOnly(t *testing.T) {
	f := ParseFilter([]string{"@zelta_2025-12-22*"}, nil)
	if !f.keepDataset("apool/treetop/vol1", "/vol1") {
		t.Fatal("all DS kept")
	}
	if !f.keepSourceSnap("@zelta_2025-12-22_16.25.11", "apool/treetop", "") {
		t.Fatal("included snap")
	}
	if f.keepSourceSnap("@zelta_2026-01-04_04.09.17", "apool/treetop", "") {
		t.Fatal("other snap out")
	}
}

func TestInvalidPatternWarning(t *testing.T) {
	f := ParseFilter(nil, []string{"foo*bar"})
	if len(f.Warnings) == 0 {
		t.Fatal("want warning")
	}
	if !strings.Contains(f.Warnings[0], "must start with '@' or include '/'") {
		t.Fatalf("warning=%q", f.Warnings[0])
	}
}
