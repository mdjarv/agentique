package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/schedule"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/steward"
	"github.com/mdjarv/agentique/backend/internal/storage"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/update"
	"github.com/mdjarv/agentique/backend/internal/usage"
)

// stewardInterval is how often the steward looks. A minute: every sensor it
// reads is already cached by its own collector, so a pass is cheap, and nothing
// it reports is urgent to the second.
const stewardInterval = time.Minute

// stewardResolvedRetention is how long resolved findings stay as history.
const stewardResolvedRetention = 14 * 24 * time.Hour

// StewardBackup tells the steward where timed backups land and how often, so
// it can say when they stop. Zero Interval means backups are off.
type StewardBackup struct {
	Dir      string
	Interval time.Duration
}

// stewardDeps is everything the sensors read. Each may be nil, and a nil one
// leaves its kinds unobserved rather than reporting a condition it cannot see.
type stewardDeps struct {
	queries  *store.Queries
	svc      *session.Service
	usage    *usage.Collector
	updates  *update.Checker
	storage  *storage.Handler
	outbox   *peer.Outbox
	assist   *assistant.Service
}

// stewardStore is the findings table behind steward.Store.
type stewardStore struct{ q *store.Queries }

func (s stewardStore) OpenFindings(ctx context.Context) ([]steward.Finding, error) {
	rows, err := s.q.ListOpenStewardFindings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]steward.Finding, 0, len(rows))
	for _, r := range rows {
		f := steward.Finding{Kind: steward.Kind(r.Kind), Subject: r.Subject, Severity: steward.Severity(r.Severity),
			Remedy: steward.Remedy(r.Remedy), OpenedAt: r.OpenedAt}
		_ = json.Unmarshal([]byte(r.Facts), &f.Facts)
		out = append(out, f)
	}
	return out, nil
}

func (s stewardStore) Open(ctx context.Context, f steward.Finding) error {
	return s.q.OpenStewardFinding(ctx, store.OpenStewardFindingParams{Kind: string(f.Kind), Subject: f.Subject,
		Severity: string(f.Severity), Remedy: string(f.Remedy), Facts: factsJSON(f.Facts), OpenedAt: f.OpenedAt})
}

func (s stewardStore) Refresh(ctx context.Context, f steward.Finding) error {
	return s.q.RefreshStewardFinding(ctx, store.RefreshStewardFindingParams{Severity: string(f.Severity),
		Remedy: string(f.Remedy), Facts: factsJSON(f.Facts), Kind: string(f.Kind), Subject: f.Subject})
}

func (s stewardStore) Resolve(ctx context.Context, f steward.Finding) error {
	if err := s.q.ResolveStewardFinding(ctx, store.ResolveStewardFindingParams{ResolvedAt: f.ResolvedAt,
		Kind: string(f.Kind), Subject: f.Subject}); err != nil {
		return err
	}
	cutoff := time.Now().Add(-stewardResolvedRetention).UTC().Format(time.RFC3339)
	if _, err := s.q.PruneStewardFindings(ctx, cutoff); err != nil {
		slog.Warn("steward: history prune failed", "error", err)
	}
	return nil
}

