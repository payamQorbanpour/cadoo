// Package version exposes the build version, set via -ldflags at link time.
package version

// Version is the build identifier. The Makefile sets it from `git describe`.
var Version = "dev"
