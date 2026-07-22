package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/viq111/bdd"
)

// runMemory implements the `bdd memory` group's fallback dispatch for a
// missing or unknown subcommand; matched subcommands are routed directly
// to their leaf by cobra and never reach here.
func runMemory(g GlobalFlags, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: memory: missing subcommand (set, get, list, search, remove)\n")
		return ExitUsage
	}
	if strings.HasPrefix(args[0], "-") {
		return reportUnknownArg(s, "memory", args[0])
	}
	sub, rest := args[0], args[1:]

	switch sub {
	case "set":
		return runMemorySet(g, rest, s)
	case "get":
		return runMemoryGet(g, rest, s)
	case "list":
		return runMemoryList(g, rest, s)
	case "search":
		return runMemorySearch(g, rest, s)
	case "remove":
		return runMemoryRemove(g, rest, s)
	default:
		s.Errorf("bdd: memory: unknown subcommand %q\n", sub)
		return ExitUsage
	}
}

// MemoryResult is the JSON/human result of `bdd memory set` and
// `bdd memory get`, and (per entry) `bdd memory list`/`bdd memory search`.
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

// runMemorySet implements `bdd memory set [body] [--key <key>] [--stdin]`.
func runMemorySet(g GlobalFlags, args []string, s *Streams) int {
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
				s.Errorf("bdd: memory set: %v\n", err)
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
			s.Errorf("bdd: memory set: unknown flag %q\n", arg)
			return ExitUsage
		}
		if haveBody {
			s.Errorf("bdd: memory set: unexpected argument %q\n", arg)
			return ExitUsage
		}
		body = arg
		haveBody = true
		i++
	}

	if useStdin && haveBody {
		s.Errorf("bdd: memory set: cannot combine a positional body with --stdin\n")
		return ExitUsage
	}
	if useStdin {
		data, err := io.ReadAll(s.Stdin)
		if err != nil {
			s.Errorf("bdd: memory set: reading stdin: %v\n", err)
			return ExitOther
		}
		body = string(data)
	} else if !haveBody {
		s.Errorf("bdd: memory set: a positional body or --stdin is required\n")
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "memory set", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	m, err := db.Remember(ctx, bdd.Remember{Key: key, Body: body, Actor: actor})
	if err != nil {
		s.Errorf("bdd: memory set: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(toMemoryResult(m)); err != nil {
			s.Errorf("bdd: memory set: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, m.Key)
	return ExitSuccess
}

// runMemoryList implements `bdd memory list`.
func runMemoryList(g GlobalFlags, args []string, s *Streams) int {
	if len(args) > 0 {
		return reportUnknownArg(s, "memory list", args[0])
	}
	return listMemories(g, "", "memory list", s)
}

// runMemorySearch implements `bdd memory search <query>`.
func runMemorySearch(g GlobalFlags, args []string, s *Streams) int {
	if arg, found := firstFlagArg(args); found {
		return reportUnknownArg(s, "memory search", arg)
	}
	if len(args) != 1 {
		s.Errorf("bdd: memory search: expected exactly one query argument\n")
		return ExitUsage
	}
	return listMemories(g, args[0], "memory search", s)
}

// listMemories lists memories matching query (empty for all), shared by
// runMemoryList and runMemorySearch.
func listMemories(g GlobalFlags, query, label string, s *Streams) int {
	ctx := context.Background()
	db, code := openDB(ctx, g, label, s)
	if db == nil {
		return code
	}
	defer db.Close()

	memories, err := db.Memories(ctx, bdd.MemoryQuery{Query: query})
	if err != nil {
		s.Errorf("bdd: %s: %v\n", label, err)
		return ExitCode(err)
	}

	if s.JSON {
		arr := NewJSONArray(s.Stdout)
		for _, m := range memories {
			if err := arr.WriteItem(toMemoryResult(&m)); err != nil {
				s.Errorf("bdd: %s: %v\n", label, err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: %s: %v\n", label, err)
			return ExitOther
		}
		return ExitSuccess
	}

	for _, m := range memories {
		fmt.Fprintf(s.Stdout, "%s\t%s\n", m.Key, firstLine(m.Body))
	}
	return ExitSuccess
}

// runMemoryGet implements `bdd memory get <key>`.
func runMemoryGet(g GlobalFlags, args []string, s *Streams) int {
	if arg, found := firstFlagArg(args); found {
		return reportUnknownArg(s, "memory get", arg)
	}
	if len(args) != 1 {
		s.Errorf("bdd: memory get: expected exactly one key argument\n")
		return ExitUsage
	}
	key := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "memory get", s)
	if db == nil {
		return code
	}
	defer db.Close()

	m, err := db.Recall(ctx, key)
	if err != nil {
		s.Errorf("bdd: memory get: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(toMemoryResult(m)); err != nil {
			s.Errorf("bdd: memory get: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, m.Body)
	return ExitSuccess
}

// runMemoryRemove implements `bdd memory remove <key>`.
func runMemoryRemove(g GlobalFlags, args []string, s *Streams) int {
	if arg, found := firstFlagArg(args); found {
		return reportUnknownArg(s, "memory remove", arg)
	}
	if len(args) != 1 {
		s.Errorf("bdd: memory remove: expected exactly one key argument\n")
		return ExitUsage
	}
	key := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "memory remove", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	if err := db.Forget(ctx, key, actor); err != nil {
		s.Errorf("bdd: memory remove: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(KeyResult{Key: key}); err != nil {
			s.Errorf("bdd: memory remove: %v\n", err)
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
