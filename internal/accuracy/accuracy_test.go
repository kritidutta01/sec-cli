package accuracy

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update golden files")

const corpusDir = "testdata/corpus"

// The regression gate: the parser must reproduce at least this much of the
// checked-in baselines. They are 1.0 today (the synthetic corpus is fully
// supported); a parser change that quietly breaks a filer drops a number below
// its floor and fails CI. Updating a floor is a deliberate, reviewed act.
const (
	minStatementAccuracy = 1.0
	minSectionCoverage   = 1.0
)

func runScore(t *testing.T) *Report {
	t.Helper()
	t.Setenv("SEC_CLI_USER_AGENT", "sec-cli-accuracy-test contact@example.com")
	rep, err := Score(corpusDir)
	require.NoError(t, err)
	return rep
}

// TestScore_RegressionGate is the gate: it runs the full pipeline over the
// corpus and asserts statement accuracy and section coverage meet the
// checked-in floors, overall and per filing.
func TestScore_RegressionGate(t *testing.T) {
	rep := runScore(t)
	require.NotEmpty(t, rep.Filings)

	require.GreaterOrEqualf(t, rep.Totals.StatementAccuracy(), minStatementAccuracy,
		"overall statement accuracy regressed: %d/%d cells", rep.Totals.MatchedCells, rep.Totals.TotalCells)
	require.GreaterOrEqualf(t, rep.Totals.SectionCoverage(), minSectionCoverage,
		"overall section coverage regressed: %d/%d sections", rep.Totals.MatchedSections, rep.Totals.TotalSections)

	for _, f := range rep.Filings {
		require.GreaterOrEqualf(t, f.StatementAccuracy(), minStatementAccuracy,
			"%s statement accuracy regressed (%d/%d)", f.Name, f.MatchedCells, f.TotalCells)
		require.GreaterOrEqualf(t, f.SectionCoverage(), minSectionCoverage,
			"%s section coverage regressed (%d/%d)", f.Name, f.MatchedSections, f.TotalSections)
	}
}

// TestScore_ConfidenceCalibration asserts the confidence the pipeline assigns
// each filing matches its hand-verified expectation, and that the buckets are
// monotone — "high" filings score at least as accurately as "medium" ones. This
// is the evidence that the confidenceLevel thresholds (internal/ixbrl) are
// calibrated rather than guessed.
func TestScore_ConfidenceCalibration(t *testing.T) {
	rep := runScore(t)
	for _, f := range rep.Filings {
		require.Equalf(t, f.ExpectedConfidence, f.GotConfidence,
			"%s landed in the wrong confidence bucket", f.Name)
	}
	high, hasHigh := rep.Buckets["high"]
	med, hasMed := rep.Buckets["medium"]
	if hasHigh && hasMed {
		require.GreaterOrEqual(t, high.Accuracy(), med.Accuracy(),
			"high bucket must not be less accurate than medium — calibration inverted")
	}
}

// TestCorpus_Diversity asserts the corpus exercises the fallbacks: at least four
// filings and at least one PartialIXBRL (the mixed fact-tagged/plain-HTML path).
func TestCorpus_Diversity(t *testing.T) {
	entries, err := os.ReadDir(corpusDir)
	require.NoError(t, err)

	var filings, partial int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		base, err := loadBaseline(filepath.Join(corpusDir, e.Name()))
		require.NoError(t, err)
		filings++
		if base.Format == "PartialIXBRL" {
			partial++
		}
	}
	require.GreaterOrEqual(t, filings, 4, "corpus needs at least four diverse filings")
	require.GreaterOrEqual(t, partial, 1, "corpus needs at least one PartialIXBRL filing")
}

// TestCellMatch covers the cell-comparison logic the harness scores with:
// tolerance on present values, null-vs-null match, and null-vs-value mismatch.
func TestCellMatch(t *testing.T) {
	f := func(v float64) *float64 { return &v }

	require.True(t, cellMatch(nil, nil), "two nulls match")
	require.False(t, cellMatch(f(100), nil), "a dropped cell is a miss")
	require.False(t, cellMatch(nil, f(0)), "a phantom 0 where null was expected is a miss")
	require.True(t, cellMatch(f(1000000000), f(1000000000)), "exact large value matches")
	require.True(t, cellMatch(f(1000000000), f(1000000000.5)), "within tolerance matches")
	require.False(t, cellMatch(f(1000), f(1100)), "a real difference is a miss")
	require.False(t, cellMatch(f(0), f(1)), "0 and 1 differ beyond tolerance")
}

// TestScore_GoldenReport golden-files the human-readable report so the corpus's
// measured numbers are reviewable and changes show up in a diff.
func TestScore_GoldenReport(t *testing.T) {
	rep := runScore(t)
	got := rep.String()

	path := filepath.Join("testdata", "report.txt")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden report.txt (run: go test ./internal/accuracy -update)")
	require.Equal(t, string(want), got)
}
