# Contributing

**Good first issues** are labeled [`good first issue`](https://github.com/kritidutta01/sec-cli/issues?q=label%3A%22good+first+issue%22) on the tracker — start there.

**Bug reports:** open an issue with the exact command you ran, the ticker + filing year, the full error output, and the accession number if you have it.

**Corpus additions:** new hand-verified filings in `internal/accuracy/testdata/corpus/` are the highest-leverage contribution. Any large-cap iXBRL 10-K filed after 2021 is fair game. See `docs/schema.md` for fixture format.

**Features:** open an issue before writing code. `DESIGN.md` is the decision record; anything that conflicts with it needs a design discussion first, not a surprise PR.

**Code conventions:** `make test` and `make lint` must pass. No new dependencies without discussion. Tests are not optional — a PR without tests covering the new path will not merge.

By contributing you agree your work is released under the [MIT License](LICENSE).
