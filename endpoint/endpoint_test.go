package endpoint

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		user    string
		host    string
		ds      string
		snap    string
		remote  bool
		wantErr bool
	}{
		{"tank/data", "", "", "tank/data", "", false, false},
		{"tank/data@snap1", "", "", "tank/data", "snap1", false, false},
		{"host:tank/data", "", "host", "tank/data", "", true, false},
		{"u@host:tank/data", "u", "host", "tank/data", "", true, false},
		{"u@host:tank/data@s", "u", "host", "tank/data", "s", true, false},
		{"u@[2001:db8::1]:tank/x", "u", "2001:db8::1", "tank/x", "", true, false},
		{"", "", "", "", "", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Parse(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.User != tc.user || got.Host != tc.host || got.Dataset != tc.ds || got.Snapshot != tc.snap || got.Remote != tc.remote {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestDSSuffix(t *testing.T) {
	s, err := DSSuffix("tank/data", "tank/data/local")
	if err != nil || s != "/local" {
		t.Fatalf("got %q %v", s, err)
	}
	s, err = DSSuffix("tank/data", "tank/data")
	if err != nil || s != "" {
		t.Fatalf("got %q %v", s, err)
	}
}
