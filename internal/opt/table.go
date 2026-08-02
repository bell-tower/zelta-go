package opt

import "github.com/bell-tower/zelta-go/data"

// Row is one opts.tsv entry (oracle zelta-args.awk table).
type Row = data.Row

// Table parses the embedded data/opts.tsv once.
func Table() ([]Row, error) {
	return data.Table()
}
