## When you change

The knowledge snapshot sync: the snapshot pass, its paths, limits, key scan and per-file failures (`internal/knowledge/snapshot.go`), the built-in converter (`internal/knowledge/convert.go`), the reader picked by a source's kind (`kindReaders`, `Ingestor.reader` in `internal/knowledge/ingest.go`) or the plain-key prefixes (`domain.KeyPrefixes`, `domain.LooksLikeKey` in `internal/core/domain/keys.go`)

## Also change

The secrets check's `plain-key` (`internal/authoring/secrets.go`), which reads the same prefixes; the snapshot tests (`internal/knowledge/snapshot_test.go`) and the ingestor's (`ingest_test.go`); goald's wiring of the ingestor (`openKnowledge` in `cmd/goald/main.go`); the "Knowledge snapshots" paragraph of `docs/engineering/architecture.md`

## Run

`task test`, `task lint`
