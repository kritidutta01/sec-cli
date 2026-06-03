package ixbrl

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// extractFixture parses a fixture and extracts its facts against its own contexts.
func extractFixture(t *testing.T, name string) ([]Fact, map[string]Context) {
	t.Helper()
	doc := parseFixture(t, name)
	contexts, err := ParseContexts(doc)
	require.NoError(t, err)
	facts, err := ExtractFacts(doc, contexts)
	require.NoError(t, err)
	return facts, contexts
}

func factByID(facts []Fact, id string) (Fact, bool) {
	for _, f := range facts {
		if f.ID == id {
			return f, true
		}
	}
	return Fact{}, false
}

func TestExtractFacts_Synthetic(t *testing.T) {
	facts, _ := extractFixture(t, "synthetic.htm")

	// f-7 (Goodwill, xsi:nil) must be skipped; everything else kept.
	require.Len(t, facts, 8)
	_, nilPresent := factByID(facts, "f-7")
	require.False(t, nilPresent, "xsi:nil facts must be skipped")

	// f-9 uses a word-number transform we don't decode: kept, flagged Unparsed,
	// raw preserved, rather than aborting the whole extraction.
	vendors, ok := factByID(facts, "f-9")
	require.True(t, ok)
	require.True(t, vendors.Numeric)
	require.True(t, vendors.Unparsed)
	require.Equal(t, "two", vendors.Raw)
	require.Zero(t, vendors.Value)

	rev, ok := factByID(facts, "f-1")
	require.True(t, ok)
	require.True(t, rev.Numeric)
	require.False(t, rev.Negative)
	require.Equal(t, 6, rev.Scale)
	require.InDelta(t, 1.234e9, rev.Value, 1, "1,234 × 10^6 with scale applied once")

	opLoss, _ := factByID(facts, "f-2")
	require.True(t, opLoss.Negative, "sign=\"-\" must negate")
	require.InDelta(t, -5e8, opLoss.Value, 1)

	otherExp, _ := factByID(facts, "f-3")
	require.True(t, otherExp.Negative, "surrounding parentheses must negate")
	require.InDelta(t, -75000, otherExp.Value, 0.5, "(75) at scale 3 → -75000")

	cash, _ := factByID(facts, "f-4")
	require.InDelta(t, 9e9, cash.Value, 1)

	eps, _ := factByID(facts, "f-5")
	require.InDelta(t, 6.08, eps.Value, 1e-9, "fractional value at scale 0 is preserved")

	// nonNumeric fact: kept, but flagged non-numeric with its raw text.
	fy, ok := factByID(facts, "f-8")
	require.True(t, ok)
	require.False(t, fy.Numeric)
	require.Equal(t, "dei:DocumentFiscalYearFocus", fy.Concept)
	require.Equal(t, "2024", fy.Raw)
}

func TestExtractFacts_SkipsUnresolvableContext(t *testing.T) {
	doc := parseFixture(t, "synthetic.htm")
	// Empty context map → every fact's contextRef is unresolvable → all dropped.
	facts, err := ExtractFacts(doc, map[string]Context{})
	require.NoError(t, err)
	require.Empty(t, facts)
}

func TestExtractFacts_AAPLRevenue(t *testing.T) {
	facts, _ := extractFixture(t, "aapl-fy2024-excerpt.htm")

	var rev *Fact
	for i := range facts {
		f := facts[i]
		if f.Concept == "us-gaap:RevenueFromContractWithCustomerExcludingAssessedTax" && f.ContextRef == "c-1" {
			rev = &facts[i]
			break
		}
	}
	require.NotNil(t, rev, "consolidated total net sales fact must be extracted")
	// Apple FY2024 total net sales: $391,035 million.
	require.InDelta(t, 391035e6, rev.Value, 1)
	require.False(t, rev.Negative)
	require.Equal(t, "usd", rev.UnitRef)
}

// TestExtractFacts_RealFiling runs against the full checked-out filing when it
// is present locally (it is gitignored under /data/, so CI skips this). It is a
// smoke test over the whole 1.5 MB document, complementing the committed excerpt.
func TestExtractFacts_RealFiling(t *testing.T) {
	const path = "../../data/AAPL_10K_FY2024.htm"
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("real filing not present (%s); skipping full-file smoke test", path)
	}
	defer func() { _ = f.Close() }()

	doc, err := html.Parse(f)
	require.NoError(t, err)
	contexts, err := ParseContexts(doc)
	require.NoError(t, err)
	require.Greater(t, len(contexts), 100, "real filing should define many contexts")

	facts, err := ExtractFacts(doc, contexts)
	require.NoError(t, err)

	var found bool
	for _, fct := range facts {
		if fct.Concept == "us-gaap:RevenueFromContractWithCustomerExcludingAssessedTax" &&
			fct.ContextRef == "c-1" {
			require.InDelta(t, 391035e6, fct.Value, 1)
			found = true
			break
		}
	}
	require.True(t, found, "consolidated FY2024 net sales fact must be present")
}
