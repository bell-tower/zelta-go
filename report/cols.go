package report

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"sync"

	"git.belltower.it/djbell/zelta-go/data"
)

// DefaultProplist is the default -o list before synonym expansion.
const DefaultProplist = "dssuffix,match,last,info"

// Col describes one match/report column from cols.tsv.
type Col struct {
	Name        string
	Synonyms    []string
	Type        string // ds, snap, text, num, bytes, ds_snap
	Description string
}

var (
	colsOnce sync.Once
	colsErr  error
	colByKey map[string][]string // synonym or name → canonical names in tsv order
	colMeta  map[string]Col
	colOrder []string
)

func loadCols() {
	colsOnce.Do(func() {
		colByKey = make(map[string][]string)
		colMeta = make(map[string]Col)
		b, err := data.ReadFile("cols.tsv")
		if err != nil {
			colsErr = err
			return
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) < 3 {
				continue
			}
			name := strings.TrimSpace(fields[0])
			if name == "" || strings.HasPrefix(name, "#") {
				continue
			}
			syns := splitCSV(fields[1])
			typ := strings.TrimSpace(fields[2])
			desc := ""
			if len(fields) > 3 {
				desc = strings.TrimSpace(fields[3])
			}
			c := Col{Name: name, Synonyms: syns, Type: typ, Description: desc}
			colMeta[name] = c
			colOrder = append(colOrder, name)
			addKey(name, name)
			for _, s := range syns {
				addKey(s, name)
			}
		}
		colsErr = sc.Err()
	})
}

func addKey(key, name string) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return
	}
	for _, existing := range colByKey[key] {
		if existing == name {
			return
		}
	}
	colByKey[key] = append(colByKey[key], name)
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ExpandProplist expands a comma-separated -o list (synonyms → canonical cols).
// Empty input uses DefaultProplist.
func ExpandProplist(proplist string) ([]string, error) {
	loadCols()
	if colsErr != nil {
		return nil, colsErr
	}
	if strings.TrimSpace(proplist) == "" {
		proplist = DefaultProplist
	}
	var out []string
	seen := make(map[string]bool)
	for _, tok := range splitCSV(proplist) {
		key := strings.ToLower(tok)
		names, ok := colByKey[key]
		if !ok {
			return nil, fmt.Errorf("unknown column %q", tok)
		}
		for _, n := range names {
			if seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// ColType returns the cols.tsv type for a canonical column name.
func ColType(name string) string {
	loadCols()
	if c, ok := colMeta[name]; ok {
		return c.Type
	}
	return ""
}
