package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/viq111/bdd"
)

// CardResult is the JSON/human result of a single-card read or mutation:
// create, show, update, note, close, reopen, defer, human. list/search/
// ready return the lighter CardSummaryResult instead, matching the
// library's full-fat-vs-projection split (GetCard vs ListCards/
// SearchCards/ReadyCards).
type CardResult struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Type         string          `json:"type"`
	Status       string          `json:"status"`
	Priority     int32           `json:"priority"`
	Worktree     string          `json:"worktree"`
	Assignee     string          `json:"assignee"`
	Owner        string          `json:"owner"`
	Labels       []string        `json:"labels"`
	Description  string          `json:"description"`
	Reproduction string          `json:"reproduction"`
	Design       string          `json:"design"`
	Acceptance   string          `json:"acceptance"`
	ExternalRef  string          `json:"external_ref"`
	Parents      []CardRefResult `json:"parents"`
	Children     []CardRefResult `json:"children"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
	StartedAt    *string         `json:"started_at"`
	ClosedAt     *string         `json:"closed_at"`
	DeferUntil   *string         `json:"defer_until"`
	Revision     int64           `json:"revision"`
}

// CardSummaryResult is one entry of the JSON/human result of `bdd list`,
// `bdd search`, and `bdd ready`: the core Card fields plus labels, without
// note/edge expansion.
type CardSummaryResult struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Type      string   `json:"type"`
	Status    string   `json:"status"`
	Priority  int32    `json:"priority"`
	Worktree  string   `json:"worktree"`
	Assignee  string   `json:"assignee"`
	Labels    []string `json:"labels"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// CardRefResult is a lightweight card reference, as returned by the
