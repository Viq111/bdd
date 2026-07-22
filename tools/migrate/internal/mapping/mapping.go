package mapping

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/viq111/bdd/tools/migrate/internal/model"
	"github.com/viq111/bdd/tools/migrate/internal/sourcebd"
)

type Config struct {
	IssuePrefix            string
	StatusCategories       map[string]string
	CustomTypes            map[string]bool
	LegacyStatusCategories map[string]string
}

var builtInStatuses = map[string]bool{"open": true, "in_progress": true, "awaiting_review": true, "blocked": true, "deferred": true, "closed": true}
var builtInTypes = map[string]bool{"task": true, "bug": true, "feature": true, "epic": true, "chore": true, "decision": true}
var skippedTypes = map[string]bool{"gate": true, "molecule": true, "template": true, "event": true, "infrastructure": true}

func Map(records []sourcebd.Record, cfg Config) (model.Plan, error) {
	if cfg.StatusCategories == nil {
		cfg.StatusCategories = map[string]string{}
	}
	if cfg.CustomTypes == nil {
		cfg.CustomTypes = map[string]bool{}
	}
	p := model.Plan{Workspace: model.WorkspacePlan{IssuePrefix: cfg.IssuePrefix}}
	warns := map[string][]string{}
	add := func(id, reason string) {
		if id == "" {
			id = "workspace"
		}
		warns[id] = append(warns[id], reason)
	}
	// Fixture exports may carry definition records. They are equivalent to the
	// supplementary read-only configuration reads and make the pure mapper easy
	// to exercise without a runner. Every such record gets an explicit plan
	// value or warning; definitions must not disappear merely because no card
	// happens to use them in the same export.
	for _, r := range records {
		infra, ok := r.(sourcebd.Infrastructure)
		if !ok {
			continue
		}
		raw := infra.RawJSON()
		kind := infra.Type()
		if kind == "infrastructure" {
			kind = rawString(raw, "kind", "")
		}
		name := rawString(raw, "name", "")
		switch kind {
		case "status", "custom_status":
			category := rawString(raw, "category", "")
			if category == "" {
				category = cfg.StatusCategories[name]
			}
			if category == "" {
				category = cfg.LegacyStatusCategories[name]
			}
			if name == "" || category == "" {
				add("workspace", "status definition "+fmt.Sprintf("%q", name)+" has no category; skipped record")
				continue
			}
			cfg.StatusCategories[name] = category
			if !builtInStatuses[name] {
				p.Workspace.Statuses = append(p.Workspace.Statuses, model.StatusPlan{Name: name, Category: category})
			} else {
				add("workspace", "built-in status definition "+fmt.Sprintf("%q", name)+" requires no workspace plan; skipped record")
			}
		case "type", "custom_type":
			if name == "" {
				add("workspace", "type definition has no name; skipped record")
				continue
			}
			cfg.CustomTypes[name] = true
			if !builtInTypes[name] && name != "role" {
				p.Workspace.Types = append(p.Workspace.Types, model.TypePlan{Name: name})
			} else {
				add("workspace", "built-in type definition "+fmt.Sprintf("%q", name)+" requires no workspace plan; skipped record")
			}
		default:
			add("workspace", "unsupported infrastructure record; skipped record")
		}
	}
	cards := map[string]bool{}
	roles := map[string]string{}
	for _, r := range records {
		switch v := r.(type) {
		case sourcebd.Memory:
			if strings.TrimSpace(v.Key) == "" {
				add("workspace", "memory record has no key; skipped record")
				continue
			}
			actor := rawString(v.RawJSON(), "actor", "")
			if actor == "" {
				actor = "bdd-migration"
			}
			p.Memories = append(p.Memories, model.MemoryPlan{Key: v.Key, Body: v.Value, Actor: actor, CreatedAt: rawTime(v.RawJSON(), "created_at")})
		case sourcebd.Issue:
			id := v.ID
			if !safeID(id) {
				add(id, "invalid bdd card ID; skipped record")
				continue
			}
			typ := v.IssueType
			if typ == "role" {
				key, ok := roleKey(v.Title)
				if !ok {
					add(id, "role title does not produce a valid rune key; skipped record")
					continue
				}
				if prior := roles[key]; prior != "" {
					add(id, "duplicate rune key "+fmt.Sprintf("%q", key)+"; skipped record")
					continue
				}
				roles[key] = id
				p.Runes = append(p.Runes, model.RunePlan{Key: key, Kind: "role", Title: v.Title, Body: v.Description, Enabled: v.Status != "closed" && v.Status != "done", Protected: true, Metadata: map[string]string{"legacy_system": "beads", "legacy_bd_id": id, "legacy_status": v.Status}})
				// Notes and edges cannot be attached to a destination rune.  Do not
				// lose that fact merely because the role itself imported successfully.
				if v.Notes != "" {
					add(id, "skipped role-attached notes because role is imported as a rune")
				}
				if len(v.Comments) != 0 {
					add(id, "skipped role-attached comments because role is imported as a rune")
				}
				continue
			}
			if skippedTypes[typ] {
				add(id, "unsupported issue type "+fmt.Sprintf("%q", typ)+"; skipped record")
				continue
			}
			if !builtInTypes[typ] && !cfg.CustomTypes[typ] {
				add(id, "unsupported issue type "+fmt.Sprintf("%q", typ)+"; skipped record")
				continue
			}
			category := cfg.StatusCategories[v.Status]
			if category == "" {
				category = cfg.LegacyStatusCategories[v.Status]
			}
			if !builtInStatuses[v.Status] && category == "" {
				add(id, "status "+fmt.Sprintf("%q", v.Status)+" has no category; skipped record")
				continue
			}
			if !builtInStatuses[v.Status] {
				p.Workspace.Statuses = append(p.Workspace.Statuses, model.StatusPlan{Name: v.Status, Category: category})
			}
			if !builtInTypes[typ] {
				p.Workspace.Types = append(p.Workspace.Types, model.TypePlan{Name: typ})
			}
			priority := rawInt(v.RawJSON(), "priority")
			assignee := rawString(v.RawJSON(), "assignee", "")
			if assignee == "unclaimed" {
				assignee = ""
			}
			worktree := metadataString(v.RawJSON(), "worktree")
			c := model.CardPlan{ID: id, Title: v.Title, Description: v.Description, Design: v.Design, Acceptance: v.AcceptanceCriteria, Status: v.Status, Type: typ, Priority: int32(priority), Assignee: assignee, Owner: rawString(v.RawJSON(), "owner", ""), Creator: rawString(v.RawJSON(), "created_by", ""), ExternalRef: rawString(v.RawJSON(), "external_ref", ""), Worktree: worktree, Labels: append([]string(nil), v.Labels...), CreatedAt: rawTime(v.RawJSON(), "created_at"), UpdatedAt: rawTime(v.RawJSON(), "updated_at"), ClosedAt: rawTime(v.RawJSON(), "closed_at"), DeferUntil: rawTime(v.RawJSON(), "defer_until")}
			if typ == "bug" {
				c.Reproduction = extractReproduction(c.Description)
			}
			p.Cards = append(p.Cards, c)
			cards[id] = true
			if v.Notes != "" {
				p.Notes = append(p.Notes, model.NotePlan{CardID: id, SourceKey: id + "/notes/" + digest(v.Notes), SourceKind: "notes", SourceID: digest(v.Notes), Body: v.Notes})
			}
			seenAnonymous := map[string]bool{}
			for _, comment := range v.Comments {
				created := rawTime(comment.Raw, "created_at")
				author := rawString(comment.Raw, "author", "")
				sid := comment.ID
				if sid == "" {
					sid = digest(author + "\x00" + comment.Body + "\x00" + timeText(created))
				}
				unique := author + "\x00" + comment.Body + "\x00" + timeText(created)
				if comment.ID == "" && seenAnonymous[unique] {
					add(id, "ambiguous identical comment; collapsed duplicate")
					continue
				}
				if comment.ID == "" {
					seenAnonymous[unique] = true
				}
				p.Notes = append(p.Notes, model.NotePlan{CardID: id, SourceKey: id + "/comment/" + sid, SourceKind: "comment", SourceID: sid, Author: author, Body: comment.Body, CreatedAt: created})
			}
		case sourcebd.RawRecord:
			add(rawString(v.RawJSON(), "id", "workspace"), "unsupported export record; skipped record")
		}
	}
	for _, r := range records {
		v, ok := r.(sourcebd.Issue)
		if !ok {
			continue
		}
		if !cards[v.ID] {
			// A role is a valid imported source record, but it has no destination
			// card to which a dependency can be attached.
			if _, role := rolesForID(roles, v.ID); role {
				for _, d := range v.Dependencies {
					if d.Kind != "blocks" {
						add(v.ID, "skipped dependency kind "+fmt.Sprintf("%q", d.Kind)+" to "+d.IssueID)
					} else {
						add(v.ID, "skipped dependency to "+d.IssueID+" because role is imported as a rune")
					}
				}
			}
			continue
		}
		for _, d := range v.Dependencies {
			if d.Kind != "blocks" {
				add(v.ID, "skipped dependency kind "+fmt.Sprintf("%q", d.Kind)+" to "+d.IssueID)
				continue
			}
			if !cards[d.IssueID] {
				add(v.ID, "skipped dependency to "+d.IssueID+" because endpoint was not imported")
				continue
			}
			if v.ID == d.IssueID {
				return model.Plan{}, fmt.Errorf("dependency graph contains self-edge %s", v.ID)
			}
			p.Edges = append(p.Edges, model.EdgePlan{ParentID: d.IssueID, ChildID: v.ID})
		}
	}
	if cyclic(p.Edges) {
		return model.Plan{}, fmt.Errorf("dependency graph contains a cycle")
	}
	for id, reasons := range warns {
		p.Warnings = append(p.Warnings, model.Warning{SourceID: id, Reasons: reasons})
	}
	p.Canonicalize()
	return p, nil
}
func rolesForID(roles map[string]string, id string) (string, bool) {
	for key, roleID := range roles {
		if roleID == id {
			return key, true
		}
	}
	return "", false
}
func safeID(s string) bool {
	return s != "" && utf8.ValidString(s) && len(s) <= 255 && !strings.ContainsAny(s, "\x00\r\n")
}

