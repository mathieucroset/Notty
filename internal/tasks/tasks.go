// Package tasks implements parsing and toggling of GitHub-flavored markdown
// task list items ("- [ ] text" / "- [x] text"), as used by Notty's editor,
// preview, and Tasks view (see docs/superpowers/specs/2026-09-29-notty-design.md
// §5 "Todo editing" and "Toggling a task from outside the editor", and §8
// "Tasks view").
package tasks

import (
	"regexp"
	"strings"
	"time"
)

// Task represents a single parsed task list item.
type Task struct {
	Line   int
	Indent int
	Done   bool
	Text   string
	Due    *time.Time
	Raw    string
}

// taskRe matches a task list line, capturing:
//  1. leading indentation (spaces/tabs)
//  2. the list marker ("-", "*", "+", or "N.")
//  3. the whitespace between the marker and the checkbox
//  4. the checkbox character (" ", "x", or "X")
//  5. everything after the closing bracket
var taskRe = regexp.MustCompile(`^([ \t]*)([-*+]|[0-9]+\.)([ \t]+)\[([ xX])\](.*)$`)

// listRe matches a list item line that has no checkbox yet.
var listRe = regexp.MustCompile(`^([ \t]*)([-*+]|[0-9]+\.)([ \t]+)(.*)$`)

// dueRe matches an inline due date marker like "@2026-10-05".
var dueRe = regexp.MustCompile(`@(\d{4}-\d{2}-\d{2})`)

// ParseLine parses a single line as a task list item. It recognizes "-",
// "*", "+", and numbered ("1.") markers, any amount of leading
// spaces/tabs (recorded as Indent), and "x"/"X" (normalized to Done=true)
// or " " for the checkbox. Text is everything after "] ", right-trimmed.
func ParseLine(line string) (Task, bool) {
	m := taskRe.FindStringSubmatch(line)
	if m == nil {
		return Task{}, false
	}
	indent := len(m[1])
	checkbox := m[4]
	done := checkbox == "x" || checkbox == "X"
	text := extractText(m[5])
	task := Task{
		Indent: indent,
		Done:   done,
		Text:   text,
		Raw:    line,
	}
	task.Due = DueDate(text)
	return task, true
}

// extractText trims a single leading space (the separator after "]") and
// any trailing whitespace from the remainder of a task line.
func extractText(rest string) string {
	rest = strings.TrimPrefix(rest, " ")
	return strings.TrimRight(rest, " \t")
}

// Parse parses every task list item in content, skipping lines inside
// fenced code blocks (``` or ~~~). Task.Line is 0-based.
func Parse(content string) []Task {
	lines := strings.Split(content, "\n")
	var result []Task
	inFence := false
	var fenceChar byte
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if isFenceDelim(trimmed) {
			ch := trimmed[0]
			if !inFence {
				inFence = true
				fenceChar = ch
			} else if ch == fenceChar {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		if task, ok := ParseLine(line); ok {
			task.Line = i
			result = append(result, task)
		}
	}
	return result
}

// isFenceDelim reports whether s (already left-trimmed) opens or closes a
// fenced code block: a run of at least 3 backticks or tildes.
func isFenceDelim(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '`' && c != '~' {
		return false
	}
	return s[0] == c && s[1] == c && s[2] == c
}

// ToggleLine toggles the checkbox on line.
//   - Task line: flips [ ] <-> [x] (uppercase X is normalized to lowercase x
//     on toggle to unchecked, i.e. any checked state becomes " ").
//   - List item without a checkbox ("- foo", "1. foo"): inserts "[ ] " after
//     the marker.
//   - Plain text: becomes indent + "- [ ] " + trimmed text.
//   - Blank line: becomes "- [ ] ".
func ToggleLine(line string) string {
	if m := taskRe.FindStringSubmatch(line); m != nil {
		indent, marker, ws, checkbox, rest := m[1], m[2], m[3], m[4], m[5]
		newCheckbox := "x"
		if checkbox == "x" || checkbox == "X" {
			newCheckbox = " "
		}
		return indent + marker + ws + "[" + newCheckbox + "]" + rest
	}
	if m := listRe.FindStringSubmatch(line); m != nil {
		indent, marker, ws, rest := m[1], m[2], m[3], m[4]
		return indent + marker + ws + "[ ] " + rest
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "- [ ] "
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	return indent + "- [ ] " + trimmed
}

// DueDate returns the first "@YYYY-MM-DD" occurrence in text that is a
// valid calendar date, parsed at midnight in the local timezone. Invalid
// dates (e.g. "@2026-13-40") are skipped in favor of a later valid match.
// Returns nil if there is no valid due date.
func DueDate(text string) *time.Time {
	matches := dueRe.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		t, err := time.ParseInLocation("2006-01-02", m[1], time.Local)
		if err == nil {
			return &t
		}
	}
	return nil
}

// headingLevel returns the heading level (number of leading '#' characters,
// 1-6) of line, or 0 if line is not a heading.
func headingLevel(line string) int {
	trimmed := strings.TrimLeft(line, " \t")
	n := 0
	for n < len(trimmed) && trimmed[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0
	}
	if n < len(trimmed) && trimmed[n] != ' ' && trimmed[n] != '\t' {
		return 0
	}
	return n
}

// Progress counts done/total tasks appearing after the heading at
// headingLine, stopping at the next heading of the same or higher level
// (fewer or equal '#' characters), skipping fenced code blocks. If
// headingLine does not point at a heading, returns 0, 0.
func Progress(lines []string, headingLine int) (done, total int) {
	if headingLine < 0 || headingLine >= len(lines) {
		return 0, 0
	}
	level := headingLevel(lines[headingLine])
	if level == 0 {
		return 0, 0
	}

	inFence := false
	var fenceChar byte
	// Establish fence state up to and including headingLine.
	for i := 0; i <= headingLine; i++ {
		trimmed := strings.TrimLeft(lines[i], " \t")
		if isFenceDelim(trimmed) {
			ch := trimmed[0]
			if !inFence {
				inFence = true
				fenceChar = ch
			} else if ch == fenceChar {
				inFence = false
			}
		}
	}

	for i := headingLine + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		if isFenceDelim(trimmed) {
			ch := trimmed[0]
			if !inFence {
				inFence = true
				fenceChar = ch
			} else if ch == fenceChar {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		if lvl := headingLevel(line); lvl > 0 && lvl <= level {
			break
		}
		if task, ok := ParseLine(line); ok {
			total++
			if task.Done {
				done++
			}
		}
	}
	return done, total
}

// FindLine locates the line for a toggle request identified by (line, text),
// per spec §5 "Toggling a task from outside the editor": if lines[line] is a
// task whose Raw equals text (exactly or after trimming), that line is
// returned. Otherwise, every line is searched for an exact Raw match, and
// the match nearest to line wins. If nothing matches, returns false.
func FindLine(lines []string, line int, text string) (int, bool) {
	if line >= 0 && line < len(lines) {
		candidate := lines[line]
		if _, ok := ParseLine(candidate); ok {
			if candidate == text || strings.TrimSpace(candidate) == strings.TrimSpace(text) {
				return line, true
			}
		}
	}

	best := -1
	bestDist := 0
	for i, l := range lines {
		if l != text {
			continue
		}
		dist := i - line
		if dist < 0 {
			dist = -dist
		}
		if best == -1 || dist < bestDist {
			best = i
			bestDist = dist
		}
	}
	if best == -1 {
		return 0, false
	}
	return best, true
}
