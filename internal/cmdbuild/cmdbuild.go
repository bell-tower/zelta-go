package cmdbuild

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"git.belltower.it/djbell/zelta-go/data"
)

// Remote roles from cmds.tsv column 2 (oracle REMOTE_SEND / REMOTE_RECV / REMOTE_DEFAULT).
const (
	RoleNone    = ""
	RoleDefault = "DEFAULT"
	RoleSend    = "SEND"
	RoleRecv    = "RECV"
)

// Template is one cmds.tsv row.
type Template struct {
	Action  string
	Remote  string // SEND, RECV, DEFAULT, or empty
	Command string
	Args    []string
	Vars    []string
}

var (
	loadOnce  sync.Once
	templates map[string]Template
	loadErr   error
)

// Load reads embedded cmds.tsv (cached).
func Load() (map[string]Template, error) {
	loadOnce.Do(func() {
		templates = make(map[string]Template)
		b, err := data.ReadFile("cmds.tsv")
		if err != nil {
			loadErr = err
			return
		}
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		for sc.Scan() {
			line := sc.Text()
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) < 3 || f[0] == "" {
				continue
			}
			t := Template{
				Action:  f[0],
				Remote:  f[1],
				Command: f[2],
			}
			if len(f) > 3 && f[3] != "" {
				t.Args = strings.Fields(f[3])
			}
			if len(f) > 4 && f[4] != "" {
				t.Vars = strings.Fields(f[4])
			}
			templates[t.Action] = t
		}
		loadErr = sc.Err()
	})
	return templates, loadErr
}

// Get returns a template by action name.
func Get(action string) (Template, error) {
	m, err := Load()
	if err != nil {
		return Template{}, err
	}
	t, ok := m[action]
	if !ok {
		return Template{}, fmt.Errorf("cmdbuild: unknown action %q", action)
	}
	return t, nil
}

// RemoteRole returns cmds.tsv REMOTE column for action (SEND/RECV/DEFAULT/"").
func RemoteRole(action string) (string, error) {
	t, err := Get(action)
	if err != nil {
		return "", err
	}
	return t.Remote, nil
}

// StdinNull is true when remote wrapper should use ssh -n (oracle REMOTE_SEND/DEFAULT).
// RECV keeps stdin open for the replication stream.
func StdinNull(role string) bool {
	return role != RoleRecv
}

// Build expands a template with vars into argv (no remote/ssh wrapping).
// Empty var values are skipped. Var values may contain spaces (split into fields).
func Build(action string, vars map[string]string) ([]string, error) {
	t, err := Get(action)
	if err != nil {
		return nil, err
	}
	var out []string
	if t.Command != "" {
		out = append(out, strings.Fields(t.Command)...)
	}
	out = append(out, t.Args...)
	for _, k := range t.Vars {
		v := vars[k]
		if v == "" {
			continue
		}
		out = append(out, strings.Fields(v)...)
	}
	return out, nil
}

// ListArgv builds LIST from cmds.tsv (match flags: -t all -S createtxg, optional -d).
func ListArgv(props []string, depth int, dataset string) ([]string, error) {
	if dataset == "" {
		return nil, fmt.Errorf("cmdbuild: LIST empty dataset")
	}
	flags := "-t all -S createtxg"
	if depth > 0 {
		flags += " -d " + strconv.Itoa(depth)
	}
	return Build("LIST", map[string]string{
		"props": strings.Join(props, ","),
		"flags": flags,
		"ds":    dataset,
	})
}

// SnapArgv builds SNAP (always -r per cmds.tsv) for dataset@snap.
func SnapArgv(datasetSnap string) ([]string, error) {
	if datasetSnap == "" {
		return nil, fmt.Errorf("cmdbuild: SNAP empty name")
	}
	return Build("SNAP", map[string]string{"ds_snap": datasetSnap})
}

// ResumeSendArgv builds zfs send -t TOKEN without normal send flags.
func ResumeSendArgv(token string) ([]string, error) {
	if token == "" {
		return nil, fmt.Errorf("cmdbuild: SEND_RESUME empty token")
	}
	return Build("SEND_RESUME", map[string]string{"resume_token": token})
}

// CreateArgv builds CREATE (zfs create -vupo canmount=noauto) for parent datasets.
func CreateArgv(dataset string) ([]string, error) {
	if dataset == "" {
		return nil, fmt.Errorf("cmdbuild: CREATE empty dataset")
	}
	return Build("CREATE", map[string]string{"ds": dataset})
}

// CheckArgv builds CHECK (zfs list -Ho name) for existence probes.
func CheckArgv(dataset string) ([]string, error) {
	if dataset == "" {
		return nil, fmt.Errorf("cmdbuild: CHECK empty dataset")
	}
	return Build("CHECK", map[string]string{"ds": dataset})
}

// BookmarkArgv builds BOOKMARK source@snap source#bookmark.
func BookmarkArgv(sourceSnap, bookmark string) ([]string, error) {
	if sourceSnap == "" || bookmark == "" {
		return nil, fmt.Errorf("cmdbuild: BOOKMARK requires source and bookmark")
	}
	return Build("BOOKMARK", map[string]string{"source_snap": sourceSnap, "bookmark": bookmark})
}

// CloneArgv builds CLONE source@snap target.
func CloneArgv(sourceSnap, dataset string) ([]string, error) {
	if sourceSnap == "" || dataset == "" {
		return nil, fmt.Errorf("cmdbuild: CLONE requires source and target")
	}
	return Build("CLONE", map[string]string{"ds_snap": sourceSnap, "ds": dataset})
}

// RenameArgv builds RENAME old target.
func RenameArgv(oldDataset, newDataset string) ([]string, error) {
	if oldDataset == "" || newDataset == "" {
		return nil, fmt.Errorf("cmdbuild: RENAME requires old and new dataset")
	}
	return Build("RENAME", map[string]string{"old_ds": oldDataset, "new_ds": newDataset})
}
