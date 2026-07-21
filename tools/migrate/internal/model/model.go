// Package model contains the stable, destination-independent migration plan.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

const HashVersion = 1

type Plan struct {
	Workspace WorkspacePlan `json:"workspace"`
	Cards     []CardPlan    `json:"cards"`
	Runes     []RunePlan    `json:"runes"`
	Memories  []MemoryPlan  `json:"memories"`
	Notes     []NotePlan    `json:"notes"`
	Edges     []EdgePlan    `json:"edges"`
	Warnings  []Warning     `json:"warnings"`
}
type WorkspacePlan struct {
	IssuePrefix string       `json:"issue_prefix,omitempty"`
	Statuses    []StatusPlan `json:"statuses"`
	Types       []TypePlan   `json:"types"`
}
type StatusPlan struct{ Name, Category string }
type TypePlan struct{ Name string }
type CardPlan struct {
	ID, Title, Description, Reproduction, Design, Acceptance, Status, Type string
	Priority                                                               int32
	Assignee, Owner, Creator, ExternalRef, Worktree                        string
	Labels                                                                 []string
	CreatedAt, UpdatedAt, ClosedAt, DeferUntil                             *time.Time
	HashVersion                                                            int    `json:"hash_version"`
	Hash                                                                   string `json:"hash"`
}
type RunePlan struct {
	Key, Kind, Title, Body string
	Enabled, Protected     bool
	Metadata               map[string]string
	HashVersion            int
	Hash                   string
}
type MemoryPlan struct {
	Key, Body, Actor string
	CreatedAt        *time.Time
	HashVersion      int
	Hash             string
}
type NotePlan struct {
	CardID, SourceKey, SourceKind, SourceID, Author, Body string
	CreatedAt                                             *time.Time
	HashVersion                                           int
	Hash                                                  string
}
type EdgePlan struct {
	ParentID, ChildID string
	HashVersion       int
	Hash              string
}
type Warning struct {
	SourceID string   `json:"source_id"`
	Reasons  []string `json:"reasons"`
}

// Canonicalize sorts all externally visible collections and calculates the
// versioned hash for each importable value. Timestamps are projected as UTC RFC3339Nano.
func (p *Plan) Canonicalize() {
	for i := range p.Cards {
		sort.Strings(p.Cards[i].Labels)
		p.Cards[i].HashVersion = HashVersion
		p.Cards[i].Hash = hashCard(p.Cards[i])
	}
	for i := range p.Runes {
		p.Runes[i].HashVersion = HashVersion
		p.Runes[i].Hash = hashRune(p.Runes[i])
	}
	for i := range p.Memories {
		p.Memories[i].HashVersion = HashVersion
		p.Memories[i].Hash = hashMemory(p.Memories[i])
	}
	for i := range p.Notes {
		p.Notes[i].HashVersion = HashVersion
		p.Notes[i].Hash = hashNote(p.Notes[i])
	}
	for i := range p.Edges {
		p.Edges[i].HashVersion = HashVersion
		p.Edges[i].Hash = hashEdge(p.Edges[i])
	}
	sort.Slice(p.Cards, func(i, j int) bool { return p.Cards[i].ID < p.Cards[j].ID })
	sort.Slice(p.Runes, func(i, j int) bool { return p.Runes[i].Key < p.Runes[j].Key })
	sort.Slice(p.Memories, func(i, j int) bool { return p.Memories[i].Key < p.Memories[j].Key })
	sort.Slice(p.Notes, func(i, j int) bool { a, b := p.Notes[i], p.Notes[j]; return noteKey(a) < noteKey(b) })
	sort.Slice(p.Edges, func(i, j int) bool {
		if p.Edges[i].ParentID == p.Edges[j].ParentID {
			return p.Edges[i].ChildID < p.Edges[j].ChildID
		}
		return p.Edges[i].ParentID < p.Edges[j].ParentID
	})
	sort.Slice(p.Warnings, func(i, j int) bool { return p.Warnings[i].SourceID < p.Warnings[j].SourceID })
	for i := range p.Warnings {
		sort.Strings(p.Warnings[i].Reasons)
	}
	sort.Slice(p.Workspace.Statuses, func(i, j int) bool { return p.Workspace.Statuses[i].Name < p.Workspace.Statuses[j].Name })
	sort.Slice(p.Workspace.Types, func(i, j int) bool { return p.Workspace.Types[i].Name < p.Workspace.Types[j].Name })
	p.Workspace.Statuses = uniqueStatuses(p.Workspace.Statuses)
	p.Workspace.Types = uniqueTypes(p.Workspace.Types)
}
func noteKey(n NotePlan) string {
	t := ""
	if n.CreatedAt != nil {
		t = n.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	return t + "\x00" + n.SourceKind + "\x00" + n.SourceID + "\x00" + n.CardID
}
func uniqueStatuses(in []StatusPlan) []StatusPlan {
	out := in[:0]
	for i, v := range in {
		if i == 0 || v.Name != in[i-1].Name {
			out = append(out, v)
		}
	}
	return out
}
func uniqueTypes(in []TypePlan) []TypePlan {
	out := in[:0]
	for i, v := range in {
		if i == 0 || v.Name != in[i-1].Name {
			out = append(out, v)
		}
	}
	return out
}

// The hash projections below are intentionally separate from the plan structs.
// In particular, they omit Hash itself so calling Canonicalize repeatedly is
// idempotent, and a future presentation-only struct field cannot accidentally
// change the import identity.
type cardProjection struct {
	ID, Title, Description, Reproduction, Design, Acceptance, Status, Type string
	Priority                                                               int32
	Assignee, Owner, Creator, ExternalRef, Worktree                        string
	Labels                                                                 []string
	CreatedAt, UpdatedAt, ClosedAt, DeferUntil                             *string
	HashVersion                                                            int
}
type runeProjection struct {
	Key, Kind, Title, Body string
	Enabled, Protected     bool
	Metadata               map[string]string
	HashVersion            int
}
type memoryProjection struct {
	Key, Body, Actor string
	CreatedAt        *string
	HashVersion      int
}
type noteProjection struct {
	CardID, SourceKey, SourceKind, SourceID, Author, Body string
	CreatedAt                                             *string
	HashVersion                                           int
}
type edgeProjection struct {
	ParentID, ChildID string
	HashVersion       int
}

func hashCard(v CardPlan) string {
	return hash(cardProjection{v.ID, v.Title, v.Description, v.Reproduction, v.Design, v.Acceptance, v.Status, v.Type, v.Priority, v.Assignee, v.Owner, v.Creator, v.ExternalRef, v.Worktree, v.Labels, timestamp(v.CreatedAt), timestamp(v.UpdatedAt), timestamp(v.ClosedAt), timestamp(v.DeferUntil), v.HashVersion})
}
func hashRune(v RunePlan) string {
	return hash(runeProjection{v.Key, v.Kind, v.Title, v.Body, v.Enabled, v.Protected, v.Metadata, v.HashVersion})
}
func hashMemory(v MemoryPlan) string {
	return hash(memoryProjection{v.Key, v.Body, v.Actor, timestamp(v.CreatedAt), v.HashVersion})
}
func hashNote(v NotePlan) string {
	return hash(noteProjection{v.CardID, v.SourceKey, v.SourceKind, v.SourceID, v.Author, v.Body, timestamp(v.CreatedAt), v.HashVersion})
}
func hashEdge(v EdgePlan) string { return hash(edgeProjection{v.ParentID, v.ChildID, v.HashVersion}) }
func timestamp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
