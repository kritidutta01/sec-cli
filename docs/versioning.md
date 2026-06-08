# Versioning

sec-cli carries three version identities. Two are user-facing contracts; one is
internal.

| Identity | Where it lives | What it means |
|----------|----------------|---------------|
| **Release version** | the git tag → binary `version` (via `-ldflags -X main.version`) and the `seccli` wheel version | which build you have |
| **`schema_version`** | `model.SchemaVersion`, stamped on every JSON document and change set | the output JSON contract |
| **`parser_version`** | `model.ParserVersion`, the cache key for parsed output | the extraction code's identity |

## Release version

The git tag is the single source. On `git tag vX.Y.Z && git push --tags`,
goreleaser injects `vX.Y.Z` into the binary's `version` string and builds the
binaries; the `seccli` wheel ships under the same number. `go install
.../cmd/sec-cli@latest` and `pip install seccli` therefore resolve to the same
release. In development the binary reports `0.0.0-dev`.

## Output contract: `schema_version`

`schema_version` is **independent** of the release version: it changes only when
the JSON output shape changes, following semver — additive fields bump the minor,
renames/removals bump the major. A consumer (the Python wrapper, any downstream
tool) keys on `schema_version`, not the release version, to know which output
contract a given run speaks. The field reference is [schema.md](schema.md).

### Release ↔ schema map

| Release | `schema_version` | Notes |
|---------|------------------|-------|
| v0.x    | `1.0.0`          | initial document + change-set schema (Phases 9–12) |

When a release changes the output shape, add a row here and bump
`model.SchemaVersion` in the same change.

## `parser_version`

`parser_version` is not a public contract — it is the extraction code's identity,
used by the SQLite cache to key parsed documents. Bumping it (a parser bug fix)
invalidates cached parsed output without touching cached raw bytes, so a re-parse
costs no network. It moves on its own cadence, unrelated to the release tag.
