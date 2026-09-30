package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// DefaultAPIURL is GitHub's "latest release" endpoint for Notty. It
// excludes drafts and releases marked pre-release.
const DefaultAPIURL = "https://api.github.com/repos/mathieucroset/notty/releases/latest"

// maxBody caps the response size read from the API.
const maxBody = 1 << 20

// Release is the newest published release.
type Release struct {
	Version string // "v0.2.0" (the tag)
}

// Latest fetches the latest release from GitHub's REST API at apiURL.
// version is the running version, sent in the User-Agent. The caller's ctx
// bounds the request; a nil client means http.DefaultClient. A non-200
// status, a body over 1 MiB, bad JSON, or a tag that is not a clean
// release version is an error.
func Latest(ctx context.Context, client *http.Client, apiURL, version string) (Release, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Release{}, fmt.Errorf("update: building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "notty/"+version)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("update: fetching the latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("update: fetching the latest release: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Release{}, fmt.Errorf("update: reading the latest release: %w", err)
	}
	if len(body) > maxBody {
		return Release{}, errors.New("update: latest release response too large")
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, fmt.Errorf("update: decoding the latest release: %w", err)
	}
	if !Releasable(payload.TagName) {
		return Release{}, fmt.Errorf("update: latest release tag %q is not a release version", payload.TagName)
	}
	return Release{Version: payload.TagName}, nil
}
