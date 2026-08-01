package opt

import "git.belltower.it/djbell/zelta-go/data"

// Row is one opts.tsv entry (oracle zelta-args.awk table).
type Row = data.Row

// Table parses the embedded data/opts.tsv once.
func Table() ([]Row, error) {
	return data.Table()
}