var roleRE = regexp.MustCompile(`(?i)^\s*\[role\]\s*`)
var separatorRE = regexp.MustCompile(`\s+[-–—]\s+`)
var headingRE = regexp.MustCompile(`(?im)^(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)

func roleKey(title string) (string, bool) {
	tag := roleRE.ReplaceAllString(title, "")
	tag = separatorRE.Split(tag, 2)[0]
	tag = strings.Join(strings.Fields(tag), "-")
	tag = strings.ToLower(tag)
	return "role/" + tag, tag != "" && regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(tag)
}
func extractReproduction(s string) string {
	matches := headingRE.FindAllStringSubmatchIndex(s, -1)
	for i, m := range matches {
		name := strings.ToLower(strings.TrimSpace(s[m[4]:m[5]]))
		name = strings.Join(strings.Fields(name), " ")
		if name != "reproduction" && name != "steps to reproduce" {
			continue
		}
		level := m[3] - m[2]
		start := m[1]
		end := len(s)
		for _, next := range matches[i+1:] {
			if next[3]-next[2] <= level {
				end = next[0]
				break
			}
		}
		return strings.Trim(s[start:end], "\r\n")
	}
	return ""
}
func rawString(raw map[string]json.RawMessage, key, def string) string {
	var s string
	if json.Unmarshal(raw[key], &s) == nil {
		return s
	}
	return def
}
func metadataString(raw map[string]json.RawMessage, key string) string {
	var metadata map[string]json.RawMessage
	if json.Unmarshal(raw["metadata"], &metadata) != nil {
		return ""
	}
	return rawString(metadata, key, "")
}
func rawInt(raw map[string]json.RawMessage, key string) int {
	var n int
	if json.Unmarshal(raw[key], &n) == nil {
		return n
	}
	return 0
}
func rawTime(raw map[string]json.RawMessage, key string) *time.Time {
	var s string
	if len(raw[key]) == 0 || string(raw[key]) == "null" || json.Unmarshal(raw[key], &s) != nil || s == "" {
		return nil
	}
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return nil
	}
	t = t.UTC()
	return &t
}
func digest(s string) string { x := sha256.Sum256([]byte(s)); return hex.EncodeToString(x[:]) }
func timeText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
func cyclic(edges []model.EdgePlan) bool {
	g := map[string][]string{}
	for _, e := range edges {
		g[e.ParentID] = append(g[e.ParentID], e.ChildID)
	}
	state := map[string]int{}
	var visit func(string) bool
	visit = func(n string) bool {
		if state[n] == 1 {
			return true
		}
		if state[n] == 2 {
			return false
		}
		state[n] = 1
		for _, x := range g[n] {
			if visit(x) {
				return true
			}
		}
		state[n] = 2
		return false
	}
	keys := make([]string, 0, len(g))
	for k := range g {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if visit(k) {
			return true
		}
	}
	return false
}
