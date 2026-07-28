package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/viq111/bdd"
)

// hookDepthEnv is the re-entrancy guard: a hook's own child process starts
// with this set, and any bdd invocation that observes it set fires no hooks
// of its own, so a hook that itself calls bdd cannot recurse.
const hookDepthEnv = "BDD_HOOK_DEPTH"

// hooksReentrant reports whether this process was itself spawned as a hook.
func hooksReentrant() bool {
	return os.Getenv(hookDepthEnv) != ""
}

// hookSource is everything a command needs to derive and fire hook events
// after a successful mutation, computed once per invocation.
type hookSource struct {
	hooks     []bdd.Hook
	workspace string
	database  string
	actor     string
}

// loadHookSource loads hooks.yaml and returns a non-nil hookSource only when
// hooks are active for this invocation AND hooks.yaml contains at least one
// hook whose event is one of candidates. Callers use a nil return to skip
// the db.GetCard pre-read entirely. The only database read loadHookSource
// itself performs is HooksEnabled's config lookup, which is unavoidable to
// answer the question at all.
func loadHookSource(ctx context.Context, db *bdd.DB, g GlobalFlags, s *Streams, actor string, candidates ...bdd.HookEvent) *hookSource {
	if HooksDisabled(g) || hooksReentrant() {
		return nil
	}

	enabled, err := db.HooksEnabled(ctx)
	if err != nil {
		s.Errorf("bdd: hooks: %v\n", err)
		return nil
	}
	if !enabled {
		return nil
	}

	hf, err := bdd.LoadHooksFile(db.HooksPath())
	if err != nil {
		s.Errorf("bdd: hooks: %v\n", err)
		return nil
	}
	if !hasAnyEvent(hf.Hooks, candidates) {
		return nil
	}

	hooksDir := filepath.Dir(db.HooksPath())
	workspace := filepath.Dir(hooksDir)
	return &hookSource{hooks: hf.Hooks, workspace: workspace, database: db.Path(), actor: actor}
}

func hasAnyEvent(hooks []bdd.Hook, candidates []bdd.HookEvent) bool {
	for _, h := range hooks {
		for _, c := range candidates {
			if h.Event == c {
				return true
			}
		}
	}
	return false
}

// fireStatusChange derives a status-change MatchEvent from card's
// post-mutation state and from/to, matches it against hs, and runs every
// matching hook in file order.
func (hs *hookSource) fireStatusChange(ctx context.Context, s *Streams, card *bdd.Card, from, to string) {
	ev := bdd.MatchEvent{
		Event:      bdd.HookEventStatusChange,
		FromStatus: from,
		ToStatus:   to,
		IssueType:  string(card.Type),
	}
	matches := bdd.MatchHooks(hs.hooks, ev)
	if len(matches) == 0 {
		return
	}

	payload := hs.payload(bdd.HookEventStatusChange, card)
	payload.StatusChange = &hookStatusChangePayload{From: from, To: to}
	env := hs.env(bdd.HookEventStatusChange, card.ID, from, to, nil, nil)

	hs.run(ctx, s, bdd.HookEventStatusChange, matches, payload, env)
}

// fireLabelChange derives a label-change MatchEvent from card's
// post-mutation state and the added/removed label deltas, matches it
// against hs, and runs every matching hook in file order.
func (hs *hookSource) fireLabelChange(ctx context.Context, s *Streams, card *bdd.Card, added, removed []string) {
	ev := bdd.MatchEvent{
		Event:     bdd.HookEventLabelChange,
		IssueType: string(card.Type),
		Added:     added,
		Removed:   removed,
	}
	matches := bdd.MatchHooks(hs.hooks, ev)
	if len(matches) == 0 {
		return
	}

	payload := hs.payload(bdd.HookEventLabelChange, card)
	payload.LabelChange = &hookLabelChangePayload{Added: added, Removed: removed}
	env := hs.env(bdd.HookEventLabelChange, card.ID, "", "", added, removed)

	hs.run(ctx, s, bdd.HookEventLabelChange, matches, payload, env)
}

func (hs *hookSource) run(ctx context.Context, s *Streams, event bdd.HookEvent, hooks []bdd.Hook, payload hookPayload, env []string) {
	data, err := json.Marshal(payload)
	if err != nil {
		s.Errorf("bdd: hooks: %v\n", err)
		return
	}
	for _, h := range hooks {
		runHook(ctx, h, event, data, env, hs.workspace, s)
	}
}

