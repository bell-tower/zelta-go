package config

import (
	"maps"
	"slices"
	"strings"

	"github.com/bell-tower/zelta-go/internal/units"
)

// Source records where an option value came from, in precedence order.
type Source uint8

const (
	FromDefault Source = iota
	FromEnvFile
	FromPolicy
	FromEnv
	FromCLI
)

func (s Source) String() string {
	return [...]string{"default", "zelta.env", "policy", "environment", "command line"}[s]
}

// EnvPrefix is the prefix of Zelta environment variables.
const EnvPrefix = "ZELTA_"

type entry struct {
	val string
	src Source
}

// Options is a set of resolved option values keyed without the ZELTA_
// prefix. The zero value is not usable; call NewOptions.
type Options struct {
	m map[string]entry
}

// NewOptions returns an empty option set.
func NewOptions() *Options { return &Options{m: map[string]entry{}} }

// Get returns the value of key, or "" when unset.
func (o *Options) Get(key string) string { return o.m[key].val }

// Lookup returns the value of key and whether it is set.
func (o *Options) Lookup(key string) (string, bool) {
	e, ok := o.m[key]
	return e.val, ok
}

// Source returns where key's value came from.
func (o *Options) Source(key string) Source { return o.m[key].src }

// Set assigns key from the given source.
func (o *Options) Set(key, val string, src Source) { o.m[key] = entry{val, src} }

// SetDefault assigns key only if it is not already set.
func (o *Options) SetDefault(key, val string) {
	if _, ok := o.m[key]; !ok {
		o.m[key] = entry{val, FromDefault}
	}
}

// Override assigns key at source src unless a higher-precedence source
// already set it.
func (o *Options) Override(key, val string, src Source) {
	if e, ok := o.m[key]; !ok || e.src <= src {
		o.m[key] = entry{val, src}
	}
}

// Unset removes key.
func (o *Options) Unset(key string) { delete(o.m, key) }

// Has reports whether key has a non-empty value.
func (o *Options) Has(key string) bool { return o.m[key].val != "" }

// Bool interprets key the way Zelta always has: empty, "0", "no", "false",
// and "off" are false; other numbers are true when nonzero; any other
// non-empty string is true.
func (o *Options) Bool(key string) bool { return Truthy(o.Get(key)) }

// Int returns the numeric value of key, using Awk numeric conversion.
func (o *Options) Int(key string) int { return units.Atoi(o.Get(key)) }

// Clone returns an independent copy.
func (o *Options) Clone() *Options { return &Options{m: maps.Clone(o.m)} }

// Keys returns the set keys in sorted order.
func (o *Options) Keys() []string { return slices.Sorted(maps.Keys(o.m)) }

// Environ renders the options as ZELTA_KEY=value environment entries.
func (o *Options) Environ() []string {
	var env []string
	for _, k := range o.Keys() {
		env = append(env, EnvPrefix+k+"="+o.Get(k))
	}
	return env
}

// Truthy reports whether an option string is true.
func Truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "no", "false", "off":
		return false
	}
	if units.IsNumeric(v) {
		return units.Atof(v) != 0
	}
	return true
}
