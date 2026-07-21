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
		p.Cards[i].Hash = hash(p.Cards[i])
	}
	for i := range p.Runes {
		p.Runes[i].HashVersion = HashVersion
		p.Runes[i].Hash = hash(p.Runes[i])
	}
	for i := range p.Memories {
		p.Memories[i].HashVersion = HashVersion
		p.Memories[i].Hash = hash(p.Memories[i])
	}
	for i := range p.Notes {
		p.Notes[i].HashVersion = HashVersion
		p.Notes[i].Hash = hash(p.Notes[i])
	}
	for i := range p.Edges {
		p.Edges[i].HashVersion = HashVersion
		p.Edges[i].Hash = hash(p.Edges[i])
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
func hash(v any) string {
	b, _ := json.Marshal(normalized(v))
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func normalized(v any) any { b, _ := json.Marshal(v); var x any; _ = json.Unmarshal(b, &x); return x }
