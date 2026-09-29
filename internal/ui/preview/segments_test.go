package preview

import (
	"testing"
)

type segWant struct {
	kind       SegmentKind
	start, end int
	targets    []string
	markdown   string // checked when non-empty
}

func checkSegments(t *testing.T, content string, want []segWant) {
	t.Helper()
	got := Split(content)
	if len(got) != len(want) {
		for i, s := range got {
			t.Logf("seg %d: kind=%v lines=%d-%d md=%q imgs=%d", i, s.Kind, s.StartLine, s.EndLine, s.Markdown, len(s.Images))
		}
		t.Fatalf("got %d segments, want %d", len(got), len(want))
	}
	for i, w := range want {
		s := got[i]
		if s.Kind != w.kind || s.StartLine != w.start || s.EndLine != w.end {
			t.Errorf("seg %d: kind=%v lines=%d-%d, want kind=%v lines=%d-%d", i, s.Kind, s.StartLine, s.EndLine, w.kind, w.start, w.end)
		}
		if len(s.Images) != len(w.targets) {
			t.Errorf("seg %d: %d images, want %d", i, len(s.Images), len(w.targets))
			continue
		}
		for j, tg := range w.targets {
			if s.Images[j].Target != tg {
				t.Errorf("seg %d image %d: target %q, want %q", i, j, s.Images[j].Target, tg)
			}
		}
		if w.markdown != "" && s.Markdown != w.markdown {
			t.Errorf("seg %d: markdown %q, want %q", i, s.Markdown, w.markdown)
		}
	}
}

func TestSplitImageOnlyParagraph(t *testing.T) {
	content := "# Title\n\n![cat](cat.png)\n\nAfter."
	checkSegments(t, content, []segWant{
		{kind: Text, start: 0, end: 0, markdown: "# Title"},
		{kind: Image, start: 2, end: 2, targets: []string{"cat.png"}},
		{kind: Text, start: 4, end: 4, markdown: "After."},
	})
}

func TestSplitInlineImageGetsFollowUpBlock(t *testing.T) {
	content := "See ![diagram](/a/d.png) here.\nSecond line.\n\nNext"
	checkSegments(t, content, []segWant{
		{kind: Text, start: 0, end: 1, markdown: "See ![diagram](/a/d.png) here.\nSecond line."},
		{kind: Image, start: 0, end: 0, targets: []string{"/a/d.png"}},
		{kind: Text, start: 3, end: 3},
	})
}

func TestSplitFencedBlockIsNeverAnImage(t *testing.T) {
	content := "```\n![x](x.png)\n\n![y](y.png)\n```\n\n~~~md\n![z](z.png)\n~~~"
	checkSegments(t, content, []segWant{
		{kind: Text, start: 0, end: 4, markdown: "```\n![x](x.png)\n\n![y](y.png)\n```"},
		{kind: Text, start: 6, end: 8},
	})
}

func TestSplitMultipleImagesInOneParagraph(t *testing.T) {
	content := "![a](a.png) ![b](b.png)\n![c](c.png)"
	checkSegments(t, content, []segWant{
		{kind: Image, start: 0, end: 1, targets: []string{"a.png", "b.png", "c.png"}},
	})
}

func TestSplitMultipleInlineImages(t *testing.T) {
	content := "one ![a](a.png) two ![b](b.png)"
	checkSegments(t, content, []segWant{
		{kind: Text, start: 0, end: 0},
		{kind: Image, start: 0, end: 0, targets: []string{"a.png", "b.png"}},
	})
}

func TestSplitLineRangesAndIndentedContinuations(t *testing.T) {
	content := "\n\n- item\n\n    continued\n\n\npara one\npara two\n\n\n"
	checkSegments(t, content, []segWant{
		{kind: Text, start: 2, end: 4, markdown: "- item\n\n    continued"},
		{kind: Text, start: 7, end: 8, markdown: "para one\npara two"},
	})
}

func TestSplitInlineCodeImageIsNotAnImage(t *testing.T) {
	content := "`![a](a.png)`"
	checkSegments(t, content, []segWant{{kind: Text, start: 0, end: 0}})
}

func TestSplitEmpty(t *testing.T) {
	if got := Split(""); len(got) != 0 {
		t.Fatalf("Split(\"\") = %v, want none", got)
	}
	if got := Split("\n  \n"); len(got) != 0 {
		t.Fatalf("Split(blank) = %v, want none", got)
	}
}

func TestSegmentForLineSkipsFollowUpImageBlock(t *testing.T) {
	segs := Split("text ![a](a.png)\nmore\n\nnext")
	for line, want := range map[int]int{0: 0, 1: 0, 3: 2} {
		if got := segmentForLine(segs, line); got != want {
			t.Errorf("segmentForLine(%d) = %d, want %d", line, got, want)
		}
	}
	if got := segmentForLine(nil, 3); got != -1 {
		t.Errorf("segmentForLine(nil) = %d, want -1", got)
	}
}

func TestSegmentContaining(t *testing.T) {
	segs := Split("a\n\nb\n\n\nc")
	cases := map[int]int{0: 0, 1: 0, 2: 1, 3: 1, 4: 1, 5: 2, 99: 2, -1: 0}
	for line, want := range cases {
		if got := segmentForLine(segs, line); got != want {
			t.Errorf("segmentForLine(%d) = %d, want %d", line, got, want)
		}
	}
}
