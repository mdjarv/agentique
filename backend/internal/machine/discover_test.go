package machine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestParseTailnetPeers(t *testing.T) {
	raw := []byte(`{
		"Self": {"DNSName": "me.tail1.ts.net."},
		"Peer": {
			"key1": {"DNSName": "zbook.tail1.ts.net.", "Online": true},
			"key2": {"DNSName": "phone.tail1.ts.net.", "Online": false},
			"key3": {"DNSName": "", "Online": true}
		}
	}`)
	peers := parseTailnetPeers(raw)
	if len(peers) != 2 {
		t.Fatalf("peers = %d, want 2 (empty DNSName dropped)", len(peers))
	}
	byName := map[string]bool{}
	for _, p := range peers {
		byName[p.DNSName] = p.Online
	}
	if !byName["zbook.tail1.ts.net"] {
		t.Fatal("zbook should be online with trailing dot stripped")
	}
	if online, ok := byName["phone.tail1.ts.net"]; !ok || online {
		t.Fatal("phone should be present and offline")
	}

	if got := parseTailnetPeers([]byte("not json")); got != nil {
		t.Fatalf("invalid json should yield nil, got %v", got)
	}
}

func TestCandidateURLs(t *testing.T) {
	urls := candidateURLs("zbook.ts.net", []string{"19201", "9201", "19201", ""})
	want := []string{
		"https://zbook.ts.net:19201",
		"http://zbook.ts.net:19201",
		"https://zbook.ts.net:9201",
		"http://zbook.ts.net:9201",
	}
	if len(urls) != len(want) {
		t.Fatalf("urls = %v, want %v", urls, want)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("urls[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}

func TestProbeDescriptor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agentique/environment" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"machineId":"abc-123","label":"zbook","version":"v1","capabilities":{"pairing":true}}`))
	}))
	defer srv.Close()

	peer, ok := probeDescriptor(context.Background(), srv.Client(), srv.URL)
	if !ok {
		t.Fatal("probe should succeed")
	}
	if peer.MachineID != "abc-123" || peer.Label != "zbook" || !peer.Pairing {
		t.Fatalf("unexpected peer: %+v", peer)
	}
	if peer.URL != srv.URL {
		t.Fatalf("url = %q, want %q", peer.URL, srv.URL)
	}

	// A non-agentique server (404 or bad body) never matches.
	notOurs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>hello</html>"))
	}))
	defer notOurs.Close()
	if _, ok := probeDescriptor(context.Background(), notOurs.Client(), notOurs.URL); ok {
		t.Fatal("non-agentique server must not match")
	}
	if _, err := url.Parse(notOurs.URL); err != nil {
		t.Fatal(err)
	}
}
