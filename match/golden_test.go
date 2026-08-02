package match

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/internal/report"
	"github.com/bell-tower/zelta-go/zfs"
)

func TestGolden(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "golden", "match")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("no golden dir")
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			runGoldenCase(t, filepath.Join(root, name))
		})
	}
}

func runGoldenCase(t *testing.T, dir string) {
	t.Helper()
	srcList, err := os.ReadFile(filepath.Join(dir, "src.list"))
	if err != nil {
		t.Fatal(err)
	}
	tgtList, err := os.ReadFile(filepath.Join(dir, "tgt.list"))
	if err != nil {
		t.Fatal(err)
	}
	meta := readMeta(t, filepath.Join(dir, "meta.yaml"))
	src := meta["source"]
	tgt := meta["target"]
	if src == "" || tgt == "" {
		t.Fatal("meta.yaml needs source and target")
	}
	scripting := meta["scripting"] == "true" || meta["scripting"] == "yes" || meta["scripting"] == "1"
	parsable := meta["parsable"] == "true" || meta["parsable"] == "yes" || meta["parsable"] == "1"
	depth := 0
	if d := meta["depth"]; d != "" {
		depth, _ = strconv.Atoi(d)
	}
	var cols []string
	if pl := meta["cols"]; pl != "" {
		var err error
		cols, err = report.ExpandProplist(pl)
		if err != nil {
			t.Fatal(err)
		}
	}
	var include, exclude []string
	if v := meta["include"]; v != "" {
		include = []string{v}
	}
	if v := meta["exclude"]; v != "" {
		exclude = []string{v}
	}
	noWritten := meta["nowritten"] == "true" || meta["nowritten"] == "yes" || meta["nowritten"] == "1" ||
		meta["no-written"] == "true" || meta["no-written"] == "yes" || meta["no-written"] == "1"
	checkTime := meta["time"] == "true" || meta["time"] == "yes" || meta["time"] == "1"

	fake := &zfs.Fake{Lists: map[string]string{
		src: string(srcList),
		tgt: string(tgtList),
	}}
	res, err := Compare(context.Background(), fake, Request{
		Source:    endpoint.MustParse(src),
		Target:    endpoint.MustParse(tgt),
		Cols:      cols,
		Depth:     depth,
		Include:   include,
		Exclude:   exclude,
		Scripting: scripting,
		Parsable:  parsable,
		NoWritten: noWritten,
		CheckTime: checkTime,
	})
	gotErr := ""
	gotExit := 0
	if err != nil {
		gotErr = err.Error() + "\n"
		gotExit = 1
	}

	wantOut := readOptional(t, filepath.Join(dir, "expected.out"))
	wantErr := readOptional(t, filepath.Join(dir, "expected.err"))
	wantExit := 0
	if b, err := os.ReadFile(filepath.Join(dir, "expected.exit")); err == nil {
		wantExit, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}

	gotOut := ""
	if res != nil {
		gotOut = res.Output
	}
	if gotOut != wantOut {
		t.Errorf("stdout mismatch\ngot:\n%s\nwant:\n%s", gotOut, wantOut)
	}
	if gotErr != wantErr {
		t.Errorf("stderr mismatch\ngot:\n%s\nwant:\n%s", gotErr, wantErr)
	}
	if gotExit != wantExit {
		t.Errorf("exit=%d want=%d", gotExit, wantExit)
	}
}

func readMeta(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func readOptional(t *testing.T, path string) string {
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
