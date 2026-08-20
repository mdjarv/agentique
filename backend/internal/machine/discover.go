package machine

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Tailnet peer discovery (multi-machine M4): enumerate online tailnet peers
// via `tailscale status --json` and probe each for an agentique descriptor,
// so the Add-machine dialog can offer one-click suggestions instead of a
// typed URL. Discovery is a HINT layer only — it never grants access
// (pairing/bearer auth is unchanged) and absence of Tailscale is a normal
// outcome, not an error.

// DiscoveredPeer is one reachable agentique server on the tailnet.
type DiscoveredPeer struct {
	MachineID string `json:"machineId"`
	Label     string `json:"label"`
	URL       string `json:"url"`
	Version   string `json:"version"`
	Pairing   bool   `json:"pairing"`
}

type tailnetPeer struct {
	DNSName string
	Online  bool
}

// parseTailnetPeers extracts online peers' MagicDNS names from `tailscale
// status --json` output.
func parseTailnetPeers(raw []byte) []tailnetPeer {
	var status struct {
		Peer map[string]struct {
			DNSName string `json:"DNSName"`
			Online  bool   `json:"Online"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil
	}
	peers := make([]tailnetPeer, 0, len(status.Peer))
	for _, p := range status.Peer {
		name := strings.TrimSuffix(strings.TrimSpace(p.DNSName), ".")
		if name == "" {
			continue
		}
		peers = append(peers, tailnetPeer{DNSName: name, Online: p.Online})
	}
	return peers
}

// candidateURLs builds the probe order for one peer: HTTPS before HTTP on
// each port, own listen port first (peers tend to mirror each other's setup)
// then the well-known defaults.
func candidateURLs(dnsName string, ports []string) []string {
	seen := map[string]bool{}
	urls := make([]string, 0, len(ports)*2)
	for _, port := range ports {
		if port == "" || seen[port] {
			continue
		}
		seen[port] = true
		urls = append(urls,
			fmt.Sprintf("https://%s:%s", dnsName, port),
			fmt.Sprintf("http://%s:%s", dnsName, port),
		)
	}
	return urls
}

// DiscoverPeers probes online tailnet peers for agentique servers, excluding
// this machine (selfID). ownPort is this server's listen port; the well-known
// defaults are always tried too. Bounded: ~1s per HTTP attempt, all peers in
// parallel, first hit per peer wins.
func DiscoverPeers(ctx context.Context, selfID, ownPort string) []DiscoveredPeer {
	statusCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(statusCtx, "tailscale", "status", "--json").Output()
	if err != nil {
		return nil
	}

	ports := []string{ownPort, "9201", "19201"}
	client := &http.Client{
		Timeout: 1 * time.Second,
		// Discovery only reads the public descriptor; a self-signed peer is
		// still worth suggesting (the pairing flow decides trust).
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec
	}

	var mu sync.Mutex
	var found []DiscoveredPeer
	var wg sync.WaitGroup
	for _, peer := range parseTailnetPeers(out) {
		if !peer.Online {
			continue
		}
		wg.Add(1)
		go func(dnsName string) {
			defer wg.Done()
			// All candidates probe concurrently (a non-agentique peer would
			// otherwise cost len(candidates)×timeout sequentially); the
			// lowest candidate index that hit wins, keeping the https-first /
			// own-port-first preference deterministic even if one host runs
			// servers on several ports.
			candidates := candidateURLs(dnsName, ports)
			hits := make([]*DiscoveredPeer, len(candidates))
			var probes sync.WaitGroup
			for i, base := range candidates {
				probes.Add(1)
				go func(i int, base string) {
					defer probes.Done()
					if peer, ok := probeDescriptor(ctx, client, base); ok {
						hits[i] = &peer
					}
				}(i, base)
			}
			probes.Wait()
			for _, hit := range hits {
				if hit == nil {
					continue
				}
				if hit.MachineID == selfID {
					return
				}
				mu.Lock()
				found = append(found, *hit)
				mu.Unlock()
				return
			}
		}(peer.DNSName)
	}
	wg.Wait()
	return found
}

func probeDescriptor(ctx context.Context, client *http.Client, base string) (DiscoveredPeer, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/.well-known/agentique/environment", nil)
	if err != nil {
		return DiscoveredPeer{}, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return DiscoveredPeer{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return DiscoveredPeer{}, false
	}

	var desc struct {
		MachineID    string          `json:"machineId"`
		Label        string          `json:"label"`
		Version      string          `json:"version"`
		Capabilities map[string]bool `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&desc); err != nil || desc.MachineID == "" {
		return DiscoveredPeer{}, false
	}
	return DiscoveredPeer{
		MachineID: desc.MachineID,
		Label:     desc.Label,
		URL:       base,
		Version:   desc.Version,
		Pairing:   desc.Capabilities["pairing"],
	}, true
}
