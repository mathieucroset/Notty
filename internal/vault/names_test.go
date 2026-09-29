package vault

import "testing"

func TestFileNameFromTitle(t *testing.T) {
	tests := []struct {
		name, title, want string
	}{
		{"plain", "Standup notes", "Standup notes.md"},
		{"special chars removed", `a/b\c:d*e?f"g<h>i|j`, "abcdefghij.md"},
		{"leading dots stripped", "...hidden", "hidden.md"},
		{"inner dots kept", "v1.2 notes", "v1.2 notes.md"},
		{"whitespace trimmed", "  spaced out \t", "spaced out.md"},
		{"space before dots", " .dot", "dot.md"},
		{"dots then space", ". x", "x.md"},
		{"empty", "", "Untitled.md"},
		{"only whitespace", "   ", "Untitled.md"},
		{"only forbidden", `/\:*?"<>|`, "Untitled.md"},
		{"only dots", "..", "Untitled.md"},
		{"control chars removed", "a\nb\tc", "abc.md"},
		{"unicode kept", "Café ☕", "Café ☕.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FileNameFromTitle(tt.title); got != tt.want {
				t.Errorf("FileNameFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestTitle(t *testing.T) {
	tests := []struct {
		name, content, rel, want string
	}{
		{"h1", "# Hello\n\nbody", "x.md", "Hello"},
		{"h1 not first line", "intro\n\n# Later\n", "x.md", "Later"},
		{"h1 trimmed", "#   Spaced  \n", "x.md", "Spaced"},
		{"closing hashes stripped", "# Title ##\n", "x.md", "Title"},
		{"crlf", "# Win\r\nbody", "x.md", "Win"},
		{"h2 ignored", "## Sub\n", "dir/Name.md", "Name"},
		{"no space ignored", "#tag line\n", "Name.md", "Name"},
		{"empty heading ignored", "# \n# Real\n", "x.md", "Real"},
		{"fallback nested", "no heading", "Work/Standup notes.md", "Standup notes"},
		{"fallback empty content", "", "a.md", "a"},
		{"fallback non-md", "", "dir/file.txt", "file.txt"},
		{"fenced backticks skipped", "```\n# not\n```\n# Yes\n", "x.md", "Yes"},
		{"fenced tildes skipped", "~~~sh\n# comment\n~~~\n", "f.md", "f"},
		{"fence needs matching char", "```\n~~~\n# inside\n```\n# out\n", "x.md", "out"},
		{"fence needs enough length", "````\n```\n# inside\n````\n# out\n", "x.md", "out"},
		{"unclosed fence", "```\n# inside\n", "u.md", "u"},
		{"indented fence", "   ```\n# inside\n   ```\n# out\n", "x.md", "out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Title(tt.content, tt.rel); got != tt.want {
				t.Errorf("Title(%q, %q) = %q, want %q", tt.content, tt.rel, got, tt.want)
			}
		})
	}
}
