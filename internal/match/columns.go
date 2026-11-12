package match

import (
	"slices"
	"strings"
)

// Column describes a "zelta match" output column.
type Column struct {
	// Name is the column key, e.g. "src_last"; headers print it uppercased.
	Name string
	// Synonyms are the names accepted by -o (underscores are ignored).
	Synonyms []string
	// Desc describes the column.
	Desc string
	kind colKind
}

type colKind uint8

const (
	kindText colKind = iota
	kindNum
	kindBytes
	kindSuffix
)

// columns is the port of zelta-cols.tsv, in display order. Deprecated
// columns (sync_code, action) are not offered.
var columns = []Column{
	{"ds_suffix", []string{"relname", "dssuffix"}, "Relative dataset name", kindSuffix},
	{"match", []string{"match"}, "Latest matching snapshot", kindText},
	{"match_ivset", []string{"matchivset"}, "IV set GUID for the selected match", kindText},
	{"num_matches", []string{"nummatches", "matches", "count"}, "Total number of matches", kindNum},
	{"xfer_size", []string{"xfersize", "size"}, "Unsynced data size based on snapshots", kindBytes},
	{"xfer_num", []string{"xfernum", "count", "num"}, "Unsynced snapshot count", kindNum},
	{"src_name", []string{"srcname", "name"}, "Full source dataset name", kindText},
	{"src_first", []string{"srcfirst", "first"}, "First source snapshot", kindText},
	{"src_next", []string{"srcnext", "next"}, "Next source snapshot after match", kindText},
	{"src_last", []string{"srclast", "last"}, "Latest source snapshot", kindText},
	{"src_written", []string{"srcwritten", "written"}, "Source data written since last snap", kindBytes},
	{"src_snaps", []string{"srcsnaps", "snaps"}, "Total source snapshots", kindNum},
	{"tgt_name", []string{"tgtname", "name"}, "Full target dataset name", kindText},
	{"tgt_first", []string{"tgtfirst", "first"}, "First target snapshot", kindText},
	{"tgt_next", []string{"tgtnext", "next"}, "Next target snapshot that is blocking sync", kindText},
	{"tgt_last", []string{"tgtlast", "last"}, "Latest target snapshot", kindText},
	{"tgt_written", []string{"tgtwritten", "written"}, "Target data written since last snap", kindBytes},
	{"tgt_snaps", []string{"tgtsnaps", "snaps"}, "Total target snapshots", kindNum},
	{"info", []string{"info", "status"}, "Sync state description", kindText},
}

// DefaultColumns is the column list used when -o is not given.
const DefaultColumns = "dssuffix,match,last,info"

// Columns lists the "zelta match" columns.
func Columns() []Column {
	out := make([]Column, len(columns))
	copy(out, columns)
	return out
}

// ColumnNames returns every name -o accepts (column synonyms and "all"),
// for shell completion.
func ColumnNames() []string {
	seen := map[string]bool{}
	names := []string{"all"}
	for _, c := range columns {
		for _, s := range append([]string{c.Name}, c.Synonyms...) {
			if !seen[s] {
				seen[s] = true
				names = append(names, s)
			}
		}
	}
	return names
}

// parseColumns expands a property list into columns. Underscores in the
// list are ignored, "all" selects every column, and a synonym such as "last"
// can expand to several columns. It returns the invalid property, if any.
func parseColumns(list string) ([]*Column, string) {
	if list == "" {
		list = DefaultColumns
	}
	if list == "all" {
		names := make([]string, len(columns))
		for i, c := range columns {
			names[i] = c.Name
		}
		list = strings.Join(names, ",")
	}
	list = strings.ReplaceAll(list, "_", "")
	var out []*Column
	for tok := range strings.SplitSeq(list, ",") {
		n := len(out)
		for i := range columns {
			if slices.Contains(columns[i].Synonyms, tok) {
				out = append(out, &columns[i])
			}
		}
		if len(out) == n {
			return nil, tok
		}
	}
	return out, ""
}
