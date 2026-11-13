package progress

import (
	"strings"
	"testing"
	"time"
)

func TestBoard(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	b := &Board{Now: func() time.Time { return now }}
	full := b.Start("apool/a@s")
	incr := b.Start("apool/b@x%z")
	idle := b.Start("apool/c@s")
	b.Size(full, 4<<30)
	b.Sent(full, "apool/a@s", 1<<30)
	b.Size(incr, 3<<20)
	b.Sent(incr, "apool/b@y", 1<<20)
	b.Sent(incr, "apool/b@z", 512<<10) // -I restarts the count per snapshot
	b.Done(idle)
	b.Start("apool/d@s")
	now = now.Add(42 * time.Second)

	var out strings.Builder
	b.Write(&out)
	want := "apool/a@s: 1G of 4G sent (25%), 42s elapsed\n" +
		"apool/b@x%z: 1M of 3M sent (50%), 42s elapsed\n" +
		"apool/d@s: starting, 42s elapsed\n"
	if out.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestNilBoard(t *testing.T) {
	var b *Board
	id := b.Start("x")
	b.Size(id, 1)
	b.Sent(id, "x", 1)
	b.Done(id)
	b.Write(nil)
}

func TestEnabled(t *testing.T) {
	for _, tt := range []struct {
		tty  bool
		opt  string
		want bool
	}{
		{true, "", true}, {false, "", false}, {true, "AUTO", true}, {false, "auto", false},
		{true, "0", false}, {true, "off", false}, {false, "1", true}, {false, "yes", true},
	} {
		if got := (&Board{TTY: tt.tty}).Enabled(tt.opt); got != tt.want {
			t.Errorf("TTY=%v SEND_PROGRESS=%q: got %v", tt.tty, tt.opt, got)
		}
	}
	if (*Board)(nil).Enabled("1") {
		t.Error("nil board enabled")
	}
}
