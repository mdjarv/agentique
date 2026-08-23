package update

import "testing"

func TestChannel(t *testing.T) {
	cases := map[string]string{
		"v0.4.1":                  ChannelRelease,
		"v10.20.30":               ChannelRelease,
		"dev":                     ChannelDev,
		"":                        ChannelDev,
		"v0.4.1-7-gab12cd3":       ChannelDev,
		"v0.4.1-dirty":            ChannelDev,
		"v0.4.1-7-gab12cd3-dirty": ChannelDev,
		"local":                   ChannelDev,
		// No pre-release channel (U8): a tag that isn't a plain vX.Y.Z is a
		// dev build as far as nagging is concerned.
		"v0.5.0-rc1": ChannelDev,
	}
	for in, want := range cases {
		if got := Channel(in); got != want {
			t.Errorf("Channel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.4.1", "v0.5.0", -1},
		{"v0.5.0", "v0.4.1", 1},
		{"v0.4.1", "v0.4.1", 0},
		{"v0.4.9", "v0.4.10", -1},
		{"v1.0.0", "v0.9.9", 1},
		// Unknown compares equal: an unparseable version must never make us
		// claim a machine is behind.
		{"dev", "v0.5.0", 0},
		{"v0.4.1-7-gab12cd3", "v0.5.0", 0},
		{"v0.4.1", "", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestPlatformMatrix(t *testing.T) {
	// Build wide (U5): every platform release.yml publishes has a name here.
	for _, p := range []struct{ os, arch string }{
		{"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}, {"darwin", "arm64"},
	} {
		if AssetName(p.os, p.arch) == "" {
			t.Errorf("no asset name for %s/%s", p.os, p.arch)
		}
	}
	if AssetName("plan9", "386") != "" {
		t.Error("unexpected asset for an unbuilt platform")
	}

	// Enable narrow: only platforms someone has actually run.
	if !Verified("linux", "amd64") {
		t.Error("linux/amd64 must be verified")
	}
	for _, p := range []struct{ os, arch string }{
		{"linux", "arm64"}, {"windows", "amd64"}, {"darwin", "arm64"},
	} {
		if Verified(p.os, p.arch) {
			t.Errorf("%s/%s is published but not verified — it must not offer in-app apply", p.os, p.arch)
		}
	}
}
