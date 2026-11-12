package config

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// envFileForbidden lists keys that must be exported before zelta.env loads.
var envFileForbidden = map[string]bool{"ZELTA_AWK": true, "ZELTA_ETC": true, "ZELTA_ENV": true}

var (
	envKeyRe      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	envCommentRe  = regexp.MustCompile(`[[:space:]]*#.*$`)
	envFileTimout = 30 * time.Second
)

// EnvFileLine is one assignment from zelta.env.
type EnvFileLine struct {
	Key  string // with the ZELTA_ prefix
	Expr string // unevaluated shell word
}

// ParseEnvFile reads zelta.env assignments. The ZELTA_ prefix is optional.
// Values are shell words that are evaluated later; trailing comments are
// removed exactly as Zelta always has. Invalid lines produce warnings.
func ParseEnvFile(r io.Reader) (lines []EnvFileLine, warnings []string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimLeft(sc.Text(), " \t\r\f\v")
		if line == "" || strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, expr, ok := strings.Cut(line, "=")
		if !ok || !envKeyRe.MatchString(key) {
			warnings = append(warnings, fmt.Sprintf("ignoring invalid zelta.env line: %s", sc.Text()))
			continue
		}
		if !strings.HasPrefix(key, EnvPrefix) {
			key = EnvPrefix + key
		}
		if envFileForbidden[key] {
			warnings = append(warnings, key+" ignored; it must be exported prior to zelta.env")
			continue
		}
		expr = envCommentRe.ReplaceAllString(expr, "")
		lines = append(lines, EnvFileLine{Key: key, Expr: expr})
	}
	if err := sc.Err(); err != nil {
		warnings = append(warnings, fmt.Sprintf("error reading zelta.env: %v", err))
	}
	return lines, warnings
}

// EvalEnvFile evaluates zelta.env assignments with a POSIX shell, exactly as
// "eval ': ${KEY:=value}'" would: values are expanded (quotes, variables,
// and command substitutions) and variables already set in env win. It
// returns the resulting value of every assigned key, without the ZELTA_
// prefix. env is the environment the shell runs with.
func EvalEnvFile(ctx context.Context, lines []EnvFileLine, env []string) (map[string]string, []string, error) {
	if len(lines) == 0 {
		return map[string]string{}, nil, nil
	}
	var script strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&script, ": ${%s:=%s}\nprintf '%%s=%%s\\0' %s \"${%s}\"\n", l.Key, l.Expr, l.Key, l.Key)
	}
	ctx, cancel := context.WithTimeout(ctx, envFileTimout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script.String())
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	vals := map[string]string{}
	for rec := range strings.SplitSeq(stdout.String(), "\x00") {
		k, v, ok := strings.Cut(rec, "=")
		if ok && strings.HasPrefix(k, EnvPrefix) {
			vals[strings.TrimPrefix(k, EnvPrefix)] = v
		}
	}
	var warnings []string
	for l := range strings.SplitSeq(strings.TrimSpace(stderr.String()), "\n") {
		if l != "" {
			warnings = append(warnings, "zelta.env: "+l)
		}
	}
	if runErr != nil && len(vals) < len(lines) {
		return vals, warnings, fmt.Errorf("evaluating zelta.env: %w", runErr)
	}
	return vals, warnings, nil
}

// LoadEnvFile parses and evaluates the file at path. A missing or unreadable
// file yields no values and no error, as it always has.
func LoadEnvFile(ctx context.Context, path string, env []string) (map[string]string, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}, nil, nil
	}
	defer f.Close()
	lines, warnings := ParseEnvFile(f)
	vals, w, err := EvalEnvFile(ctx, lines, env)
	return vals, append(warnings, w...), err
}
