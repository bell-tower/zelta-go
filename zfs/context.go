package zfs

import (
	"context"
	"fmt"
	"strings"

	"git.belltower.it/djbell/zelta-go/endpoint"
)

// Features reports which optional ZFS properties appeared in a dataset get.
// Absence means the host/tree does not expose the property (pre-feature ZFS).
type Features struct {
	Encryption         bool
	ReceiveResumeToken bool
	SnapshotsChanged   bool
	Origin             bool
	IVSetGUID          bool // true when encryption property exists (ivsetguid listable)
}

// DatasetContext is filesystem/volume property state for one endpoint tree.
// Populated by LoadDatasetContext (zfs get) or future cache hints.
type DatasetContext struct {
	Root     string
	Exists   bool
	// BySuffix maps ds_suffix ("" = root) → property name → value.
	BySuffix map[string]map[string]string
	Features Features
}

// Prop returns a property for ds_suffix, or "".
func (c *DatasetContext) Prop(dsSuffix, name string) string {
	if c == nil {
		return ""
	}
	m := c.BySuffix[dsSuffix]
	if m == nil {
		return ""
	}
	return m[name]
}

// SourceEncrypted reports whether any dataset has encryption enabled (Awk DSTree source_encrypted).
func (c *DatasetContext) SourceEncrypted() bool {
	if c == nil {
		return false
	}
	for _, m := range c.BySuffix {
		if encryptionEnabled(m["encryption"]) {
			return true
		}
	}
	return false
}

// AncestorProp walks from dsSuffix up to root looking for the first non-empty property.
func (c *DatasetContext) AncestorProp(dsSuffix, name string) string {
	if c == nil {
		return ""
	}
	suf := dsSuffix
	for {
		if v := c.Prop(suf, name); v != "" && v != "-" {
			return v
		}
		if suf == "" {
			return ""
		}
		// "/a/b" → "/a" → ""
		if i := strings.LastIndex(suf, "/"); i > 0 {
			suf = suf[:i]
		} else {
			suf = ""
		}
	}
}

// ParseGetLines parses zfs get -Hpr -o name,property,value lines into a DatasetContext.
func ParseGetLines(root string, lines []string) (*DatasetContext, error) {
	ctx := &DatasetContext{
		Root:     root,
		Exists:   true,
		BySuffix: make(map[string]map[string]string),
	}
	seenKeys := make(map[string]bool)
	for i, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			return nil, fmt.Errorf("get line %d: got %d fields, want 3", i+1, len(fields))
		}
		name, key, val := fields[0], fields[1], fields[2]
		if root != "" && name != root && !strings.HasPrefix(name, root+"/") {
			continue
		}
		suf, err := endpoint.DSSuffix(root, name)
		if err != nil {
			continue
		}
		if val == "off" {
			val = "0"
		}
		m := ctx.BySuffix[suf]
		if m == nil {
			m = make(map[string]string)
			ctx.BySuffix[suf] = m
		}
		m[key] = val
		seenKeys[key] = true
	}
	ctx.Features = Features{
		Encryption:         seenKeys["encryption"],
		ReceiveResumeToken: seenKeys["receive_resume_token"],
		SnapshotsChanged:   seenKeys["snapshots_changed"],
		Origin:             seenKeys["origin"],
		IVSetGUID:          seenKeys["encryption"], // ivsetguid available when encryption is
	}
	return ctx, nil
}

// LoadDatasetContext runs GetProps and parses the result.
// Missing dataset → Exists=false, nil error (Awk load_properties return 0).
func LoadDatasetContext(ctx context.Context, exec Executor, epStr, dataset string, depth int) (*DatasetContext, error) {
	if dataset == "" {
		return nil, fmt.Errorf("dataset context: empty dataset")
	}
	lines, err := exec.GetProps(ctx, epStr, dataset, "all", depth)
	if err != nil {
		if isMissingDataset(err) {
			return &DatasetContext{Root: dataset, Exists: false, BySuffix: map[string]map[string]string{}}, nil
		}
		return nil, fmt.Errorf("get properties %s: %w", dataset, err)
	}
	dc, err := ParseGetLines(dataset, lines)
	if err != nil {
		return nil, err
	}
	dc.Exists = true
	return dc, nil
}

// ProbeFeatures loads top-level-only properties (depth 1) to learn optional feature support.
func ProbeFeatures(ctx context.Context, exec Executor, epStr, dataset string) (Features, error) {
	dc, err := LoadDatasetContext(ctx, exec, epStr, dataset, 1)
	if err != nil {
		return Features{}, err
	}
	if !dc.Exists {
		return Features{}, nil
	}
	return dc.Features, nil
}

func encryptionEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "-", "0", "off", "none":
		return false
	default:
		return true
	}
}
