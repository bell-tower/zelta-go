package backup

import "testing"

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{1, "1B"},
		{624, "624B"},
		{1023, "1023B"},
		{1024, "1K"},
		{2496, "2K"},
		{51 * 1024, "51K"},
		{1024 * 1024, "1M"},
		{2*1024*1024*1024 + 512*1024*1024, "2G"},
		{3 * 1024 * 1024 * 1024 * 1024, "3T"},
		{-624, "-624B"},
	}
	for _, c := range cases {
		if got := HumanBytes(c.in); got != c.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
