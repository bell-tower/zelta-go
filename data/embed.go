package data

import "embed"

//go:embed *.tsv
var FS embed.FS

func ReadFile(name string) ([]byte, error) {
	return FS.ReadFile(name)
}
