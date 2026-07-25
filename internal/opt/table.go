package opt

import (
	"strings"
	"sync"

	"git.belltower.it/djbell/zelta-go/data"
)

// Row is one opts.tsv entry (oracle zelta-args.awk table).
type Row struct {
	Verbs string   // "all" or comma verbs (oracle matches by substring)
	Flags []string // with dashes: "--verbose", "-v"
	Key   string   // bare env key (no ZELTA_ prefix)
	Alias string   // legacy env alias (KEY_ALIAS column)
	Type  string   // true false set arglist list incr decr invalid
	Value string   // fixed value for set ("" → flag takes an argument)
	Desc  string
	Warn  string
}

// AppliesTo mirrors the oracle's substring match on the VERBS column.
func (r Row) AppliesTo(verb string) bool {
	return r.Verbs == "all" || strings.Contains(r.Verbs, verb)
}

var (
	tableOnce sync.Once
	table     []Row
	tableErr  error
)

// Table parses the embedded data/opts.tsv once.
func Table() ([]Row, error) {
	tableOnce.Do(func() {
		b, err := data.ReadFile("opts.tsv")
		if err != nil {
			tableErr = err
			return
		}
		for _, line := range strings.Split(string(b), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Split(line, "\t")
			for len(f) < 8 {
				f = append(f, "")
			}
			v := f[5]
			if strings.TrimSpace(v) == "" {
				v = "" // whitespace-padding artifacts (e.g. -F row)
			}
			table = append(table, Row{
				Verbs: f[0],
				Flags: strings.Split(f[1], ","),
				Key:   f[2],
				Alias: f[3],
				Type:  f[4],
				Value: v,
				Desc:  f[6],
				Warn:  f[7],
			})
		}
	})
	return table, tableErr
}