// parents/children commands and embedded in CardResult.Parents.
type CardRefResult struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// NoteResult is one entry of the JSON/human result of `bdd note` and the
// note log embedded in `bdd show`.
type NoteResult struct {
	ID        int64  `json:"id"`
	CardID    string `json:"card_id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// ShowResult is the JSON/human result of `bdd show`: the full card plus its
// note log.
type ShowResult struct {
	CardResult
	Notes []NoteResult `json:"notes"`
}

func formatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func formatNullableTimestamp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := formatTimestamp(*t)
	return &s
}

func nonNilLabels(labels []string) []string {
	if labels == nil {
		return []string{}
	}
	return labels
}

func toCardRefResult(r bdd.CardRef) CardRefResult {
	return CardRefResult{ID: r.ID, Title: r.Title, Type: string(r.Type), Status: string(r.Status)}
}

func toCardRefResults(refs []bdd.CardRef) []CardRefResult {
	out := make([]CardRefResult, len(refs))
	for i, r := range refs {
		out[i] = toCardRefResult(r)
	}
	return out
}

func toCardResult(c *bdd.Card) CardResult {
	return CardResult{
		ID:           c.ID,
		Title:        c.Title,
		Type:         string(c.Type),
		Status:       string(c.Status),
		Priority:     c.Priority,
		Worktree:     c.Worktree,
		Assignee:     c.Assignee,
		Owner:        c.Owner,
		Labels:       nonNilLabels(c.Labels),
		Description:  c.Description,
		Reproduction: c.Reproduction,
		Design:       c.Design,
		Acceptance:   c.Acceptance,
		ExternalRef:  c.ExternalRef,
		Parents:      toCardRefResults(c.Parents),
		Children:     toCardRefResults(c.Children),
		CreatedBy:    c.CreatedBy,
		CreatedAt:    formatTimestamp(c.CreatedAt),
		UpdatedAt:    formatTimestamp(c.UpdatedAt),
		StartedAt:    formatNullableTimestamp(c.StartedAt),
		ClosedAt:     formatNullableTimestamp(c.ClosedAt),
		DeferUntil:   formatNullableTimestamp(c.DeferUntil),
		Revision:     c.Revision,
	}
}

func toCardSummaryResult(c bdd.Card) CardSummaryResult {
	return CardSummaryResult{
		ID:        c.ID,
		Title:     c.Title,
		Type:      string(c.Type),
		Status:    string(c.Status),
		Priority:  c.Priority,
		Worktree:  c.Worktree,
		Assignee:  c.Assignee,
		Labels:    nonNilLabels(c.Labels),
		CreatedAt: formatTimestamp(c.CreatedAt),
		UpdatedAt: formatTimestamp(c.UpdatedAt),
	}
}

func toNoteResult(n bdd.Note) NoteResult {
	return NoteResult{ID: n.ID, CardID: n.CardID, Author: n.Author, Body: n.Body, CreatedAt: formatTimestamp(n.CreatedAt)}
}

func toNoteResults(notes []bdd.Note) []NoteResult {
	out := make([]NoteResult, len(notes))
	for i, n := range notes {
		out[i] = toNoteResult(n)
	}
	return out
}

// emitCard writes the result of a single-card command: JSON encodes the
// full object, --silent prints just the ID, and the human default prints a
// one-line confirmation naming the verb (the caller already knows the
// fields it just wrote).
func emitCard(s *Streams, cmdName string, r CardResult) int {
	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(r); err != nil {
			s.Errorf("bdd: %s: %v\n", cmdName, err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		fmt.Fprintln(s.Stdout, r.ID)
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, cardMutationLine(cmdName, r.ID))
	return ExitSuccess
}

// cardMutationLine picks the past-tense confirmation word for cmdName. create
// has no prior state to confirm, so it reads as `id: <id>` instead.
func cardMutationLine(cmdName, id string) string {
	switch cmdName {
	case "create":
		return fmt.Sprintf("id: %s", id)
	case "close":
		return fmt.Sprintf("closed %s", id)
	case "reopen":
		return fmt.Sprintf("reopened %s", id)
	case "defer":
		return fmt.Sprintf("deferred %s", id)
	case "human":
		return fmt.Sprintf("blocked-on-human %s", id)
	default:
		return fmt.Sprintf("updated %s", id)
	}
}

// emitShow writes the result of `bdd show`.
func emitShow(s *Streams, r ShowResult) int {
	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(r); err != nil {
			s.Errorf("bdd: show: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		fmt.Fprintln(s.Stdout, r.ID)
		return ExitSuccess
	}
	renderCard(s.Stdout, r.CardResult)
	if len(r.Notes) > 0 {
		fmt.Fprintln(s.Stdout)
		fmt.Fprintln(s.Stdout, "notes:")
		for _, n := range r.Notes {
			author := n.Author
			if author == "" {
				author = "-"
			}
			fmt.Fprintf(s.Stdout, "  [%d] %s %s\n", n.ID, n.CreatedAt, sanitizeForTerminal(author))
			for _, line := range strings.Split(sanitizeForTerminal(n.Body), "\n") {
				fmt.Fprintf(s.Stdout, "      %s\n", line)
			}
		}
	}
	return ExitSuccess
}

// renderCard writes r in the fixed human layout: identity, status, and
// priority first, then Worktree immediately after (plan section 9), then
// the remaining fields.
func renderCard(w io.Writer, r CardResult) {
	fmt.Fprintf(w, "id:           %s\n", r.ID)
	fmt.Fprintf(w, "title:        %s\n", sanitizeForTerminal(r.Title))
	fmt.Fprintf(w, "type:         %s\n", r.Type)
	fmt.Fprintf(w, "status:       %s\n", r.Status)
	fmt.Fprintf(w, "priority:     %d\n", r.Priority)
	fmt.Fprintf(w, "worktree:     %s\n", sanitizeForTerminal(formatWorktreeDisplay(r.Worktree)))
	fmt.Fprintf(w, "assignee:     %s\n", sanitizeForTerminal(emptyDash(r.Assignee)))
	fmt.Fprintf(w, "owner:        %s\n", sanitizeForTerminal(emptyDash(r.Owner)))
	fmt.Fprintf(w, "labels:       %s\n", sanitizeForTerminal(emptyDash(strings.Join(r.Labels, ", "))))
	if len(r.Parents) > 0 {
		parts := make([]string, len(r.Parents))
		for i, p := range r.Parents {
			parts[i] = fmt.Sprintf("%s (%s, %s)", p.ID, p.Type, p.Status)
		}
		fmt.Fprintf(w, "parents:      %s\n", strings.Join(parts, ", "))
	}
	if len(r.Children) > 0 {
		parts := make([]string, len(r.Children))
		for i, c := range r.Children {
			parts[i] = fmt.Sprintf("%s (%s, %s)", c.ID, c.Type, c.Status)
		}
		fmt.Fprintf(w, "children:     %s\n", strings.Join(parts, ", "))
	}
	if r.ExternalRef != "" {
		fmt.Fprintf(w, "external_ref: %s\n", sanitizeForTerminal(r.ExternalRef))
	}
	fmt.Fprintf(w, "created_by:   %s\n", sanitizeForTerminal(r.CreatedBy))
	fmt.Fprintf(w, "created_at:   %s\n", r.CreatedAt)
	fmt.Fprintf(w, "updated_at:   %s\n", r.UpdatedAt)
	if r.StartedAt != nil {
		fmt.Fprintf(w, "started_at:   %s\n", *r.StartedAt)
	}
	if r.ClosedAt != nil {
		fmt.Fprintf(w, "closed_at:    %s\n", *r.ClosedAt)
	}
	if r.DeferUntil != nil {
		fmt.Fprintf(w, "defer_until:  %s\n", *r.DeferUntil)
	}
	fmt.Fprintf(w, "revision:     %d\n", r.Revision)

	printTextField(w, "description", r.Description)
	printTextField(w, "reproduction", r.Reproduction)
	printTextField(w, "design", r.Design)
	printTextField(w, "acceptance", r.Acceptance)
}

func printTextField(w io.Writer, name, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(w, "\n%s:\n%s\n", name, sanitizeForTerminal(value))
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// formatWorktreeDisplay annotates wt with a "not present locally" note when
// it is set but does not exist on disk, without treating that as an error
// (plan section 9).
func formatWorktreeDisplay(wt string) string {
	if wt == "" {
		return "-"
	}
	if _, err := os.Stat(wt); err != nil {
		return wt + " (not present locally)"
	}
	return wt
}

// renderCardSummaryLine writes one compact line for a list/search/ready
// result entry.
func renderCardSummaryLine(w io.Writer, c CardSummaryResult) {
	fmt.Fprintf(w, "%s P%d - %s\n", c.ID, c.Priority, sanitizeForTerminal(c.Title))
}

// emitCardSummaries writes a list/search/ready result set.
func emitCardSummaries(s *Streams, cmdName string, cards []bdd.Card) int {
	if s.JSON {
		arr := NewJSONArray(s.Stdout)
		for _, c := range cards {
			if err := arr.WriteItem(toCardSummaryResult(c)); err != nil {
				s.Errorf("bdd: %s: %v\n", cmdName, err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: %s: %v\n", cmdName, err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		for _, c := range cards {
			fmt.Fprintln(s.Stdout, c.ID)
		}
		return ExitSuccess
	}
	for _, c := range cards {
		renderCardSummaryLine(s.Stdout, toCardSummaryResult(c))
	}
	return ExitSuccess
}

// parsePriority parses a --priority value as either a plain non-negative
// decimal or a "P<n>" token (case-insensitive).
func parsePriority(raw string) (int32, error) {
	s := raw
	if len(s) > 0 && (s[0] == 'P' || s[0] == 'p') {
		s = s[1:]
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("priority must be a non-negative integer or P<n>, got %q", raw)
	}
	return int32(n), nil
}

// parseTimeFlag parses a --until-style timestamp flag, accepting RFC3339
// with or without fractional seconds.
func parseTimeFlag(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("must be an RFC3339 timestamp, got %q", raw)
	}
	return t, nil
}
