package zfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// DatasetType is an OpenZFS dataset kind.
type DatasetType string

const (
	DatasetFilesystem DatasetType = "filesystem"
	DatasetSnapshot   DatasetType = "snapshot"
	DatasetVolume     DatasetType = "volume"
	DatasetPool       DatasetType = "pool"
	DatasetBookmark   DatasetType = "bookmark"
)

// PropertySource identifies where OpenZFS obtained a property value.
type PropertySource string

const (
	PropertySourceNone      PropertySource = "none"
	PropertySourceDefault   PropertySource = "default"
	PropertySourceTemporary PropertySource = "temporary"
	PropertySourceLocal     PropertySource = "local"
	PropertySourceInherited PropertySource = "inherited"
	PropertySourceReceived  PropertySource = "received"
)

// Property is one OpenZFS property value. Value remains raw JSON so the
// observation layer does not impose a property-specific Go type.
type Property struct {
	Value      json.RawMessage
	Source     PropertySource
	SourceData string
}

// String returns a JSON string property value.
func (p Property) String() (string, bool) {
	var value string
	if json.Unmarshal(p.Value, &value) != nil {
		return "", false
	}
	return value, true
}

// Uint64 returns a property represented as either a JSON number or a numeric
// JSON string. OpenZFS emits strings by default and numbers with --json-int.
func (p Property) Uint64() (uint64, bool) {
	var value json.Number
	decoder := json.NewDecoder(bytes.NewReader(p.Value))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		var text string
		if json.Unmarshal(p.Value, &text) != nil {
			return 0, false
		}
		value = json.Number(text)
	}
	n, err := strconv.ParseUint(string(value), 10, 64)
	return n, err == nil
}

// PropertySet is the named property collection returned for one dataset.
type PropertySet map[string]Property

// Get returns a property by its OpenZFS name.
func (p PropertySet) Get(name string) (Property, bool) {
	value, ok := p[name]
	return value, ok
}

// Dataset is the provider-neutral subset of an OpenZFS dataset observation.
// It intentionally models identity and properties, not an open libzfs handle.
type Dataset struct {
	Name       string      `json:"name"`
	Type       DatasetType `json:"type"`
	Pool       string      `json:"pool"`
	CreateTXG  uint64      `json:"createtxg,string"`
	Properties PropertySet `json:"properties"`
}

// GetJSON is the structured document emitted by zfs get -j.
type GetJSON struct {
	OutputVersion map[string]any     `json:"output_version"`
	Datasets      map[string]Dataset `json:"datasets"`
}

// UnmarshalJSON accepts both OpenZFS's default numeric strings and --json-int
// numeric values for dataset createtxg and property values.
func (d *Dataset) UnmarshalJSON(data []byte) error {
	type wireDataset struct {
		Name       string              `json:"name"`
		Type       DatasetType         `json:"type"`
		Pool       string              `json:"pool"`
		CreateTXG  flexibleUint64      `json:"createtxg"`
		Properties map[string]Property `json:"properties"`
	}
	var wire wireDataset
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*d = Dataset{
		Name:       wire.Name,
		Type:       DatasetType(strings.ToLower(string(wire.Type))),
		Pool:       wire.Pool,
		CreateTXG:  uint64(wire.CreateTXG),
		Properties: wire.Properties,
	}
	return nil
}

type flexibleUint64 uint64

func (v *flexibleUint64) UnmarshalJSON(data []byte) error {
	var text string
	if json.Unmarshal(data, &text) == nil {
		n, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return err
		}
		*v = flexibleUint64(n)
		return nil
	}
	var n uint64
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("uint64 value: %w", err)
	}
	*v = flexibleUint64(n)
	return nil
}

func (p *Property) UnmarshalJSON(data []byte) error {
	var wire struct {
		Value  json.RawMessage `json:"value"`
		Source struct {
			Type PropertySource `json:"type"`
			Data string         `json:"data"`
		} `json:"source"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	p.Value = append(p.Value[:0], wire.Value...)
	p.Source = PropertySource(strings.ToLower(string(wire.Source.Type)))
	p.SourceData = wire.Source.Data
	return nil
}
