package zfs

import (
	"context"
	"strings"
	"testing"
)

func TestParseGetLinesFeatures(t *testing.T) {
	lines := []string{
		"tank/src\twritten\t100",
		"tank/src\ttype\tfilesystem",
		"tank/src\tencryption\taes-256-gcm",
		"tank/src/child\twritten\t0",
		"tank/src/child\ttype\tfilesystem",
	}
	dc, err := ParseGetLines("tank/src", lines)
	if err != nil {
		t.Fatal(err)
	}
	if !dc.Features.Encryption || !dc.Features.IVSetGUID {
		t.Fatalf("features=%+v", dc.Features)
	}
	if got := dc.Prop("", "encryption"); got != "aes-256-gcm" {
		t.Fatalf("root encryption=%q", got)
	}
	if !dc.SourceEncrypted() {
		t.Fatal("want source encrypted")
	}
	if got := dc.Prop("/child", "written"); got != "0" {
		t.Fatalf("child written=%q", got)
	}
}

func TestParseGetLinesOldHostNoEncryption(t *testing.T) {
	lines := []string{
		"zroot\twritten\t0",
		"zroot\ttype\tfilesystem",
	}
	dc, err := ParseGetLines("zroot", lines)
	if err != nil {
		t.Fatal(err)
	}
	if dc.Features.Encryption {
		t.Fatal("old host should not report encryption feature")
	}
	if dc.SourceEncrypted() {
		t.Fatal("not encrypted")
	}
}

func TestLoadDatasetContextMissing(t *testing.T) {
	fake := &Fake{}
	dc, err := LoadDatasetContext(context.Background(), fake, "host:missing", "missing", 0)
	if err != nil {
		t.Fatal(err)
	}
	if dc.Exists {
		t.Fatal("want missing")
	}
}

func TestLoadDatasetContextFromProps(t *testing.T) {
	fake := &Fake{Props: map[string]string{
		"tank/src": "tank/src\tencryption\toff\ntank/src\twritten\t42\n",
	}}
	dc, err := LoadDatasetContext(context.Background(), fake, "tank/src", "tank/src", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !dc.Exists || !dc.Features.Encryption {
		t.Fatalf("dc=%+v", dc)
	}
	if dc.SourceEncrypted() {
		t.Fatal("off is not encrypted")
	}
	if got := dc.Prop("", "written"); got != "42" {
		t.Fatalf("written=%q", got)
	}
}

func TestLoadDatasetContextListOnly(t *testing.T) {
	fake := &Fake{Lists: map[string]string{"tank/src": "tank/src\tg1\n"}}
	dc, err := LoadDatasetContext(context.Background(), fake, "tank/src", "tank/src", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !dc.Exists {
		t.Fatal("want exists via list")
	}
	if dc.Features.Encryption {
		t.Fatal("list-only has no encryption feature")
	}
}

func TestAncestorProp(t *testing.T) {
	dc := &DatasetContext{BySuffix: map[string]map[string]string{
		"":       {"encryption": "aes-256-gcm"},
		"/child": {"written": "1"},
	}}
	if got := dc.AncestorProp("/child/missing", "encryption"); got != "aes-256-gcm" {
		t.Fatalf("got %q", got)
	}
}

func TestFakeGetPropsBody(t *testing.T) {
	body := strings.Join([]string{
		"tank/a\twritten\t1",
		"tank/a\ttype\tfilesystem",
	}, "\n")
	f := &Fake{Props: map[string]string{"tank/a": body}}
	lines, err := f.GetProps(context.Background(), "tank/a", "tank/a", "all", 0)
	if err != nil || len(lines) != 2 {
		t.Fatalf("lines=%v err=%v", lines, err)
	}
}
