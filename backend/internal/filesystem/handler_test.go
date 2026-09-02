package filesystem

import "testing"

// The windows shape ("C:\" as the first crumb) cannot run under a linux test
// binary — filepath switches on GOOS — but the walk-up loop is shared, so this
// pins its contract: root first as its own crumb, every crumb navigable,
// termination at the point where Dir stops changing the path.
func TestPathSegments(t *testing.T) {
	segs := pathSegments("/home/u/git/repo")
	want := []segment{
		{Name: "/", Path: "/"},
		{Name: "home", Path: "/home"},
		{Name: "u", Path: "/home/u"},
		{Name: "git", Path: "/home/u/git"},
		{Name: "repo", Path: "/home/u/git/repo"},
	}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments %v, want %d", len(segs), segs, len(want))
	}
	for i, w := range want {
		if segs[i] != w {
			t.Errorf("segment %d: got %+v, want %+v", i, segs[i], w)
		}
	}

	root := pathSegments("/")
	if len(root) != 1 || root[0].Path != "/" {
		t.Errorf("root should be exactly its own crumb, got %v", root)
	}
}
