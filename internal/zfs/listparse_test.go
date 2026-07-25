package zfs

import "testing"

func TestParseListLines(t *testing.T) {
	lines := []string{
		"tank/a\tguid1\t12",
		"tank/a@s1\tguid2\t0",
	}
	rows, err := ParseListLines(lines, []string{"name", "guid", "written"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "tank/a" || rows[0].Props["guid"] != "guid1" {
		t.Fatalf("%+v", rows)
	}
}
