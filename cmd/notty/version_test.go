package main

import (
	"runtime/debug"
	"testing"

	"github.com/mathieucroset/notty/internal/update"
)

func TestResolveVersion(t *testing.T) {
	const pseudo = "v0.1.1-0.20260930120000-abcdef123456"
	info := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/mathieucroset/notty", Version: v}}
	}
	tests := []struct {
		name       string
		ldflags    string
		info       *debug.BuildInfo
		ok         bool
		want       string
		releasable bool
	}{
		{"go install of a release", "dev", info("v0.2.0"), true, "v0.2.0", true},
		{"local build", "dev", info("(devel)"), true, "dev", false},
		{"pseudo-version", "dev", info(pseudo), true, pseudo, false},
		{"no module version", "dev", info(""), true, "dev", false},
		{"no build info", "dev", nil, false, "dev", false},
		{"ldflags version kept", "0.1.0", info("v0.2.0"), true, "0.1.0", true},
		{"ldflags describe kept", "v0.1.0-3-gabc1234", info("(devel)"), true, "v0.1.0-3-gabc1234", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveVersion(tt.ldflags, tt.info, tt.ok)
			if got != tt.want {
				t.Errorf("resolveVersion(%q, %v) = %q, want %q", tt.ldflags, tt.ok, got, tt.want)
			}
			if r := update.Releasable(got); r != tt.releasable {
				t.Errorf("Releasable(%q) = %v, want %v", got, r, tt.releasable)
			}
		})
	}
}
