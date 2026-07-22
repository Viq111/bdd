package bddsink

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/viq111/bdd"
	"github.com/viq111/bdd/internal/sqlite"
	"github.com/viq111/bdd/tools/migrate/internal/model"
)

func verifyTx(ctx context.Context, tx *sql.Tx, plan model.Plan) error {
	for _, v := range plan.Cards {
		if err := verifyCardTx(ctx, tx, v); err != nil {
			return err
		}
	}
	for _, v := range plan.Notes {
		if err := verifyNoteTx(ctx, tx, v); err != nil {
			return err
		}
	}
	for _, v := range plan.Runes {
		if err := verifyRuneTx(ctx, tx, v); err != nil {
			return err
		}
	}
	for _, v := range plan.Memories {
		if err := verifyMemoryTx(ctx, tx, v); err != nil {
			return err
		}
	}
	if err := verifyEdgesTx(ctx, tx, plan.Edges); err != nil {
		return err
	}
	if err := verifyAcyclic(ctx, tx); err != nil {
		return err
	}
	return pragmaEmpty(ctx, tx, "PRAGMA foreign_key_check")
}
func verifyCardTx(ctx context.Context, tx *sql.Tx, v model.CardPlan) error {
	var got struct {
		ID, Title, Worktree, Description, Reproduction, Design, Acceptance, Status string
		Priority                                                                   int32
		Type, ExternalRef, Assignee, Creator                                       string
		Created, Updated                                                           string
		Closed, Defer                                                              sql.NullString
	}
	err := tx.QueryRowContext(ctx, `SELECT id,title,worktree,description,reproduction,design,acceptance,status,priority,card_type,external_ref,assignee,created_by,created_at,updated_at,closed_at,defer_until FROM cards WHERE id=?`, v.ID).Scan(&got.ID, &got.Title, &got.Worktree, &got.Description, &got.Reproduction, &got.Design, &got.Acceptance, &got.Status, &got.Priority, &got.Type, &got.ExternalRef, &got.Assignee, &got.Creator, &got.Created, &got.Updated, &got.Closed, &got.Defer)
	if err != nil {
		return fmt.Errorf("bdd migration sink: card %q: %w", v.ID, err)
	}
	if got.ID != v.ID || got.Title != v.Title || got.Worktree != v.Worktree || got.Description != v.Description || got.Reproduction != v.Reproduction || got.Design != v.Design || got.Acceptance != v.Acceptance || got.Status != v.Status || got.Priority != v.Priority || got.Type != v.Type || got.ExternalRef != v.ExternalRef || got.Assignee != v.Assignee || got.Creator != v.Creator || !equalOptionalTime(got.Created, v.CreatedAt) || !equalOptionalTime(got.Updated, v.UpdatedAt) || !equalNullable(got.Closed, v.ClosedAt) || !equalNullable(got.Defer, v.DeferUntil) {
		return fmt.Errorf("bdd migration sink: card projection mismatch for %q", v.ID)
	}
	return equalStringSet(ctx, tx, `SELECT label FROM labels WHERE card_id=?`, v.ID, v.Labels, "labels")
}
func verifyNoteTx(ctx context.Context, tx *sql.Tx, v model.NotePlan) error {
	var payload string
	err := tx.QueryRowContext(ctx, `SELECT payload_json FROM events WHERE subject_kind='note' AND subject_key=? AND action='migration.note'`, v.SourceKey).Scan(&payload)
	if err != nil {
		return fmt.Errorf("bdd migration sink: note provenance %q: %w", v.SourceKey, err)
	}
	var event struct {
		SourceSystem, SourceKind, SourceID, SourceKey, Hash string `json:"-"`
		NoteID                                              int64  `json:"note_id"`
		HashVersion                                         int    `json:"hash_version"`
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return err
	}
	if raw["source_system"] != "beads" || raw["source_kind"] != v.SourceKind || raw["source_id"] != v.SourceID || raw["source_key"] != v.SourceKey || raw["hash"] != v.Hash {
		return fmt.Errorf("bdd migration sink: note provenance mismatch for %q", v.SourceKey)
	}
	n, ok := raw["note_id"].(float64)
	if !ok {
		return fmt.Errorf("bdd migration sink: note %q lacks note_id provenance", v.SourceKey)
	}
	event.NoteID = int64(n)
	var card, body, created string
	var author sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT card_id,author,body,created_at FROM notes WHERE id=?`, event.NoteID).Scan(&card, &author, &body, &created)
	if err != nil {
		return err
	}
	if card != v.CardID || author.String != v.Author || body != v.Body || !equalTimeText(created, v.CreatedAt) {
		return fmt.Errorf("bdd migration sink: note projection mismatch for %q", v.SourceKey)
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE subject_kind='note' AND subject_key=? AND action='migration.note'`, v.SourceKey).Scan(&count); err != nil || count != 1 {
		return fmt.Errorf("bdd migration sink: note key %q is not unique", v.SourceKey)
	}
	return nil
}
func verifyRuneTx(ctx context.Context, tx *sql.Tx, v model.RunePlan) error {
	var kind, title, body, metadata string
	var enabled, protected int
	if err := tx.QueryRowContext(ctx, `SELECT kind,title,body,metadata_json,enabled,protected FROM runes WHERE key=?`, v.Key).Scan(&kind, &title, &body, &metadata, &enabled, &protected); err != nil {
		return err
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(metadata), &got); err != nil {
		return err
	}
	if kind != v.Kind || title != v.Title || body != v.Body || enabled != boolInt(v.Enabled) || protected != boolInt(v.Protected) || !reflect.DeepEqual(got, v.Metadata) || got["legacy_bd_id"] == "" {
		return fmt.Errorf("bdd migration sink: rune projection mismatch for %q", v.Key)
	}
	return nil
}
func verifyMemoryTx(ctx context.Context, tx *sql.Tx, v model.MemoryPlan) error {
	var body string
	var createdBy sql.NullString
	var createdAt string
	if err := tx.QueryRowContext(ctx, `SELECT body,created_by,created_at FROM memories WHERE key=?`, v.Key).Scan(&body, &createdBy, &createdAt); err != nil {
		return err
	}
	if body != v.Body || createdBy.String != v.Actor || !equalOptionalTime(createdAt, v.CreatedAt) {
		return fmt.Errorf("bdd migration sink: memory projection mismatch for %q", v.Key)
	}
	return nil
}
func verifyEdgesTx(ctx context.Context, tx *sql.Tx, edges []model.EdgePlan) error {
	want := make([]string, len(edges))
	for i, e := range edges {
		want[i] = e.ParentID + "\x00" + e.ChildID
	}
	sort.Strings(want)
	rows, err := tx.QueryContext(ctx, `SELECT parent_id,child_id FROM card_edges WHERE parent_id IN (SELECT subject_key FROM events WHERE subject_kind='card' AND action='migration.card') AND child_id IN (SELECT subject_key FROM events WHERE subject_kind='card' AND action='migration.card') ORDER BY parent_id,child_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var p, c string
		if err := rows.Scan(&p, &c); err != nil {
			return err
		}
		got = append(got, p+"\x00"+c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(got) != len(want) || (len(got) != 0 && !reflect.DeepEqual(got, want)) {
		return fmt.Errorf("bdd migration sink: edge set mismatch: got %v want %v", got, want)
	}
	return nil
}
func verifyAcyclic(ctx context.Context, tx *sql.Tx) error {
	var cycle int
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE walk(root,node,path) AS (SELECT parent_id,child_id,parent_id||char(0)||child_id FROM card_edges UNION ALL SELECT walk.root,e.child_id,walk.path||char(0)||e.child_id FROM walk JOIN card_edges e ON e.parent_id=walk.node WHERE instr(walk.path,char(0)||e.child_id)=0) SELECT 1 FROM walk WHERE root=node LIMIT 1`).Scan(&cycle)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("bdd migration sink: edge graph contains a cycle")
}
func equalStringSet(ctx context.Context, tx *sql.Tx, q, id string, want []string, what string) error {
	rows, err := tx.QueryContext(ctx, q, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return err
		}
		got = append(got, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	copyWant := append([]string(nil), want...)
	sort.Strings(copyWant)
	if !reflect.DeepEqual(got, copyWant) {
		return fmt.Errorf("bdd migration sink: %s mismatch for %q: got %v want %v", what, id, got, copyWant)
	}
	return nil
}
func equalNullable(got sql.NullString, want *time.Time) bool {
	if want == nil {
		return !got.Valid
	}
	return got.Valid && equalTimeText(got.String, want)
}
func equalTimeText(got string, want *time.Time) bool {
	if want == nil {
		return got != ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, got)
	return err == nil && parsed.Equal(want.UTC())
}
func equalOptionalTime(got string, want *time.Time) bool {
	return want == nil || equalTimeText(got, want)
}
func pragmaEmpty(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, statement string) error {
	rows, err := q.QueryContext(ctx, statement)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("bdd migration sink: %s returned rows", statement)
	}
	return rows.Err()
}

