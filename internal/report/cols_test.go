package report

import "testing"

func TestExpandProplistDefault(t *testing.T) {
	got, err := ExpandProplist("")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ds_suffix", "match", "src_last", "tgt_last", "info"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestExpandProplistSynonyms(t *testing.T) {
	got, err := ExpandProplist("dssuffix,last,info")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ds_suffix", "src_last", "tgt_last", "info"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestExpandProplistUnknown(t *testing.T) {
	_, err := ExpandProplist("nope")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		n    int64
		p    bool
		want string
	}{
		{0, false, "0B"},
		{0, true, "0"},
		{512, false, "512B"},
		{1024, false, "1K"},
		{1536, false, "1.5K"},
		{1048576, true, "1048576"},
	}
	for _, tc := range cases {
		if got := FormatBytes(tc.n, tc.p); got != tc.want {
			t.Errorf("FormatBytes(%d,%v)=%q want %q", tc.n, tc.p, got, tc.want)
		}
	}
}
