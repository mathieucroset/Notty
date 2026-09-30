package update

import "testing"

func TestReleasable(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{"v0.1.0", true},
		{"0.1.0", true}, // GoReleaser's version has no "v"
		{"v1.22.333", true},
		{"v0.10.0", true},
		{"", false},
		{"v", false},
		{"dev", false},
		{"fa3bfe9", false},
		{"v0.1.0-3-gabc1234", false},
		{"v0.1.0-dirty", false},
		{"v0.1.0-3-gabc1234-dirty", false},
		{"v1.0.0-rc1", false},
		{"v1.0.0+build", false},
		{"v1.0", false},
		{"v1.0.0.0", false},
		{"v1..0", false},
		{"v-1.0.0", false},
		{"v+1.0.0", false},
		{"V1.0.0", false},
		{"vv1.0.0", false},
		{" v1.0.0", false},
		{"v1.0.0\n", false},
		{"v99999999999999999999.0.0", false}, // overflows
	}
	for _, tt := range tests {
		if got := Releasable(tt.v); got != tt.want {
			t.Errorf("Releasable(%q) = %v, want %v", tt.v, got, tt.want)
		}
	}
}

func TestNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"v0.10.0", "v0.9.0", true}, // numeric, not lexical
		{"v0.9.0", "v0.10.0", false},
		{"v0.2.0", "v0.1.0", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.1.1", "v0.1.0", true},
		{"v0.1.0", "v0.1.0", false}, // equal
		{"v0.1.0", "v0.2.0", false}, // older
		{"v0.2.0", "0.1.0", true},   // current without "v" (GoReleaser)
		{"0.2.0", "v0.1.0", true},   // latest without "v"
		{"0.1.0", "0.1.0", false},
		{"v0.2.0", "dev", false},
		{"v0.2.0", "fa3bfe9", false},
		{"v0.2.0", "v0.1.0-3-gabc1234", false},
		{"v0.2.0", "v0.1.0-dirty", false},
		{"v0.2.0", "", false},
		{"v1.0.0-rc1", "v0.1.0", false},
		{"v0.2.0", "v0.1.0-rc1", false},
		{"", "v0.1.0", false},
		{"dev", "v0.1.0", false},
	}
	for _, tt := range tests {
		if got := Newer(tt.latest, tt.current); got != tt.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}