func verifyPublic(ctx context.Context, path string, plan model.Plan) error {
	db, err := bdd.Open(ctx, bdd.OpenOptions{Path: path})
	if err != nil {
		return err
	}
	defer db.Close()
	for _, v := range plan.Cards {
		c, e := db.GetCard(ctx, v.ID)
		if e != nil {
			return e
		}
		if c.ID != v.ID || c.Title != v.Title || string(c.Status) != v.Status || string(c.Type) != v.Type || c.Priority != v.Priority || c.Description != v.Description || c.Reproduction != v.Reproduction || c.Design != v.Design || c.Acceptance != v.Acceptance || c.ExternalRef != v.ExternalRef || c.Worktree != v.Worktree || c.Assignee != v.Assignee || c.CreatedBy != v.Creator || !equalStringSlices(c.Labels, v.Labels) || !equalOptionalTime(c.CreatedAt.Format(time.RFC3339Nano), v.CreatedAt) || !equalOptionalTime(c.UpdatedAt.Format(time.RFC3339Nano), v.UpdatedAt) || !equalPublicNullable(c.ClosedAt, v.ClosedAt) || !equalPublicNullable(c.DeferUntil, v.DeferUntil) {
			return fmt.Errorf("bdd migration sink: public card projection mismatch for %q", v.ID)
		}
		if err := verifyPublicLinks(c, v.ID, plan.Edges); err != nil {
			return err
		}
		expected := expectedReady(v, plan)
		reasons, e := db.ExplainReady(ctx, v.ID)
		if e != nil {
			return e
		}
		if (len(reasons) == 0) != expected {
			return fmt.Errorf("bdd migration sink: readiness mismatch for %q", v.ID)
		}
	}
	for _, v := range plan.Notes {
		if err := verifyPublicNote(ctx, db, v); err != nil {
			return err
		}
	}
	for _, v := range plan.Runes {
		r, e := db.GetRune(ctx, v.Key)
		if e != nil {
			return e
		}
		var metadata map[string]string
		if e = json.Unmarshal([]byte(r.Metadata), &metadata); e != nil {
			return e
		}
		if r.Key != v.Key || r.Kind != v.Kind || r.Title != v.Title || r.Body != v.Body || r.Enabled != v.Enabled || r.Protected != v.Protected || !reflect.DeepEqual(metadata, v.Metadata) {
			return fmt.Errorf("bdd migration sink: public rune projection mismatch for %q", v.Key)
		}
	}
	for _, v := range plan.Memories {
		m, e := db.Recall(ctx, v.Key)
		if e != nil {
			return e
		}
		if m.Body != v.Body || m.CreatedBy != v.Actor || !equalOptionalTime(m.CreatedAt.Format(time.RFC3339Nano), v.CreatedAt) {
			return fmt.Errorf("bdd migration sink: public memory projection mismatch for %q", v.Key)
		}
	}
	raw, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot, SkipJournalMode: true})
	if err != nil {
		return err
	}
	defer raw.Close()
	var result string
	if err = raw.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("bdd migration sink: integrity_check = %s", result)
	}
	return pragmaEmpty(ctx, raw, "PRAGMA foreign_key_check")
}
func expectedReady(card model.CardPlan, plan model.Plan) bool {
	if !isActive(card.Status, plan) || card.Assignee != "" {
		return false
	}
	for _, l := range card.Labels {
		if l == bdd.HumanLabel {
			return false
		}
	}
	for _, e := range plan.Edges {
		if e.ChildID == card.ID {
			for _, p := range plan.Cards {
				if p.ID == e.ParentID && !isDone(p.Status, plan) {
					return false
				}
			}
		}
	}
	return true
}
func isActive(status string, plan model.Plan) bool {
	if status == "open" {
		return true
	}
	for _, s := range plan.Workspace.Statuses {
		if s.Name == status {
			return s.Category == "active"
		}
	}
	return false
}
func isDone(status string, plan model.Plan) bool {
	if status == "closed" {
		return true
	}
	for _, s := range plan.Workspace.Statuses {
		if s.Name == status {
			return s.Category == "done"
		}
	}
	return false
}
func equalStringSlices(a, b []string) bool {
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}
func equalPublicNullable(got, want *time.Time) bool {
	if want == nil {
		return got == nil
	}
	return got != nil && got.Equal(want.UTC())
}
func verifyPublicLinks(c *bdd.Card, id string, edges []model.EdgePlan) error {
	var parents, children []string
	for _, e := range edges {
		if e.ChildID == id {
			parents = append(parents, e.ParentID)
		}
		if e.ParentID == id {
			children = append(children, e.ChildID)
		}
	}
	gotParents := make([]string, len(c.Parents))
	for i, v := range c.Parents {
		gotParents[i] = v.ID
	}
	gotChildren := make([]string, len(c.Children))
	for i, v := range c.Children {
		gotChildren[i] = v.ID
	}
	if !equalStringSlices(gotParents, parents) || !equalStringSlices(gotChildren, children) {
		return fmt.Errorf("bdd migration sink: public edge projection mismatch for %q", id)
	}
	return nil
}
func verifyPublicNote(ctx context.Context, db *bdd.DB, v model.NotePlan) error {
	notes, err := db.Notes(ctx, v.CardID)
	if err != nil {
		return err
	}
	for _, n := range notes {
		if n.Author == v.Author && n.Body == v.Body && (v.CreatedAt == nil || n.CreatedAt.Equal(v.CreatedAt.UTC())) {
			return nil
		}
	}
	return fmt.Errorf("bdd migration sink: public note projection mismatch for %q", v.SourceKey)
}
