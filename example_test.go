package bdd_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/viq111/bdd"
)

// exampleDB creates a throwaway workspace for use by the Example* functions
// below and returns a cleanup func that closes the database and removes the
// workspace directory.
func exampleDB() (*bdd.DB, func()) {
	dir, err := os.MkdirTemp("", "bdd-example")
	if err != nil {
		panic(err)
	}
	db, err := bdd.Init(context.Background(), bdd.InitOptions{Workspace: dir, Prefix: "ex"})
	if err != nil {
		panic(err)
	}
	return db, func() {
		db.Close()
		os.RemoveAll(dir)
	}
}

func ExampleOpen() {
	dir, err := os.MkdirTemp("", "bdd-example-open")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(dir)

	initDB, err := bdd.Init(context.Background(), bdd.InitOptions{Workspace: dir, Prefix: "ex"})
	if err != nil {
		fmt.Println(err)
		return
	}
	initDB.Close()

	db, err := bdd.Open(context.Background(), bdd.OpenOptions{Workspace: dir})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()

	prefix, err := db.Prefix(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(prefix)
	// Output: ex
}

func ExampleDB_CreateCard() {
	db, cleanup := exampleDB()
	defer cleanup()
	ctx := context.Background()

	reproduction := "Start two writers and interrupt one during compaction"
	acceptance := "The previous cache remains readable"
	card, err := db.CreateCard(ctx, bdd.CreateCard{
		Title:        "Cache corruption",
		Type:         bdd.CardTypeBug,
		Reproduction: &reproduction,
		Acceptance:   &acceptance,
		CreatedBy:    "alice",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(card.Title, card.Type, card.Status, card.Priority)
	// Output: Cache corruption bug open 2
}

func ExampleDB_ClaimCard() {
	db, cleanup := exampleDB()
	defer cleanup()
	ctx := context.Background()

	card, err := db.CreateCard(ctx, bdd.CreateCard{
		Title:     "Tidy up",
		Type:      bdd.CardTypeChore,
		CreatedBy: "alice",
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	claimed, err := db.ClaimCard(ctx, card.ID, "alice")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(claimed.Status, claimed.Assignee)
	// Output: in_progress alice
}

func ExampleDB_ReadyCards() {
	db, cleanup := exampleDB()
	defer cleanup()
	ctx := context.Background()

	if _, err := db.CreateCard(ctx, bdd.CreateCard{Title: "First", Type: bdd.CardTypeChore, CreatedBy: "alice"}); err != nil {
		fmt.Println(err)
		return
	}
	second, err := db.CreateCard(ctx, bdd.CreateCard{Title: "Second", Type: bdd.CardTypeChore, CreatedBy: "alice"})
	if err != nil {
		fmt.Println(err)
		return
	}
	// Claim the second card so it drops out of the ready set.
	if _, err := db.ClaimCard(ctx, second.ID, "alice"); err != nil {
		fmt.Println(err)
		return
	}

	ready, err := db.ReadyCards(ctx, bdd.ReadyOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, c := range ready {
		fmt.Println(c.Title)
	}
	// Output: First
}

func ExampleDB_Remember() {
	db, cleanup := exampleDB()
	defer cleanup()
	ctx := context.Background()

	mem, err := db.Remember(ctx, bdd.Remember{
		Key:   "testing-race",
		Body:  "Always run the race tests",
		Actor: "alice",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(mem.Key, mem.Body)
	// Output: testing-race Always run the race tests
}

func ExampleDB_PutRune() {
	db, cleanup := exampleDB()
	defer cleanup()
	ctx := context.Background()

	title := "Programmer"
	body := "Implements the CLI, domain model, and storage."
	r, err := db.PutRune(ctx, bdd.PutRune{
		Key:  "role/programmer",
		Kind: "role",
		Mutation: bdd.RuneMutation{
			Title: &title,
			Body:  &body,
		},
		Actor: "alice",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(r.Key, r.Kind, r.Enabled)
	// Output: role/programmer role true
}

func ExampleDB_Snapshot() {
	db, cleanup := exampleDB()
	defer cleanup()
	ctx := context.Background()

	result, err := db.Snapshot(ctx, bdd.SnapshotOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(filepath.Base(result.Path))
	// Output: backup.sqlite
}
