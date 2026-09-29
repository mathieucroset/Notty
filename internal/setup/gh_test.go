package setup_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/setup"
)

func TestNewGH(t *testing.T) {
	type call struct {
		dir  string
		args []string
	}
	var calls []call
	var deadlines []time.Duration
	gh := setup.NewGH(func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		calls = append(calls, call{dir, append([]string{name}, args...)})
		if d, ok := ctx.Deadline(); ok {
			deadlines = append(deadlines, time.Until(d))
		}
		return nil, nil
	})
	ctx := context.Background()
	if !gh.Available(ctx) {
		t.Error("Available = false")
	}
	if err := gh.CreateRepo(ctx, "/v", "notes"); err != nil {
		t.Fatal(err)
	}
	want := []call{
		{"", []string{"gh", "auth", "status", "--hostname", "github.com"}},
		{"/v", []string{"gh", "repo", "create", "notes", "--private", "--source", ".", "--remote", "origin", "--push"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %q, want %q", calls, want)
	}
	if len(deadlines) != 2 || deadlines[0] > 15*time.Second || deadlines[1] > 2*time.Minute || deadlines[1] < time.Minute {
		t.Errorf("deadlines = %v, want ≤15s for auth status and 2m for create", deadlines)
	}
}

func TestGHErrors(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want error
	}{
		{"not logged in", "You are not logged into any GitHub hosts. To log in, run: gh auth login", gitsync.ErrAuth},
		{"bad credentials", "HTTP 401: Bad credentials (https://api.github.com/graphql)", gitsync.ErrAuth},
		{"push publickey", "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.", gitsync.ErrAuth},
		{"dns", "error connecting to api.github.com\ncheck your internet connection", gitsync.ErrNetwork},
		{"resolve", "Post \"https://api.github.com/graphql\": dial tcp: lookup api.github.com: no such host", gitsync.ErrNetwork},
		{"timeout", "Post \"https://api.github.com/graphql\": net/http: TLS handshake timeout", gitsync.ErrNetwork},
		{"name taken", "GraphQL: Name already exists on this account (createRepository)", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := setup.NewGH(func(context.Context, string, string, ...string) ([]byte, error) {
				return []byte(tt.out + "\n"), errors.New("exit status 1")
			})
			if gh.Available(context.Background()) {
				t.Error("Available = true when gh fails")
			}
			err := gh.CreateRepo(context.Background(), "/v", "notes")
			if err == nil || !strings.Contains(err.Error(), strings.SplitN(tt.out, "\n", 2)[0]) {
				t.Fatalf("err = %v, want gh's output included", err)
			}
			for _, sentinel := range []error{gitsync.ErrAuth, gitsync.ErrNetwork} {
				if got := errors.Is(err, sentinel); got != (sentinel == tt.want) {
					t.Errorf("errors.Is(err, %v) = %v", sentinel, got)
				}
			}
		})
	}
}
