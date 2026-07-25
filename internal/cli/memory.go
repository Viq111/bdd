package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/viq111/bdd"
)

// runMemory implements the `bdd memory` group's fallback dispatch for a
// missing or unknown subcommand; matched subcommands are routed directly
// to their leaf by cobra and never reach here. "memory set" specifically is
// intercepted earlier, in Run (internal/cli/cli.go), before cobra ever
// parses cmdArgs, so its create/update steering message fires regardless of
// what flags follow "set" — see the comment there for why.
func runMemory(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: memory: missing subcommand (create, update, get, list, search, remove)\n")
		return ExitUsage
	}
	s.Errorf("bdd: memory: unknown subcommand %q\n", args[0])
	return ExitUsage
}

// MemoryResult is the JSON/human result of `bdd memory create` and
// `bdd memory update`/`bdd memory get`, and (per entry) `bdd memory
// list`/`bdd memory search`.
type MemoryResult struct {
	Key       string `json:"key"`
	Body      string `json:"body"`
	Prime     string `json:"prime"`
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
		Prime:     m.Prime,
		CreatedBy: m.CreatedBy,
		UpdatedBy: m.UpdatedBy,
		CreatedAt: m.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt: m.UpdatedAt.Format(time.RFC3339Nano),
		Revision:  m.Revision,
	}
}

// runMemoryCreate implements `bdd memory create [body] --key <key> [--stdin]`.
func runMemoryCreate(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	fs := cmd.Flags()
	key, _ := flagString(fs, "key")
	if strings.TrimSpace(key) == "" {
		s.Errorf("bdd: memory create: --key is required\n")
		return ExitUsage
	}
	prime, havePrime := flagString(fs, "prime")
	useStdin := flagBool(fs, "stdin")

	var body string
	var haveBody bool
	if len(args) > 0 {
		body, haveBody = args[0], true
	}
	if len(args) > 1 {
		s.Errorf("bdd: memory create: unexpected argument %q\n", args[1])
		return ExitUsage
	}

	if useStdin && haveBody {
		s.Errorf("bdd: memory create: cannot combine a positional body with --stdin\n")
		return ExitUsage
	}
	if useStdin {
		data, err := io.ReadAll(s.Stdin)
		if err != nil {
			s.Errorf("bdd: memory create: reading stdin: %v\n", err)
			return ExitOther
		}
		body = string(data)
	} else if !haveBody {
		s.Errorf("bdd: memory create: a positional body or --stdin is required\n")
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "memory create", s)
	if db == nil {
		return code
	}
	defer db.Close()

	remember := bdd.Remember{Key: key, Body: body, Actor: ResolveActor(g.Actor)}
	if havePrime {
		remember.Prime = &prime
	}
	m, err := db.CreateMemory(ctx, remember)
	if err != nil {
		s.Errorf("bdd: memory create: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(toMemoryResult(m)); err != nil {
			s.Errorf("bdd: memory create: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, m.Key)
	return ExitSuccess
}

// runMemoryUpdate implements `bdd memory update <key> [body] [--stdin]`.
func runMemoryUpdate(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: memory update: key is required\n")
		return ExitUsage
	}
	key := args[0]
	rest := args[1:]

	fs := cmd.Flags()
	prime, havePrime := flagString(fs, "prime")
	useStdin := flagBool(fs, "stdin")

	var body string
	var haveBody bool
	if len(rest) > 0 {
		body, haveBody = rest[0], true
	}
	if len(rest) > 1 {
		s.Errorf("bdd: memory update: unexpected argument %q\n", rest[1])
		return ExitUsage
	}

	if useStdin && haveBody {
		s.Errorf("bdd: memory update: cannot combine a positional body with --stdin\n")
		return ExitUsage
	}
	if useStdin {
		data, err := io.ReadAll(s.Stdin)
		if err != nil {
			s.Errorf("bdd: memory update: reading stdin: %v\n", err)
			return ExitOther
		}
		body = string(data)
	} else if !haveBody {
		s.Errorf("bdd: memory update: a positional body or --stdin is required\n")
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "memory update", s)
	if db == nil {
		return code
	}
	defer db.Close()

	remember := bdd.Remember{Key: key, Body: body, Actor: ResolveActor(g.Actor)}
	if havePrime {
		remember.Prime = &prime
	}
	m, err := db.UpdateMemory(ctx, remember)
	if err != nil {
		s.Errorf("bdd: memory update: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(toMemoryResult(m)); err != nil {
			s.Errorf("bdd: memory update: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, m.Key)
	return ExitSuccess
}

// runMemoryList implements `bdd memory list`.
func runMemoryList(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) > 0 {
		return reportUnknownArg(s, "memory list", args[0])
	}
	return listMemories(g, "", "memory list", s)
}

// runMemorySearch implements `bdd memory search <query>`.
func runMemorySearch(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
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
func runMemoryGet(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
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
func runMemoryRemove(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
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
