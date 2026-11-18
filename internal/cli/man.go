package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"zelta.space/doc"
)

// systemMan shows a manual page: from ZELTA_DOC when set, else the installed
// system manual, else the copy embedded in the binary.
func systemMan(ctx context.Context, a *App, topic string) int {
	name, section := doc.Topic(topic)
	page, embedded := doc.Lookup(name, section)
	run := func(args ...string) int {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = a.Stdin, a.Stdout, a.Stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return ee.ExitCode()
			}
			return 1
		}
		return 0
	}
	if dir := a.NewLoader().Paths().Doc; dir != "" {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return run("man", "-M", dir, section, name)
		}
	}
	if _, err := exec.LookPath("man"); err == nil {
		if exec.CommandContext(ctx, "man", "-w", section, name).Run() == nil {
			return run("man", section, name)
		}
	}
	if !embedded {
		fmt.Fprintf(a.Stderr, "no manual entry for '%s'\n", topic)
		return 1
	}
	if dir, err := os.MkdirTemp("", "zelta-man"); err == nil {
		defer os.RemoveAll(dir)
		file := filepath.Join(dir, name+"."+section)
		if os.WriteFile(file, page.Roff, 0o644) == nil {
			if _, err := exec.LookPath("mandoc"); err == nil {
				return run("mandoc", "-a", file)
			}
			if _, err := exec.LookPath("man"); err == nil {
				return run("man", "-l", file)
			}
		}
	}
	_, _ = a.Stdout.Write(page.Markdown)
	return 0
}
