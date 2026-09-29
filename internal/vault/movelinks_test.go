package vault

import (
	"testing"
)

func TestMoveRewritesImageLinks(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string // path -> content ("" content for images)
		op    func(v *Vault) (string, error)
		want  map[string]string // path -> expected content after the op
	}{
		{
			name: "note moved deeper",
			files: map[string]string{
				"attachments/x.png": "",
				"Note.md":           "# N\n\n![alt](attachments/x.png)\n",
			},
			op:   func(v *Vault) (string, error) { return v.Move("Note.md", "Work/Sub") },
			want: map[string]string{"Work/Sub/Note.md": "# N\n\n![alt](../../attachments/x.png)\n"},
		},
		{
			name: "note moved shallower",
			files: map[string]string{
				"attachments/x.png": "",
				"Work/Sub/Note.md":  "![](../../attachments/x.png) and ![b](../../attachments/x.png \"t\")\n",
			},
			op:   func(v *Vault) (string, error) { return v.Move("Work/Sub/Note.md", "") },
			want: map[string]string{"Note.md": "![](attachments/x.png) and ![b](attachments/x.png \"t\")\n"},
		},
		{
			name: "root-relative, external and missing targets untouched",
			files: map[string]string{
				"attachments/x.png": "",
				"Note.md":           "![](/attachments/x.png)\n![](https://e.com/a.png)\n![](missing.png)\n",
			},
			op:   func(v *Vault) (string, error) { return v.Move("Note.md", "Work") },
			want: map[string]string{"Work/Note.md": "![](/attachments/x.png)\n![](https://e.com/a.png)\n![](missing.png)\n"},
		},
		{
			name: "rename in place leaves content byte-identical",
			files: map[string]string{
				"attachments/x.png": "",
				"Work/Note.md":      "![](./../attachments/x.png)\n",
			},
			op:   func(v *Vault) (string, error) { return v.Rename("Work/Note.md", "Renamed") },
			want: map[string]string{"Work/Renamed.md": "![](./../attachments/x.png)\n"},
		},
		{
			name: "folder move rewrites every note inside",
			files: map[string]string{
				"attachments/x.png": "",
				"Work/img.png":      "",
				"Work/N.md":         "![](../attachments/x.png) ![](img.png)\n",
				"Work/Sub/N2.md":    "![](../../attachments/x.png) ![](../img.png)\n",
				"Other.md":          "![](attachments/x.png)\n",
			},
			op: func(v *Vault) (string, error) { return v.Move("Work", "Archive") },
			want: map[string]string{
				"Archive/Work/N.md":      "![](../../attachments/x.png) ![](img.png)\n",
				"Archive/Work/Sub/N2.md": "![](../../../attachments/x.png) ![](../img.png)\n",
				"Other.md":               "![](attachments/x.png)\n",
			},
		},
		{
			name: "folder rename keeps links valid",
			files: map[string]string{
				"attachments/x.png": "",
				"A/Work/N.md":       "![](../../attachments/x.png)\n",
			},
			op:   func(v *Vault) (string, error) { return v.Rename("A", "B") },
			want: map[string]string{"B/Work/N.md": "![](../../attachments/x.png)\n"},
		},
		{
			name: "non-note files are not rewritten",
			files: map[string]string{
				"attachments/x.png": "",
				"Work/readme.txt":   "![](../attachments/x.png)\n",
			},
			op:   func(v *Vault) (string, error) { return v.Move("Work", "Deep/Er") },
			want: map[string]string{"Deep/Er/Work/readme.txt": "![](../attachments/x.png)\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			for p, c := range tt.files {
				writeFile(t, v, p, c)
			}
			if _, err := tt.op(v); err != nil {
				t.Fatalf("op: %v", err)
			}
			for p, want := range tt.want {
				if got := readFile(t, v, p); got != want {
					t.Errorf("%s = %q, want %q", p, got, want)
				}
			}
			assertNoTmp(t, v.Root)
		})
	}
}
