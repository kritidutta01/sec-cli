# seccli

A thin Python wrapper over the [`sec-cli`](https://github.com/kritidutta01/sec-cli)
binary. It locates the binary, invokes `get`/`diff` with `--format json`, and
deserializes the canonical JSON into typed dataclasses. The Go JSON schema is the
only contract — the wrapper adds no parsing logic of its own, so the Go and Python
views never diverge.

## Install

```sh
pip install seccli
```

The wheel bundles the matching static `sec-cli` binary (pure-Go, no cgo), so no
Go toolchain is needed. To use an existing binary instead, put `sec-cli` on
`PATH` or set `SECCLI_BINARY=/path/to/sec-cli`.

## Use

```python
import seccli

doc = seccli.get("AAPL")                     # latest 10-K, as a Document
print(doc.metadata.company, doc.metadata.period_end)
for s in doc.sections:
    print(s.item, s.title)

income = doc.statements[0]
for row in income.rows:
    print(row.label, row.values)             # absent cells are None, never 0

changes = seccli.diff("AAPL", frm=2023, to=2024)   # a ChangeSet
for st in changes.statements:
    for r in st.rows:
        print(r.status, r.label, r.cells)
```

`get` accepts `form=`, `year=`, `section=`, and `no_cache=`. `diff` accepts
`form=` and `no_cache=`. A binary failure (non-zero exit) raises `SecCliError`
carrying the binary's `sec-cli: <err>` message; a missing binary raises
`SecCliNotFoundError`.

## Binary resolution order

1. `$SECCLI_BINARY`
2. the binary bundled in the wheel (`seccli/_bin/`)
3. `sec-cli` on `PATH`

## Versioning

The package version tracks the `sec-cli` release it ships. See
[docs/versioning.md](../docs/versioning.md) for how release versions map to the
output `schema_version`.
