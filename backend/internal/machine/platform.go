package machine

// ValidPlatformOS bounds a machine's reported operating system before it is
// stored: empty (unknown / older peer) or a short lowercase token shaped like
// a GOOS value ("linux", "windows", "darwin"). Deliberately not an allowlist —
// an unrecognised OS renders as the generic host glyph client-side, and a new
// GOOS must not need a server release to be storable.
func ValidPlatformOS(os string) bool {
	if os == "" {
		return true
	}
	if len(os) > 16 {
		return false
	}
	for _, c := range os {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
