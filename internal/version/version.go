// Package version reports the Zelta version.
package version

// Version is the release version, overridden at build time with
// -ldflags "-X github.com/bell-tower/zelta-go/internal/version.Version=...".
var Version = "1.3.0-dev"

// String returns the version banner printed by "zelta version".
func String() string { return "Zelta " + Version }

// Zprune returns the version banner printed by "zprune --version".
func Zprune() string { return "zprune " + Version }