func (hs *hookSource) payload(event bdd.HookEvent, card *bdd.Card) hookPayload {
	return hookPayload{
		Version:   1,
		Event:     string(event),
		Workspace: hs.workspace,
		Database:  hs.database,
		Actor:     hs.actor,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Card: hookCardPayload{
			ID:       card.ID,
			Title:    card.Title,
			Type:     string(card.Type),
			Status:   string(card.Status),
			Priority: card.Priority,
			Assignee: card.Assignee,
			Labels:   nonNilLabels(card.Labels),
			Revision: card.Revision,
		},
	}
}

func (hs *hookSource) env(event bdd.HookEvent, cardID, from, to string, added, removed []string) []string {
	return []string{
		"BDD_HOOK_EVENT=" + string(event),
		"BDD_HOOK_CARD_ID=" + cardID,
		"BDD_HOOK_FROM_STATUS=" + from,
		"BDD_HOOK_TO_STATUS=" + to,
		"BDD_HOOK_LABELS_ADDED=" + strings.Join(added, ","),
		"BDD_HOOK_LABELS_REMOVED=" + strings.Join(removed, ","),
		"BDD_WORKSPACE=" + hs.workspace,
		"BDD_DB=" + hs.database,
	}
}

// runHook executes one hook's command with payload on stdin and extraEnv
// added to the process environment, plus the BDD_HOOK_DEPTH re-entrancy
// guard. Failure is advisory: a non-zero exit or timeout is reported to
// s.Stderr and otherwise ignored, never surfaced as bdd's own exit code.
func runHook(ctx context.Context, h bdd.Hook, event bdd.HookEvent, payload []byte, extraEnv []string, workspace string, s *Streams) {
	hctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()

	cmd := exec.CommandContext(hctx, h.Command[0], h.Command[1:]...)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Env = append(cmd.Env, hookDepthEnv+"=1")
	cmd.Stdin = bytes.NewReader(payload)

	if err := cmd.Run(); err != nil {
		s.Errorf("bdd: hook %s [%s]: %v\n", event, strings.Join(h.Command, " "), err)
	}
}

// fireStatusChangeIfChanged fires hs's status-change event for post using
// pre's status as "from", when hs and pre are non-nil and the status
// actually changed. It is a no-op otherwise, so callers can invoke it
// unconditionally after a successful mutation.
func fireStatusChangeIfChanged(ctx context.Context, s *Streams, hs *hookSource, pre, post *bdd.Card) {
	if hs == nil || pre == nil {
		return
	}
	if pre.Status == post.Status {
		return
	}
	hs.fireStatusChange(ctx, s, post, string(pre.Status), string(post.Status))
}

// labelDiff reports the labels added and removed going from pre to post.
// added and removed are always non-nil, even when empty, so callers that
// serialize them (e.g. as hook payload JSON) get "[]" rather than "null".
func labelDiff(pre, post []string) (added, removed []string) {
	added = []string{}
	removed = []string{}
	preSet := make(map[string]bool, len(pre))
	for _, l := range pre {
		preSet[l] = true
	}
	postSet := make(map[string]bool, len(post))
	for _, l := range post {
		postSet[l] = true
	}
	for _, l := range post {
		if !preSet[l] {
			added = append(added, l)
		}
	}
	for _, l := range pre {
		if !postSet[l] {
			removed = append(removed, l)
		}
	}
	return added, removed
}

// hookPayload is the stdin JSON contract documented in docs/hooks.md.
type hookPayload struct {
	Version      int                      `json:"version"`
	Event        string                   `json:"event"`
	Workspace    string                   `json:"workspace"`
	Database     string                   `json:"database"`
	Actor        string                   `json:"actor"`
	Timestamp    string                   `json:"timestamp"`
	Card         hookCardPayload          `json:"card"`
	StatusChange *hookStatusChangePayload `json:"status_change,omitempty"`
	LabelChange  *hookLabelChangePayload  `json:"label_change,omitempty"`
}

type hookCardPayload struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Type     string   `json:"type"`
	Status   string   `json:"status"`
	Priority int32    `json:"priority"`
	Assignee string   `json:"assignee"`
	Labels   []string `json:"labels"`
	Revision int64    `json:"revision"`
}

type hookStatusChangePayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type hookLabelChangePayload struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
}
