package prune

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/zfs"
)

func TestGolden(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "golden", "prune")
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
				runPruneGolden(t, filepath.Join(root, entry.Name()))
			})
		}
	}
}

func runPruneGolden(t *testing.T, dir string) {
	t.Helper()
	meta := readMeta(t, filepath.Join(dir, "meta.yaml"))
	source := meta["source"]
	if source == "" {
		t.Fatal("meta.yaml needs source")
	}
	src, err := os.ReadFile(filepath.Join(dir, "src.list"))
	if err != nil {
		t.Fatal(err)
	}
	tgt, _ := os.ReadFile(filepath.Join(dir, "tgt.list"))
	now, _ := strconv.ParseInt(meta["now"], 10, 64)
	pruneNum := -1
	if meta["prune-num"] != "" {
		pruneNum, err = strconv.Atoi(meta["prune-num"])
		if err != nil {
			t.Fatal(err)
		}
	}
	var pruneGuard PruneGuard
	if meta["prune-guard"] != "" {
		var gerr error
		pruneGuard, gerr = ParsePruneGuard(meta["prune-guard"])
		if gerr != nil {
			t.Fatal(gerr)
		}
	}
	pruneTime, terr := ParsePruneTime(meta["prune-time"])
	if terr != nil {
		t.Fatal(terr)
	}
	srcEp := endpoint.MustParse(source)
	var guardEp endpoint.Endpoint
	if meta["guard-target"] != "" {
		guardEp = endpoint.MustParse(meta["guard-target"])
	}
	res, runErr := Run(context.Background(), &zfs.Fake{Lists: map[string]string{
		source:               string(src),
		meta["guard-target"]: string(tgt),
	}}, Request{
		Source: srcEp, GuardTarget: guardEp, PruneGuard: pruneGuard,
		PruneNum: pruneNum, PruneTime: pruneTime, PruneGrid: meta["prune-grid"],
		NoRanges: meta["no-ranges"] == "true", Visual: meta["visual"] == "true", Now: now,
	})
	gotOut, gotErr, gotExit := "", "", 0
	if runErr != nil {
		gotErr, gotExit = runErr.Error()+"\n", 1
	} else if res != nil {
		gotOut = res.Output
	}
	wantOut := readOptional(t, filepath.Join(dir, "expected.out"))
	wantErr := readOptional(t, filepath.Join(dir, "expected.err"))
	wantExit := 0
	if b, err := os.ReadFile(filepath.Join(dir, "expected.exit")); err == nil {
		wantExit, _ = strconv.Atoi(strings.TrimSpace(string(b)))
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
		key, value, ok := strings.Cut(line, ":")
		if ok {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
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
