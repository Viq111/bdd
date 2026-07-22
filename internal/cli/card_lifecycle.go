package cli

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/viq111/bdd"
)

// runCardNote implements `bdd note <id> [body] [--stdin]`.
func runCardNote(g GlobalFlags, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: note: card id is required\n")
		return ExitUsage
	}
	if strings.HasPrefix(args[0], "-") {
		return reportUnknownArg(s, "note", args[0])
	}
	id := args[0]
	rest := args[1:]

	var body string
	var haveBody, stdin bool

	i := 0
	for i < len(rest) {
		arg := rest[i]
		if arg == "--stdin" {
			stdin = true
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			s.Errorf("bdd: note: unknown flag %q\n", arg)
			return ExitUsage
		}
		if haveBody {
			s.Errorf("bdd: note: unexpected argument %q\n", arg)
			return ExitUsage
		}
		body, haveBody = arg, true
		i++
	}

	if stdin && haveBody {
		s.Errorf("bdd: note: cannot combine a positional body with --stdin\n")
		return ExitUsage
	}
	if stdin {
		data, err := io.ReadAll(s.Stdin)
		if err != nil {
			s.Errorf("bdd: note: reading stdin: %v\n", err)
			return ExitOther
		}
		body = string(data)
	} else if !haveBody {
		s.Errorf("bdd: note: a positional body or --stdin is required\n")
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "note", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	note, err := db.AddNote(ctx, bdd.AddNote{CardID: id, Body: body, Author: actor})
	if err != nil {
		s.Errorf("bdd: note: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(toNoteResult(*note)); err != nil {
			s.Errorf("bdd: note: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	s.Stdout.Write([]byte(id + "\n"))
	return ExitSuccess
}

// runCardClose implements `bdd close <id> [reason]`.
func runCardClose(g GlobalFlags, args []string, s *Streams) int {
	if arg, found := firstFlagArg(args); found {
		return reportUnknownArg(s, "close", arg)
	}
	if len(args) == 0 {
		s.Errorf("bdd: close: card id is required\n")
		return ExitUsage
	}
	id := args[0]
	var reason string
	if len(args) == 2 {
		reason = args[1]
	} else if len(args) > 2 {
		s.Errorf("bdd: close: unexpected argument %q\n", args[2])
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "close", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	card, err := db.CloseCard(ctx, id, bdd.CloseCard{Reason: reason, Actor: actor})
	if err != nil {
		s.Errorf("bdd: close: %v\n", err)
		return ExitCode(err)
	}
	return emitCard(s, "close", toCardResult(card))
}

// runCardReopen implements `bdd reopen <id>`.
func runCardReopen(g GlobalFlags, args []string, s *Streams) int {
	if arg, found := firstFlagArg(args); found {
		return reportUnknownArg(s, "reopen", arg)
	}
	if len(args) != 1 {
		s.Errorf("bdd: reopen: expected exactly one card id argument\n")
		return ExitUsage
	}
	id := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "reopen", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	card, err := db.ReopenCard(ctx, id, actor)
	if err != nil {
		s.Errorf("bdd: reopen: %v\n", err)
		return ExitCode(err)
	}
	return emitCard(s, "reopen", toCardResult(card))
}

// runCardDefer implements `bdd defer <id> [--until <RFC3339 timestamp>]`.
func runCardDefer(g GlobalFlags, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: defer: card id is required\n")
		return ExitUsage
	}
	if strings.HasPrefix(args[0], "-") {
		return reportUnknownArg(s, "defer", args[0])
	}
	id := args[0]
	rest := args[1:]

	var untilRaw string
	var haveUntil bool

	i := 0
	for i < len(rest) {
		arg := rest[i]
		name, inline, hasInline := cutFlagValue(arg)
		if name != "--until" {
			s.Errorf("bdd: defer: unknown flag %q\n", arg)
			return ExitUsage
		}
		val, consumed, err := flagValue(name, inline, hasInline, rest, i)
		if err != nil {
			s.Errorf("bdd: defer: %v\n", err)
			return ExitUsage
		}
		untilRaw, haveUntil = val, true
		i += consumed
	}

	var until *time.Time
	if haveUntil {
		t, err := parseTimeFlag(untilRaw)
		if err != nil {
			s.Errorf("bdd: defer: --until %v\n", err)
			return ExitUsage
		}
		until = &t
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "defer", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	card, err := db.DeferCard(ctx, id, actor, until)
	if err != nil {
		s.Errorf("bdd: defer: %v\n", err)
		return ExitCode(err)
	}
	return emitCard(s, "defer", toCardResult(card))
}

// runCardHuman implements `bdd human <id> [reason]`.
func runCardHuman(g GlobalFlags, args []string, s *Streams) int {
	if arg, found := firstFlagArg(args); found {
		return reportUnknownArg(s, "human", arg)
	}
	if len(args) == 0 {
		s.Errorf("bdd: human: card id is required\n")
		return ExitUsage
	}
	id := args[0]
	var reason string
	if len(args) == 2 {
		reason = args[1]
	} else if len(args) > 2 {
		s.Errorf("bdd: human: unexpected argument %q\n", args[2])
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "human", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	card, err := db.HumanCard(ctx, id, actor, reason)
	if err != nil {
		s.Errorf("bdd: human: %v\n", err)
		return ExitCode(err)
	}
	return emitCard(s, "human", toCardResult(card))
}
