package setup_test

import (
	"testing"

	"github.com/mathieucroset/notty/internal/setup"
)

func TestValidateRepoName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"notes", true},
		{"my-notes_2.0", true},
		{"owner/notes", true},
		{"", false},
		{"my notes", false},
		{"--help", false},
		{".hidden", false},
		{"a/b/c", false},
		{"owner/", false},
		{"/notes", false},
		{"nötes", false},
	}
	for _, c := range cases {
		err := setup.ValidateRepoName(c.name)
		if (err == nil) != c.ok {
			t.Errorf("ValidateRepoName(%q) = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}
