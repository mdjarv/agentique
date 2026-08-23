package update

import "runtime"

// Build wide, enable narrow (decision U5).
//
// Cross-compiling costs nothing, so every platform we can build gets a
// published asset and a working `install.sh`. Publishing an asset is not the
// same as promising it self-upgrades: in-app apply is enabled only for
// platforms someone has actually run agentique on, and a platform graduates
// when it is verified, not when it compiles.

// publishedAssets maps "GOOS/GOARCH" to the asset name
// .github/workflows/release.yml uploads. Keep the two in step — a name here
// with no asset there produces a download that 404s at apply time.
var publishedAssets = map[string]string{
	"linux/amd64":   "agentique-linux-amd64",
	"linux/arm64":   "agentique-linux-arm64",
	"windows/amd64": "agentique-windows-amd64.exe",
	"darwin/arm64":  "agentique-darwin-arm64",
}

// verifiedPlatforms is the allowlist for in-app apply. Everything else reports
// supported:false and the UI says "manual upgrade". Windows is built but not
// verified (replacing a running .exe and restarting a scheduled task have
// never been exercised on real hardware); darwin is inference only.
var verifiedPlatforms = map[string]bool{
	"linux/amd64": true,
}

// AssetName returns the release asset published for a platform, or "" when we
// do not build one.
func AssetName(goos, goarch string) string {
	return publishedAssets[goos+"/"+goarch]
}

// Verified reports whether in-app apply is enabled for a platform.
func Verified(goos, goarch string) bool {
	return verifiedPlatforms[goos+"/"+goarch]
}

// ThisPlatform is the running server's platform key, for logging and errors.
func ThisPlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }
