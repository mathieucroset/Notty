package links

import (
	"strings"
	"testing"
)

func TestFindImages(t *testing.T) {
	content := strings.Join([]string{
		`![alt one](a.png) and ![alt two](b.png "a title")`, // 0
		"```",                                // 1
		"![fenced](should-not-appear.png)",   // 2
		"```",                                // 3
		"some `![code span](skip.png)` text", // 4
		"![angled](<my file.png>)",           // 5
		"no image here",                      // 6
	}, "\n")

	got := FindImages(content)

	wantLines := []int{0, 0, 5}
	if len(got) != len(wantLines) {
		t.Fatalf("FindImages returned %d images, want %d: %+v", len(got), len(wantLines), got)
	}
	for i, l := range wantLines {
		if got[i].Line != l {
			t.Errorf("got[%d].Line = %d, want %d", i, got[i].Line, l)
		}
	}

	if got[0].Alt != "alt one" || got[0].Target != "a.png" {
		t.Errorf("got[0] = %+v, want Alt=alt one Target=a.png", got[0])
	}
	if got[1].Alt != "alt two" || got[1].Target != "b.png" {
		t.Errorf("got[1] = %+v, want Alt=alt two Target=b.png (title stripped)", got[1])
	}
	if got[2].Alt != "angled" || got[2].Target != "my file.png" {
		t.Errorf("got[2] = %+v, want Alt=angled Target=my file.png", got[2])
	}

	// Verify Start/End cover the whole "![alt](target)" span.
	line0 := strings.Split(content, "\n")[0]
	if line0[got[0].Start:got[0].End] != "![alt one](a.png)" {
		t.Errorf("got[0] span = %q, want %q", line0[got[0].Start:got[0].End], "![alt one](a.png)")
	}
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		noteRel string
		wantRel string
		wantExt bool
	}{
		{"root relative", "/x/y.png", "a/b/n.md", "x/y.png", false},
		{"note relative", "img.png", "a/b/n.md", "a/b/img.png", false},
		{"note relative up", "../img.png", "a/b/n.md", "a/img.png", false},
		{"external http", "http://example.com/x.png", "a/b/n.md", "", true},
		{"external https", "https://example.com/x.png", "a/b/n.md", "", true},
		{"external data", "data:image/png;base64,AAA", "a/b/n.md", "", true},
		{"escapes vault", "../../../img.png", "a/b/n.md", "", false},
		{"percent encoded space", "my%20file.png", "a/b/n.md", "a/b/my file.png", false},
		{"root note", "img.png", "n.md", "img.png", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotRel, gotExt := Resolve(c.target, c.noteRel)
			if gotRel != c.wantRel || gotExt != c.wantExt {
				t.Errorf("Resolve(%q, %q) = (%q, %v), want (%q, %v)", c.target, c.noteRel, gotRel, gotExt, c.wantRel, c.wantExt)
			}
		})
	}
}

func TestRewriteForMove(t *testing.T) {
	exists := func(vaultRel string) bool {
		return vaultRel == "a/img.png"
	}

	t.Run("move to vault root", func(t *testing.T) {
		content := "![pic](../img.png)\n"
		got, changed := RewriteForMove(content, "a/b/n.md", "n.md", exists)
		if !changed {
			t.Fatal("expected changed = true")
		}
		want := "![pic](a/img.png)\n"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("move to deep folder", func(t *testing.T) {
		content := "![pic](../img.png)\n"
		got, changed := RewriteForMove(content, "a/b/n.md", "x/y/z/n.md", exists)
		if !changed {
			t.Fatal("expected changed = true")
		}
		want := "![pic](../../../a/img.png)\n"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("root-relative untouched", func(t *testing.T) {
		content := "![pic](/a/img.png)\n"
		got, changed := RewriteForMove(content, "a/b/n.md", "n.md", exists)
		if changed {
			t.Fatal("expected changed = false")
		}
		if got != content {
			t.Errorf("got %q, want unchanged %q", got, content)
		}
	})

	t.Run("external untouched", func(t *testing.T) {
		content := "![pic](https://example.com/img.png)\n"
		got, changed := RewriteForMove(content, "a/b/n.md", "n.md", exists)
		if changed {
			t.Fatal("expected changed = false")
		}
		if got != content {
			t.Errorf("got %q, want unchanged %q", got, content)
		}
	})

	t.Run("non-existent target untouched", func(t *testing.T) {
		content := "![pic](../missing.png)\n"
		got, changed := RewriteForMove(content, "a/b/n.md", "n.md", exists)
		if changed {
			t.Fatal("expected changed = false")
		}
		if got != content {
			t.Errorf("got %q, want unchanged %q", got, content)
		}
	})
}

func TestImageOnlyParagraph(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"single image", "![a](a.png)", true},
		{"two images with space", "![a](a.png) ![b](b.png)", true},
		{"leading/trailing whitespace", "  ![a](a.png)  ", true},
		{"image with text", "see this: ![a](a.png)", false},
		{"text after image", "![a](a.png) some text", false},
		{"no image", "just text", false},
		{"empty line", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ImageOnlyParagraph(c.line)
			if got != c.want {
				t.Errorf("ImageOnlyParagraph(%q) = %v, want %v", c.line, got, c.want)
			}
		})
	}
}

func TestRelPath(t *testing.T) {
	cases := []struct {
		fromDir string
		toRel   string
		want    string
	}{
		{".", "a/img.png", "a/img.png"},
		{"x/y/z", "a/img.png", "../../../a/img.png"},
		{"a/b", "a/img.png", "../img.png"},
		{"a", "a/b/img.png", "b/img.png"},
	}
	for _, c := range cases {
		got := RelPath(c.fromDir, c.toRel)
		if got != c.want {
			t.Errorf("RelPath(%q, %q) = %q, want %q", c.fromDir, c.toRel, got, c.want)
		}
	}
}
