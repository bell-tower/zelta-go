package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxImportDepth = 8

type line struct {
	text     string
	file     string
	lineNum  int
	expanded bool // true if produced from import splice (indent already applied)
}

// expandFile reads path and recursively splices import: fragments.
// Leading whitespace of the import: line is prepended to each imported line.
func expandFile(path string) ([]line, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	stack := map[string]bool{}
	var out []line
	n, err := expandInto(abs, "", 0, stack, &out)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("empty or unreadable policy file: %s", path)
	}
	return out, nil
}

func expandInto(path, baseIndent string, depth int, stack map[string]bool, out *[]line) (int, error) {
	if depth > maxImportDepth {
		return 0, fmt.Errorf("import depth exceeded near %s", path)
	}
	if stack[path] {
		return 0, fmt.Errorf("recursive import detected: %s", path)
	}
	stack[path] = true
	defer delete(stack, path)

	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	rawLines := strings.Split(string(b), "\n")
	// Drop final empty element from trailing newline so empty files stay empty.
	if len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}
	count := 0
	for i, raw := range rawLines {
		lineNum := i + 1
		trimmedRight := strings.TrimRight(raw, " \t")
		code := stripComment(trimmedRight)
		if isImport(code) {
			indent, imp := splitImport(code)
			child := resolveImport(imp, path)
			sub := []line{}
			n, err := expandInto(child, "", depth+1, stack, &sub)
			if err != nil {
				return 0, err
			}
			_ = n
			for _, s := range sub {
				text := s.text
				if text != "" {
					text = baseIndent + indent + text
				}
				*out = append(*out, line{text: text, file: s.file, lineNum: s.lineNum, expanded: true})
				count++
			}
			continue
		}
		text := raw
		if baseIndent != "" && text != "" {
			text = baseIndent + text
		}
		*out = append(*out, line{text: text, file: path, lineNum: lineNum})
		count++
	}
	return count, nil
}

func stripComment(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return strings.TrimRight(s[:i], " \t")
	}
	return s
}

func isImport(code string) bool {
	t := strings.TrimLeft(code, " ")
	return strings.HasPrefix(t, "import:")
}

func splitImport(code string) (indent, path string) {
	i := 0
	for i < len(code) && code[i] == ' ' {
		i++
	}
	indent = code[:i]
	rest := strings.TrimSpace(code[i+len("import:"):])
	return indent, rest
}

func resolveImport(path, baseFile string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(filepath.Dir(baseFile), path)
}
