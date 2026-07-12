// Package fixture generates deterministic bdd workspace SQLite databases for
// benchmarking and QA verification, using the same embedded migrations
// (internal/schema) as the real bdd storage layer, so fixtures stay openable
// by the real bdd binary.
package fixture
