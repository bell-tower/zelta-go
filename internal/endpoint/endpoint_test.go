package endpoint

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Endpoint
	}{
		{"pool", Endpoint{ID: "pool", User: "me", Host: "here", DS: "pool", Pool: "pool", Leaf: "pool", Depth: 1}},
		{"pool/a/b@snap", Endpoint{ID: "pool/a/b@snap", User: "me", Host: "here", DS: "pool/a/b", Snap: "@snap", Pool: "pool", Leaf: "b", Depth: 3}},
		{"host:pool/ds", Endpoint{ID: "host:pool/ds", Remote: "host", User: "me", Host: "host", DS: "pool/ds", Pool: "pool", Leaf: "ds", Depth: 2}},
		{"bk@host.example:tank/Backups/x@s1", Endpoint{ID: "bk@host.example:tank/Backups/x@s1", Remote: "bk@host.example", User: "bk", Host: "host.example", DS: "tank/Backups/x", Snap: "@s1", Pool: "tank", Leaf: "x", Depth: 3}},
		{"user@[2001:db8::1]:pool/ds", Endpoint{ID: "user@[2001:db8::1]:pool/ds", Remote: "user@2001:db8::1", User: "user", Host: "2001:db8::1", DS: "pool/ds", Pool: "pool", Leaf: "ds", Depth: 2}},
		{"[::1]:pool", Endpoint{ID: "[::1]:pool", Remote: "::1", User: "me", Host: "::1", DS: "pool", Pool: "pool", Leaf: "pool", Depth: 1}},
		{"localhost:pool/odd:name", Endpoint{ID: "localhost:pool/odd:name", User: "me", Host: "here", DS: "pool/odd:name", Pool: "pool", Leaf: "odd:name", Depth: 2}},
		{"root@localhost:pool", Endpoint{ID: "root@localhost:pool", Remote: "root@localhost", User: "root", Host: "here", DS: "pool", Pool: "pool", Leaf: "pool", Depth: 1}},
		{"pool/with space/x", Endpoint{ID: "pool/with space/x", User: "me", Host: "here", DS: "pool/with space/x", Pool: "pool", Leaf: "x", Depth: 3}},
		{"pool/ds@a@b", Endpoint{ID: "pool/ds@a@b", User: "me", Host: "here", DS: "pool/ds", Pool: "pool", Leaf: "ds", Depth: 2}},
		{"host:", Endpoint{ID: "host:", Remote: "host", User: "me", Host: "host", DS: "", Pool: "", Leaf: "", Depth: 1}},
		{"", Endpoint{}},
	}
	for _, tt := range tests {
		got := Parse(tt.in, "here", "me")
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Parse(%q) =\n %#v\nwant\n %#v", tt.in, got, tt.want)
		}
	}
}

func TestHelpers(t *testing.T) {
	l := Parse("pool/ds", "h", "u")
	r := Parse("u@h2:pool/ds", "h", "u")
	if l.IsRemote() || !r.IsRemote() || !l.Valid() || Parse("h:", "h", "u").Valid() {
		t.Errorf("IsRemote/Valid mismatch")
	}
	if l.RemoteFor("ssh -n") != nil {
		t.Errorf("local endpoint has a remote")
	}
	rem := r.RemoteFor("ssh -n -p 22")
	if rem == nil || rem.Host != "u@h2" || !reflect.DeepEqual(rem.Prefix, []string{"ssh", "-n", "-p", "22"}) {
		t.Errorf("RemoteFor = %#v", rem)
	}
	if l.SameHost(r) || !r.SameHost(Parse("u@h2:other", "h", "u")) {
		t.Errorf("SameHost mismatch")
	}
}
