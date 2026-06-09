# Confidence calibration (Phase 13)

`internal/ixbrl`'s `confidenceLevel` buckets a projected statement into
`high` / `medium` / `low` from two completeness rates — the fraction of rows with
every period filled (`rowRate`) and the fraction of cells filled by a fact
(`cellRate`):

```
high   : rowRate >= 0.95 && cellRate >= 0.95
medium : rowRate >= 0.85 || (0.80 <= cellRate <= 0.95)
low    : otherwise
```

Phases 6–9 set these thresholds by hand. Phase 13's job is to check them against
**measured** accuracy rather than intuition, using the accuracy harness
(`internal/accuracy`) over a corpus of hand-verified filings.

## Method

The harness runs the full Phase 11 pipeline against each corpus filing's recorded
bytes (hermetic — the corpus ships the fixtures) and scores the assembled
document against an independent, hand-verified `baseline.json`: the expected
statement values (by concept and period) and the expected section item ids. It
reports per-field statement accuracy, section coverage, and — the calibration
signal — **accuracy within each confidence bucket**.

Run it locally with:

```
go test ./internal/accuracy            # the regression gate
sec-cli accuracy internal/accuracy/testdata/corpus   # the human-readable report
```

## Corpus

Four filings chosen to exercise the fallbacks the thresholds are meant to
separate:

| Filing   | Format         | Section path        | Statement completeness        | Expected confidence |
|----------|----------------|---------------------|-------------------------------|---------------------|
| globex   | IXBRL          | TOC anchors         | every cell present & tagged   | high                |
| umbrella | PartialIXBRL   | TOC anchors         | statement fully tagged        | high                |
| acme     | IXBRL          | TOC anchors         | one missing period cell       | medium              |
| initech  | IXBRL          | heading-pattern     | one missing period cell       | medium              |

## Measured result

```
overall: statement accuracy 100.0% (32/32), section coverage 100.0% (13/13)

confidence calibration (accuracy within each bucket):
  high   2 filing(s): 100.0% cell accuracy
  medium 2 filing(s): 100.0% cell accuracy
```

## Conclusion

On the corpus the parser reproduces every baseline cell and section, and the
bucket the pipeline assigns matches the hand-verified expectation for all four
filings. The `high` bucket is at least as accurate as `medium` (the calibration
is not inverted), and the `medium` cases are exactly the ones with a genuinely
missing cell or a weaker (heading-pattern) section path — i.e. the thresholds
land the softer extractions in `medium`, as intended.

The existing thresholds are therefore **kept** — the measurement validates them
rather than forcing a retune. The harness is now the regression gate: the floors
in `accuracy_test.go` (`minStatementAccuracy`, `minSectionCoverage`) fail CI if a
parser change drops accuracy below the checked-in baseline, and the per-filing
`ExpectedConfidence == GotConfidence` assertion fails if a change moves a filing
into the wrong bucket. Re-tuning the thresholds in the future is a deliberate act:
change `confidenceLevel`, re-run the harness, and update this evidence and the
baselines together.
