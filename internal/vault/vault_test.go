package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkfiles creates files (paths ending in "/" are directories) under root.
func mkfiles(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		abs := filepath.Join(root, filepath.FromSlash(p))
		if strings.HasSuffix(p, "/") {
			if err := os.MkdirAll(abs, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func openVault(t *testing.T) *Vault {
	t.Helper()
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return v
}

func TestOpen(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{"existing dir", func(t *testing.T) string { return t.TempDir() }},
		{"missing dir created", func(t *testing.T) string { return filepath.Join(t.TempDir(), "a", "b") }},
		{"unclean path", func(t *testing.T) string { return t.TempDir() + "/x/../y/" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tt.setup(t)
			v, err := Open(root)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if !filepath.IsAbs(v.Root) || filepath.Clean(v.Root) != v.Root {
				t.Errorf("Root %q not absolute and clean", v.Root)
			}
			fi, err := os.Stat(v.Root)
			if err != nil || !fi.IsDir() {
				t.Errorf("root not a directory: %v", err)
			}
		})
	}
}

func TestOpenRejectsFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(f); err == nil {
		t.Fatal("Open on a regular file: want error")
	}
}

type flatNode struct {
	Path          string
	IsDir, IsNote bool
}

func flatten(n *Node, out *[]flatNode) {
	for _, c := range n.Children {
		*out = append(*out, flatNode{c.Path, c.IsDir, c.IsNote})
		flatten(c, out)
	}
}

func TestTree(t *testing.T) {
	v := openVault(t)
	mkfiles(t, v.Root,
		".git/HEAD",
		".notty/state.json",
		".trash/123/meta.json",
		"attachments/img.png",
		".gitignore",
		"b.md",
		"A.md",
		"c.txt",
		"zeta/",
		"Alpha/note.md",
		"Alpha/.hidden.md",
		"Alpha/.dotdir/x.md",
		"Alpha/attachments/keep.md", // only top-level attachments is hidden
		"Alpha/sub/deep.md",
		"Alpha/draft.md.notty-tmp",
		"beta/Z.md",
		"beta/a.md",
	)

	root, err := v.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if root.Path != "" || !root.IsDir || root.IsNote {
		t.Errorf("root node = %+v, want Path \"\" and IsDir", root)
	}

	var got []flatNode
	flatten(root, &got)
	want := []flatNode{
		{"Alpha", true, false},
		{"Alpha/attachments", true, false},
		{"Alpha/attachments/keep.md", false, true},
		{"Alpha/sub", true, false},
		{"Alpha/sub/deep.md", false, true},
		{"Alpha/note.md", false, true},
		{"beta", true, false},
		{"beta/a.md", false, true},
		{"beta/Z.md", false, true},
		{"zeta", true, false},
		{"A.md", false, true},
		{"b.md", false, true},
		{"c.txt", false, false},
	}
	if len(got) != len(want) {
		t.Fatalf("tree:\n got  %v\n want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("node %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// Name is the last path element.
	if n := root.Children[0].Children[0]; n.Name != "attachments" {
		t.Errorf("Name = %q, want attachments", n.Name)
	}
}

func TestAbs(t *testing.T) {
	v := openVault(t)
	tests := []struct {
		rel, want string
	}{
		{"", v.Root},
		{".", v.Root},
		{"a.md", filepath.Join(v.Root, "a.md")},
		{"Work/Standup notes.md", filepath.Join(v.Root, "Work", "Standup notes.md")},
		{"/a.md", filepath.Join(v.Root, "a.md")},
		{"../x", filepath.Join(v.Root, "x")},
		{"../../etc/passwd", filepath.Join(v.Root, "etc", "passwd")},
		{"a/../../x", filepath.Join(v.Root, "x")},
		{"a/./b/", filepath.Join(v.Root, "a", "b")},
		{"..", v.Root},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			got := v.Abs(tt.rel)
			if got != tt.want {
				t.Errorf("Abs(%q) = %q, want %q", tt.rel, got, tt.want)
			}
			if got != v.Root && !strings.HasPrefix(got, v.Root+string(filepath.Separator)) {
				t.Errorf("Abs(%q) = %q escapes root %q", tt.rel, got, v.Root)
			}
		})
	}
}