func factsJSON(facts map[string]any) string {
	if len(facts) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// newSteward builds this machine's steward from what the server already runs.
func newSteward(d stewardDeps, backup StewardBackup) *steward.Steward {
	return steward.New(d.sense(backup), stewardStore{q: d.queries}, d.notify)
}

// sense gathers one observation. Every reading is one this machine's own
// collectors already hold; nothing here dials out or shells out except the
// storage breakdown, and that only when the disk is already under the floor.
func (d stewardDeps) sense(backup StewardBackup) func(ctx context.Context) steward.Observation {
	return func(ctx context.Context) steward.Observation {
		obs := steward.Observation{Observed: map[steward.Kind]bool{}}

		if d.usage != nil {
			doc := d.usage.Document(ctx)
			obs.Observed[steward.KindCLISignedOut] = true
			for _, a := range doc.Agents {
				if a.Kind == usage.KindGauge {
					continue
				}
				obs.Agents = append(obs.Agents, steward.AgentAuth{ID: a.ID, Name: a.Name,
					SignedOut: !a.Ready && a.AuthHelpText != "", Help: a.AuthHelpText})
			}
		}

		if st, err := storage.Stats(); err == nil {
			obs.Observed[steward.KindDiskLow] = true
			obs.Disk = steward.Disk{FreeBytes: st.FreeBytes, Path: st.Path}
			if st.FreeBytes < steward.DiskFloorBytes && d.storage != nil {
				if u, err := d.storage.Usage(ctx); err == nil && u != nil {
					obs.Disk.ReclaimableBytes = u.ReclaimableBytes
				}
			}
		}

		if d.queries != nil {
			if schedules, err := d.queries.ListSchedules(ctx); err == nil {
				obs.Observed[steward.KindLoopPaused] = true
				for _, sc := range schedules {
					if sc.Enabled == 0 && sc.PauseReason == schedule.PauseAutoFailures {
						obs.Loops = append(obs.Loops, steward.PausedLoop{ID: sc.ID, Name: sc.Name,
							SessionID: sc.SessionID, Failures: sc.ConsecutiveFailures})
					}
				}
			}
		}

		if d.svc != nil {
			if list, err := d.svc.ListAllSessions(ctx); err == nil {
				obs.Observed[steward.KindSessionBlockedLong] = true
				for _, info := range list.Sessions {
					if info.ArchivedAt != "" {
						continue
					}
					switch {
					case info.PendingApproval != nil:
						obs.Blocked = append(obs.Blocked, steward.BlockedSession{ID: info.ID, Name: info.Name, Waiting: "an approval"})
					case info.PendingQuestion != nil:
						obs.Blocked = append(obs.Blocked, steward.BlockedSession{ID: info.ID, Name: info.Name, Waiting: "a question"})
					}
				}
			}
		}

		if d.updates != nil {
			st := d.updates.Status()
			if st.CheckedAt != "" && st.CheckError == "" {
				obs.Observed[steward.KindUpdateWaiting] = true
				obs.Update = steward.Update{Behind: st.Behind, Current: st.Current, Latest: st.Latest}
			}
		}

		if backup.Interval > 0 && backup.Dir != "" {
			newest, found, err := storage.NewestPeriodicBackup(backup.Dir)
			if err == nil {
				obs.Observed[steward.KindBackupFailing] = true
				obs.Backup = steward.Backup{Enabled: true, Interval: backup.Interval}
				if found {
					obs.Backup.Newest = newest
				}
			}
		}
		return obs
	}
}

// notify tells whoever listens: this machine's assistant, and every paired
// server through the outbox.
func (d stewardDeps) notify(ctx context.Context, c steward.Change) {
	f := c.Finding
	slog.Info("steward: finding changed", "kind", f.Kind, "subject", f.Subject, "opened", c.Opened)
	if d.assist != nil {
		d.assist.IngestFinding(ctx, assistant.Finding{Kind: string(f.Kind), Subject: f.Subject,
			Severity: string(f.Severity), Remedy: string(f.Remedy), Facts: f.Facts, Opened: c.Opened})
	}
	if d.outbox != nil {
		payload := map[string]any{
			"kind": string(f.Kind), "subject": f.Subject, "severity": string(f.Severity),
			"remedy": string(f.Remedy), "facts": f.Facts, "opened": c.Opened,
		}
		if err := d.outbox.Publish(ctx, peer.EventFinding, payload); err != nil {
			slog.Warn("steward: finding not published to paired servers", "kind", f.Kind, "error", err)
		}
	}
}

// handleFindings answers GET /api/steward/findings: this machine's open
// findings, for its own footer.
func handleFindings(q *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		open, err := stewardStore{q: q}.OpenFindings(r.Context())
		if err != nil {
			httperror.RespondError(w, httperror.Internal("read findings", err))
			return
		}
		if open == nil {
			open = []steward.Finding{}
		}
		httperror.JSON(w, http.StatusOK, map[string]any{"findings": open})
	}
}
