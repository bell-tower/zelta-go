package zfs

import (
	"fmt"
	"strings"
)

// ListRow is one zfs list -Hpr line with named properties.
type ListRow struct {
	Name  string
	Props map[string]string
}

// ParseListLines parses tab-separated zfs list -H output.
// props[0] must be "name" or the first column is treated as name.
func ParseListLines(lines []string, props []string) ([]ListRow, error) {
	if len(props) == 0 {
		return nil, fmt.Errorf("no properties")
	}
	out := make([]ListRow, 0, len(lines))
	for i, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) < len(props) {
			// Older callers and fixtures may omit the optional resume-token column.
			token := -1
			for j, prop := range props {
				if prop == "receive_resume_token" {
					token = j
					break
				}
			}
			if token < 0 || len(fields) != len(props)-1 {
				return nil, fmt.Errorf("line %d: got %d fields, want %d", i+1, len(fields), len(props))
			}
			fields = append(fields, "")
			copy(fields[token+1:], fields[token:])
			fields[token] = ""
		}
		row := ListRow{Props: make(map[string]string, len(props))}
		for j, p := range props {
			row.Props[p] = fields[j]
			if p == "name" || j == 0 {
				row.Name = fields[j]
			}
		}
		if row.Name == "" {
			row.Name = fields[0]
		}
		out = append(out, row)
	}
	return out, nil
}
