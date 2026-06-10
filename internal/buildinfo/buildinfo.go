// Package buildinfo holds build metadata injected at link time via -ldflags
// (see deploy.sh). It mirrors the godev buildinfo pattern so a deployed binary
// can report exactly what it is.
package buildinfo

import "fmt"

var (
	Version        = "dev"
	GitCommit      = "unknown"
	GitBranch      = "unknown"
	GitDirty       = "unknown"
	BuildTimestamp = "unknown"
	BuildUser      = "unknown"
	BuildMachine   = "unknown"
	GoVersion      = "unknown"
)

// String renders a multi-line human-readable build summary.
func String() string {
	return fmt.Sprintf(
		"locus-go\n"+
			"  version:    %s\n"+
			"  gitCommit:  %s\n"+
			"  gitBranch:  %s\n"+
			"  gitDirty:   %s\n"+
			"  buildTime:  %s\n"+
			"  buildUser:  %s\n"+
			"  buildHost:  %s\n"+
			"  goVersion:  %s",
		Version, GitCommit, GitBranch, GitDirty, BuildTimestamp, BuildUser, BuildMachine, GoVersion,
	)
}
