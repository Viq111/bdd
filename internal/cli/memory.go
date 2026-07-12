package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/viq111/bdd"
)

// MemoryResult is the JSON/human result of `bdd remember` and `bdd recall`,
// and (per entry) `bdd memories`.
type MemoryResult struct {
	Key       string `json:"key"`
	Body      string `json:"body"`
	CreatedBy string `json:"created_by"`
	UpdatedBy string `json:"updated_by"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Revision  int64  `json:"revision"`
}

func toMemoryResult(m *bdd.Memory) MemoryResult {
	return MemoryResult{
		Key:       m.Key,
		Body:      m.Body,
		CreatedBy: m.CreatedBy,
		UpdatedBy: m.UpdatedBy,
		CreatedAt: m.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt: m.UpdatedAt.Format(time.RFC3339Nano),
		Revision:  m.Revision,
	}
}

// runRemember implements `bdd remember [body] [--key <key>] [--stdin]`.
func runRemember(g GlobalFlags, args []string, s *Streams) int {
	var key, body string
	var haveBody, useStdin bool

	i := 0
	for i < len(args) {
		arg := args[i]
		name, inline, hasInline := cutFlagValue(arg)

		switch name {
		case "--key":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: remember: %v\n", err)
				return ExitUsage
			}
			key = val
			i += consumed
			continue
		case "--stdin":
			useStdin = true
			i++
			continue
		}

		if strings.HasPrefix(arg, "-") {
			s.Errorf("bdd: remember: unknown flag %q\n", arg)
			return ExitUsage
		}
		if haveBody {
			s.Errorf("bdd: remember: unexpected argument %q\n", arg)
			return ExitUsage
		}
		body = arg
		haveBody = true
		i++
	}

	if useStdin && haveBody {
		s.Errorf("bdd: remember: cannot combine a positional body with --stdin\n")
		return ExitUsage
	}
	if useStdin {
		data, err := io.ReadAll(s.Stdin)
		if err != nil {
			s.Errorf("bdd: remember: reading stdin: %v\n", err)
			return ExitOther
		}
		body = string(data)
	} else if !haveBody {
		s.Errorf("bdd: remember: a positional body or --stdin is required\n")
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "remember", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	m, err := db.Remember(ctx, bdd.Remember{Key: key, Body: body, Actor: actor})
	if err != nil {
		s.Errorf("bdd: remember: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(toMemoryResult(m)); err != nil {
			s.Errorf("bdd: remember: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, m.Key)
	return ExitSuccess
}

// runMemories implements `bdd memories [query]`.
func runMemories(g GlobalFlags, args []string, s *Streams) int {
	var query string
	if len(args) == 1 {
		query = args[0]
	} else if len(args) > 1 {
		s.Errorf("bdd: memories: unexpected argument %q\n", args[1])
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "memories", s)
	if db == nil {
		return code
	}
	defer db.Close()

	memories, err := db.Memories(ctx, bdd.MemoryQuery{Query: query})
	if err != nil {
		s.Errorf("bdd: memories: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		arr := NewJSONArray(s.Stdout)
		for _, m := range memories {
			if err := arr.WriteItem(toMemoryResult(&m)); err != nil {
				s.Errorf("bdd: memories: %v\n", err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: memories: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}

	for _, m := range memories {
		fmt.Fprintf(s.Stdout, "%s\t%s\n", m.Key, firstLine(m.Body))
	}
	return ExitSuccess
}

// runRecall implements `bdd recall <key>`.
func runRecall(g GlobalFlags, args []string, s *Streams) int {
	if len(args) != 1 {
		s.Errorf("bdd: recall: expected exactly one key argument\n")
		return ExitUsage
	}
	key := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "recall", s)
	if db == nil {
		return code
	}
	defer db.Close()

	m, err := db.Recall(ctx, key)
	if err != nil {
		s.Errorf("bdd: recall: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(toMemoryResult(m)); err != nil {
			s.Errorf("bdd: recall: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, m.Body)
	return ExitSuccess
}

// runForget implements `bdd forget <key>`.
func runForget(g GlobalFlags, args []string, s *Streams) int {
	if len(args) != 1 {
		s.Errorf("bdd: forget: expected exactly one key argument\n")
		return ExitUsage
	}
	key := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "forget", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	if err := db.Forget(ctx, key, actor); err != nil {
		s.Errorf("bdd: forget: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(KeyResult{Key: key}); err != nil {
			s.Errorf("bdd: forget: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, key)
	return ExitSuccess
}

// firstLine returns the first line of s, trimmed of surrounding whitespace,
// for compact tabular display.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
