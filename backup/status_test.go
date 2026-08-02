package backup

import "testing"

func TestErrCodeFromOutput(t *testing.T) {
	cases := []struct {
		out  string
		want ErrCode
	}{
		{"1 dataset up-to-date", ErrCodeUpToDate},
		{"NO SOURCE: tank/missing", ErrCodeNoSource},
		{"target has local writes on tank/x", ErrCodeTargetLocalWrites},
		{"no common snapshot (diverged)", ErrCodeNoCommonSnapshot},
		{"no snapshot; target diverged", ErrCodeDiverged},
		{"target has diverged from source", ErrCodeDiverged},
		{"no source snapshot on tank/x", ErrCodeNoSourceSnapshot},
		{"source_snapshot_creation_failed", ErrCodeSourceSnapshot},
		{"sent 2 streams ok", ErrCodeNone},
		{"", ErrCodeNone},
	}
	for _, tc := range cases {
		if got := ErrCodeFromOutput(tc.out); got != tc.want {
			t.Errorf("ErrCodeFromOutput(%q)=%q want %q", tc.out, got, tc.want)
		}
	}
}

func TestErrCodeBlocked(t *testing.T) {
	if ErrCodeNone.Blocked() || ErrCodeUpToDate.Blocked() {
		t.Fatal("none/up-to-date must not be blocked")
	}
	if !ErrCodeDiverged.Blocked() || !ErrCodeNoSource.Blocked() {
		t.Fatal("failure codes must be blocked")
	}
}

func TestErrCodeFromPlan(t *testing.T) {
	if got := ErrCodeFromPlan(&Plan{Skip: 1}); got != ErrCodeUpToDate {
		t.Fatalf("skip-only: %q", got)
	}
	if got := ErrCodeFromPlan(&Plan{Steps: []*Step{{
		Kind: KindBlocked, Notice: "blocked sync: target has local writes",
	}}}); got != ErrCodeTargetLocalWrites {
		t.Fatalf("blocked local writes: %q", got)
	}
	if got := ErrCodeFromPlan(&Plan{Full: 1}); got != ErrCodeNone {
		t.Fatalf("work planned: %q", got)
	}
}
