package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// DefaultAPIURL is the unauthenticated GitHub endpoint for the newest
// published release. 60 requests/hour/IP is the anonymous budget; one per hour
// per machine is nowhere near it.
const DefaultAPIURL = "https://api.github.com/repos/mdjarv/agentique/releases/latest"

// Release is the slice of the GitHub release payload we use.
type Release struct {
	TagName string  `json:"tag_name"`
	Body    string  `json:"body"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

// Asset is one published file on a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Find returns the named asset, or nil.
func (r *Release) Find(name string) *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// errNotModified reports a 304 against the cached ETag: the release we already
// have is still the newest one.
var errNotModified = errors.New("not modified")

// fetchRelease asks the API for the latest release, sending the cached ETag so
// an unchanged release costs a 304 rather than a body. Returns errNotModified
// when the server confirms the cache.
func fetchRelease(ctx context.Context, client *http.Client, url, etag string) (*Release, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, errNotModified
	}
	if resp.StatusCode != http.StatusOK {
		// Read a little of the body so rate-limit and not-found answers say so.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, "", fmt.Errorf("release API returned %d: %s", resp.StatusCode, snippet)
	}

	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, "", fmt.Errorf("decode release: %w", err)
	}
	if rel.TagName == "" {
		return nil, "", errors.New("release API returned no tag_name")
	}
	return &rel, resp.Header.Get("ETag"), nil
}
