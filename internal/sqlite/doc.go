// Package sqlite owns the low-level SQLite connection: opening with the
// required PRAGMAs (foreign_keys, journal_mode, synchronous, busy_timeout),
// connection pooling policy, and bounded retry-with-jitter on SQLITE_BUSY.
package sqlite
