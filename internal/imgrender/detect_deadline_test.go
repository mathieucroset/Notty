package imgrender

import (
	"os"
	"testing"
	"time"
)

// scheduledTTY delivers reply chunks at fixed offsets from the first Read
// and honours read deadlines.
type scheduledTTY struct {
	chunks   []scheduledChunk
	start    time.Time
	deadline time.Time
}

type scheduledChunk struct {
	at   time.Duration
	data string
}

func (s *scheduledTTY) Write(p []byte) (int, error)       { return len(p), nil }
func (s *scheduledTTY) SetReadDeadline(t time.Time) error { s.deadline = t; return nil }
func (s *scheduledTTY) Read(p []byte) (int, error) {
	if s.start.IsZero() {
		s.start = time.Now()
	}
	if len(s.chunks) == 0 {
		time.Sleep(time.Until(s.deadline))
		return 0, os.ErrDeadlineExceeded
	}
	next := s.start.Add(s.chunks[0].at)
	if !s.deadline.IsZero() && s.deadline.Before(next) {
		time.Sleep(time.Until(s.deadline))
		return 0, os.ErrDeadlineExceeded
	}
	time.Sleep(time.Until(next))
	n := copy(p, s.chunks[0].data)
	s.chunks = s.chunks[1:]
	return n, nil
}

func TestIncompleteEscape(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"abc", false},
		{"\x1b", true},
		{"\x1b[", true},
		{"\x1b[6;20", true},
		{"\x1b[6;20;10t", false},
		{"\x1b[?62;4c", false},
		{"\x1b_Gi=31;O", true},
		{"\x1b_Gi=31;OK\x1b", true},
		{"\x1b_Gi=31;OK\x1b\\", false},
		{"\x1b]11;rgb:0/0/0", true},
		{"\x1b]11;rgb:0/0/0\x07", false},
		{"\x1bPabc", true},
		{"\x1b7", false},
	}
	for _, tt := range tests {
		if got := incompleteEscape(tt.in); got != tt.want {
			t.Errorf("incompleteEscape(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestDetectFinishesSequenceCutByDeadline(t *testing.T) {
	tests := []struct {
		name   string
		chunks []scheduledChunk
		inline Protocol
		maxDur time.Duration
	}{
		{
			name: "kitty reply split across the deadline",
			chunks: []scheduledChunk{
				{10 * time.Millisecond, "\x1b_Gi=31;O"},
				{115 * time.Millisecond, "K\x1b\\"},
			},
			inline: ProtoKitty,
			maxDur: 300 * time.Millisecond,
		},
		{
			name: "complete replies are not extended",
			chunks: []scheduledChunk{
				{10 * time.Millisecond, replyCell},
				{115 * time.Millisecond, replyKittyOK},
			},
			inline: ProtoHalfBlocks,
			maxDur: 300 * time.Millisecond,
		},
		{
			name: "extension happens once and is short",
			chunks: []scheduledChunk{
				{10 * time.Millisecond, "\x1b_Gi=31;O"},
				{200 * time.Millisecond, "K\x1b\\"},
			},
			inline: ProtoHalfBlocks,
			maxDur: 190 * time.Millisecond,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			got := Detect("auto", envMap(), &scheduledTTY{chunks: tt.chunks}, noRun(t))
			if got.Inline != tt.inline {
				t.Errorf("inline = %v, want %v", got.Inline, tt.inline)
			}
			if el := time.Since(start); el > tt.maxDur {
				t.Errorf("took %v, want < %v", el, tt.maxDur)
			}
		})
	}
}
