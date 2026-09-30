package main

import "runtime/debug"

// resolveVersion returns the version to report: v, set with -ldflags, or,
// for a build without it ("dev"), the module version from the build info
// (info, ok as debug.ReadBuildInfo returns them). A go install of a release
// reports its tag ("v0.2.0"); a local build reports "(devel)" and stays
// "dev", and a pseudo-version is kept (neither is a release, so neither
// checks for updates).
func resolveVersion(v string, info *debug.BuildInfo, ok bool) string {
	if v != "dev" || !ok || info == nil {
		return v
	}
	if mv := info.Main.Version; mv != "" && mv != "(devel)" {
		return mv
	}
	return v
}
