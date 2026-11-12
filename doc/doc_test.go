package doc

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
)

func TestTopics(t *testing.T) {
	tests := map[string][2]string{
		"":         {"zelta", "8"},
		"options":  {"zelta-options", "7"},
		"zprune":   {"zprune", "8"},
		"zmatch":   {"zelta-match", "8"},
		"zpull":    {"zelta-backup", "8"},
		"zpush":    {"zelta-backup", "8"},
		"backup":   {"zelta-backup", "8"},
		"failover": {"zelta-failover", "8"},
	}
	for topic, want := range tests {
		name, sec := Topic(topic)
		if name != want[0] || sec != want[1] {
			t.Errorf("Topic(%q) = %s(%s)", topic, name, sec)
		}
		p, ok := Lookup(name, sec)
		if !ok || !strings.Contains(string(p.Roff), ".TH") {
			t.Errorf("Lookup(%s, %s) missing", name, sec)
		}
		if len(p.Markdown) == 0 {
			t.Errorf("no markdown for %s", name)
		}
	}
	if _, ok := Lookup("nope", "8"); ok {
		t.Errorf("unexpected page")
	}
	topics := Topics()
	for _, want := range []string{"backup", "match", "options", "zprune", "policy", "prune"} {
		if !slices.Contains(topics, want) {
			t.Errorf("Topics missing %q: %v", want, topics)
		}
	}
	if slices.Contains(topics, "zelta") {
		t.Errorf("zelta itself is not a topic")
	}
	if entries, _ := fs.ReadDir(FS(), "man8"); len(entries) < 10 {
		t.Errorf("embedded man8 = %d pages", len(entries))
	}
}
