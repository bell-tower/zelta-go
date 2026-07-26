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

func TestParseListLinesAcceptsPreEncryptionFixtures(t *testing.T) {
	props := []string{"name", "guid", "written", "creation", "used", "type", "encryption", "ivsetguid", "receive_resume_token"}
	rows, err := ParseListLines([]string{"tank/a\tguid1\t12\t1\t1M\tfilesystem\t-"}, props)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[0].Props["receive_resume_token"]; got != "-" {
		t.Fatalf("token=%q", got)
	}
	if got := rows[0].Props["encryption"]; got != "-" {
		t.Fatalf("encryption=%q", got)
	}
}
