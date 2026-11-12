package cache

import (
	"testing"
	"time"
)

func TestStore(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := &Store{Dir: t.TempDir(), TTL: time.Hour, Now: func() time.Time { return now }}
	type entry struct{ A string }
	if err := s.Save("x/"+Key("a", "b"), entry{"hi"}); err != nil {
		t.Fatal(err)
	}
	var got entry
	if !s.Fresh("x/"+Key("a", "b"), &got) || got.A != "hi" {
		t.Fatalf("fresh load: %+v", got)
	}
	now = now.Add(2 * time.Hour)
	if s.Fresh("x/"+Key("a", "b"), &got) {
		t.Error("expired entry loaded as fresh")
	}
	if _, ok := s.Load("x/"+Key("a", "b"), &got); !ok {
		t.Error("Load must ignore TTL")
	}
	s.Delete("x/" + Key("a", "b"))
	if _, ok := s.Load("x/"+Key("a", "b"), &got); ok {
		t.Error("deleted entry loaded")
	}
	var nilStore *Store
	if nilStore.Fresh("x", &got) || nilStore.Save("x", got) != nil {
		t.Error("nil store must be a disabled cache")
	}
	if Key("a", "b") == Key("ab") {
		t.Error("key parts must be separated")
	}
}
