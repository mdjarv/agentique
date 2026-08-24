package update

import (
	"errors"
	"testing"
)

// The asset and checksum URLs are read out of the release document, and the
// checksum only proves the download matches what that same document said. So
// the transport carrying both has to be the trustworthy part.
func TestRequireHTTPS(t *testing.T) {
	secure := []string{
		"https://github.com/mdjarv/agentique/releases/download/v1/agentique-linux-amd64",
		"HTTPS://github.com/x",
		// Loopback has no network position to attack from — same rule the
		// machine catalog applies to remote origins, and what httptest serves.
		"http://127.0.0.1:8080/asset",
		"http://localhost:8080/asset",
		"http://[::1]:8080/asset",
	}
	for _, u := range secure {
		if err := requireHTTPS(u); err != nil {
			t.Errorf("requireHTTPS(%q) = %v, want nil", u, err)
		}
	}

	insecure := []string{
		"http://github.com/x/agentique-linux-amd64",
		"http://evil.example/agentique-linux-amd64",
		"ftp://example.com/x",
		"file:///etc/passwd",
		"not a url",
		"",
	}
	for _, u := range insecure {
		if err := requireHTTPS(u); !errors.Is(err, ErrInsecureAsset) {
			t.Errorf("requireHTTPS(%q) = %v, want ErrInsecureAsset", u, err)
		}
	}
}

func TestPreflightRefusesAPlaintextAsset(t *testing.T) {
	c := NewChecker(Options{Version: "v1.0.0", GOOS: "linux", GOARCH: "amd64"})
	c.mu.Lock()
	c.rel = &Release{
		TagName: "v2.0.0",
		Assets: []Asset{
			// Published over plain HTTP to a real host: the digest that would
			// "verify" it comes down the same swappable channel.
			{Name: AssetName("linux", "amd64"), URL: "http://cdn.example/agentique", Size: 10},
			{Name: ChecksumsAsset, URL: "https://cdn.example/checksums.txt"},
		},
	}
	c.mu.Unlock()

	a := NewApplier(c, Deps{
		BinaryPath:       func() (string, error) { return t.TempDir() + "/agentique", nil },
		ServiceInstalled: func() bool { return true },
	})
	if _, err := a.Preflight(); !errors.Is(err, ErrInsecureAsset) {
		t.Errorf("Preflight = %v, want ErrInsecureAsset", err)
	}
}
