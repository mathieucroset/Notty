package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLatest(t *testing.T) {
	huge := `{"tag_name":"v9.9.9","body":"` + strings.Repeat("a", maxBody) + `"}`
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr string
	}{
		{"ok", 200, `{"tag_name":"v0.2.0","name":"Notty 0.2.0","draft":false}`, "v0.2.0", ""},
		{"ok without v", 200, `{"tag_name":"0.2.0"}`, "0.2.0", ""},
		{"no release yet", 404, `{"message":"Not Found"}`, "", "404"},
		{"rate limited 403", 403, `{"message":"API rate limit exceeded"}`, "", "403"},
		{"rate limited 429", 429, ``, "", "429"},
		{"server error", 500, ``, "", "500"},
		{"bad json", 200, `{"tag_name":`, "", "decoding"},
		{"huge body", 200, huge, "", "too large"},
		{"empty tag", 200, `{"tag_name":""}`, "", "not a release version"},
		{"missing tag", 200, `{}`, "", "not a release version"},
		{"non-releasable tag", 200, `{"tag_name":"v1.0.0-rc1"}`, "", "not a release version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			got, err := Latest(context.Background(), srv.Client(), srv.URL, "0.1.0")
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Latest = %+v, want an error containing %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Latest: %v", err)
			}
			if got.Version != tt.want {
				t.Errorf("Version = %q, want %q", got.Version, tt.want)
			}
		})
	}
}

func TestLatestSendsHeaders(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.0"}`))
	}))
	defer srv.Close()
	if _, err := Latest(context.Background(), srv.Client(), srv.URL+"/repos/o/r/releases/latest", "0.1.0"); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("no request received")
	}
	if got.Method != http.MethodGet {
		t.Errorf("method = %s, want GET", got.Method)
	}
	if got.URL.Path != "/repos/o/r/releases/latest" {
		t.Errorf("path = %s", got.URL.Path)
	}
	for header, want := range map[string]string{
		"Accept":               "application/vnd.github+json",
		"User-Agent":           "notty/0.1.0",
		"X-Github-Api-Version": "2022-11-28",
	} {
		if v := got.Header.Get(header); v != want {
			t.Errorf("%s = %q, want %q", header, v, want)
		}
	}
}

func TestLatestNilClientUsesDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.0"}`))
	}))
	defer srv.Close()
	if _, err := Latest(context.Background(), nil, srv.URL, "0.1.0"); err != nil {
		t.Fatal(err)
	}
}

func TestLatestHonoursContextTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	begin := time.Now()
	_, err := Latest(ctx, srv.Client(), srv.URL, "0.1.0")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(begin); d > 3*time.Second {
		t.Errorf("Latest took %v despite a 50ms deadline", d)
	}
}

func TestLatestBadURL(t *testing.T) {
	if _, err := Latest(context.Background(), nil, "://bad", "0.1.0"); err == nil {
		t.Error("Latest with a bad URL returned no error")
	}
}

func TestDefaultAPIURL(t *testing.T) {
	const want = "https://api.github.com/repos/mathieucroset/notty/releases/latest"
	if DefaultAPIURL != want {
		t.Errorf("DefaultAPIURL = %q, want %q", DefaultAPIURL, want)
	}
}
