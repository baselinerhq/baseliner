// Package version holds the baseliner build version.
package version

// Version is the current baseliner version. Override at build time with
// -ldflags "-X github.com/baselinerhq/baseliner/internal/version.Version=x.y.z".
var Version = "0.2.0"
