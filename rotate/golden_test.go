package rotate

import (
	"git.belltower.it/djbell/zelta-go/backup"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"git.belltower.it/djbell/zelta-go/match"
	"git.belltower.it/djbell/zelta-go/zfs"
)

func TestPlanGoldens(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "golden", "rotate")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("no rotate golden dir")
		}
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Run(entry.Name(), func(t *testing.T) { runPlanGolden(t, filepath.Join(root, entry.Name())) })
		}
	}
}

func runPlanGolden(t *testing.T, dir string) {
	t.Helper()
	meta := readGoldenMeta(t, filepath.Join(dir, "meta.yaml"))
	rows, err := readPairs(filepath.Join(dir, "pairs.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	targetRows, err := readTargetRows(filepath.Join(dir, "target.list"))
	if err != nil {
		t.Fatal(err)
	}
	steps, err := PlanTree(TreeRequest{
		Source: meta["source"], Target: meta["target"], Pairs: rows,
		TargetRows: targetRows, Intermediate: meta["intermediate"] != "false", Flags: backup.DefaultSendRecv(),
	})
	got := ""
	if err == nil {
		got = Format(steps)
	} else {
		got = "ERROR: " + err.Error() + "\n"
	}
	want, err := os.ReadFile(filepath.Join(dir, "expected.out"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("plan mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func readPairs(path string) ([]*match.Pair, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []*match.Pair
	for lineNo, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 8 {
			return nil, &goldenLineError{lineNo + 1, "pairs.tsv requires 8 columns"}
		}
		out = append(out, &match.Pair{DSSuffix: f[0], SrcName: f[1], TgtName: f[2], Match: f[3], SrcLast: f[4], TgtLast: f[5], SrcOrigin: f[6], SrcType: f[7]})
	}
	return out, nil
}

func readTargetRows(path string) ([]zfs.ListRow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []zfs.ListRow
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, zfs.ListRow{Name: strings.TrimSpace(line)})
		}
	}
	return out, nil
}

func readGoldenMeta(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return out
}

type goldenLineError struct {
	line int
	msg  string
}

func (e *goldenLineError) Error() string { return "line " + strconv.Itoa(e.line) + ": " + e.msg }
