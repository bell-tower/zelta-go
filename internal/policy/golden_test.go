package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGolden(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "golden", "policy")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("no golden dir")
		}
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Run(entry.Name(), func(t *testing.T) {
				runGolden(t, filepath.Join(root, entry.Name()))
			})
		}
	}
}

type goldCase struct {
	name     string
	operands []string
	noHeader bool
	goldFile string
}

func runGolden(t *testing.T, dir string) {
	t.Helper()
	config := filepath.Join(dir, "config", "zelta.yaml")
	if _, err := os.Stat(config); os.IsNotExist(err) {
		t.Skipf("no config in %s", dir)
	}

	cases := []goldCase{
		{name: "table-header-all", goldFile: "table-header-all.out"},
		{name: "table-nh-all", operands: nil, noHeader: true, goldFile: "table-nh-all.out"},
		{name: "table-nh-AWS0", operands: []string{"AWS0"}, noHeader: true, goldFile: "table-nh-AWS0.out"},
		{name: "table-nh-AWS0-CLIENT0", operands: []string{"AWS0", "CLIENT0"}, noHeader: true, goldFile: "table-nh-AWS0-CLIENT0.out"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			jobs, _, err := Load(config, nil)
			if err != nil {
				t.Fatal(err)
			}
			jobs = Filter(jobs, c.operands)
			got := FormatTable(jobs, c.noHeader)
			want := readOptionalGold(t, filepath.Join(dir, c.goldFile))
			if want == "" && got != "" {
				t.Fatalf("no golden file %s, but got output:\n%s", c.goldFile, got)
			}
			if got != want {
				t.Errorf("mismatch\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func readOptionalGold(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(b)
}
