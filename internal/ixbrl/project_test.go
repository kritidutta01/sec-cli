package ixbrl

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// rowByLabel returns the projected row with the given display label.
func rowByLabel(tbl *Table, label string) (Row, bool) {
	for _, r := range tbl.Rows {
		if r.Label == label {
			return r, true
		}
	}
	return Row{}, false
}

// rowByLabelContains returns the first row whose label contains substr.
func rowByLabelContains(tbl *Table, substr string) (Row, bool) {
	for _, r := range tbl.Rows {
		if strings.Contains(r.Label, substr) {
			return r, true
		}
	}
	return Row{}, false
}

func mustDate(s string) time.Time {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

// projectFixture loads a linkbase + facts fixture set and projects the single
// role it defines.
func projectFixture(t *testing.T, preXML, labXML, factsHTM string, titles map[string]string) *Table {
	t.Helper()
	roles, err := ParseLinkbase(readFixture(t, preXML), readFixture(t, labXML), titles)
	require.NoError(t, err)
	require.Len(t, roles, 1)

	facts, contexts := extractFixture(t, factsHTM)
	return ProjectTable(roles[0], facts, contexts)
}

// rowByConcept returns the projected row for a concept.
func rowByConcept(tbl *Table, concept string) (Row, bool) {
	for _, r := range tbl.Rows {
		if r.Concept == concept {
			return r, true
		}
	}
	return Row{}, false
}

func TestProjectTable_Synthetic(t *testing.T) {
	tbl := projectFixture(t,
		"synthetic-income_pre.xml", "synthetic-income_lab.xml", "synthetic-income-facts.htm", nil)

	// Two comparative columns, newest first.
	require.Len(t, tbl.Columns, 2)
	require.Equal(t, "2024", tbl.Columns[0].Label)
	require.Equal(t, "2023", tbl.Columns[1].Label)
	require.False(t, tbl.Columns[0].Instant)

	// Structural nodes (the abstract root) are not rows.
	require.Len(t, tbl.Rows, 7)
	_, hasAbstract := rowByConcept(tbl, "acme:IncomeStatementAbstract")
	require.False(t, hasAbstract)

	rev, ok := rowByConcept(tbl, "acme:Revenues")
	require.True(t, ok)
	require.Equal(t, "Revenue", rev.Label)
	require.Equal(t, RowData, rev.Type)
	require.NotNil(t, rev.Values[0])
	require.InDelta(t, 1000e6, *rev.Values[0], 1)
	require.InDelta(t, 900e6, *rev.Values[1], 1)

	// Missing 2023 fact projects to null, not zero.
	rd, _ := rowByConcept(tbl, "acme:ResearchExpense")
	require.NotNil(t, rd.Values[0])
	require.Nil(t, rd.Values[1], "absent fact must be null, never 0")

	// Row typing: totalLabel → total, non-abstract parent → subtotal, leaf → data.
	gp, _ := rowByConcept(tbl, "acme:GrossProfit")
	require.Equal(t, RowTotal, gp.Type)
	opex, _ := rowByConcept(tbl, "acme:OperatingExpenses")
	require.Equal(t, RowTotal, opex.Type)
	ce, _ := rowByConcept(tbl, "acme:CostsAndExpenses")
	require.Equal(t, RowSubtotal, ce.Type)

	// One null cell out of fourteen → medium, with the gap reported.
	require.Equal(t, "medium", tbl.Confidence.Level)
	require.Equal(t, 1, tbl.Confidence.UntaggedCellCount)
	require.Equal(t, extractorFacts, tbl.Source.Extractor)
}

// TestProjectTable_PeriodBoundaryInstants covers the cash-flow opening/closing
// balance case: a concept presented with periodStart/EndLabel is an instant
// value that must be placed into a duration column at its period boundary, with
// the opening balance read from the instant on the day before the period begins.
func TestProjectTable_PeriodBoundaryInstants(t *testing.T) {
	role := LinkbaseRole{
		RoleURI: "http://acme.example/role/CashFlows",
		Title:   "Statement of Cash Flows",
		Concepts: []RoleConcept{
			{Name: "acme:CashFlowAbstract", Structural: true, HasChildren: true, Depth: 0},
			{Name: "acme:OperatingCash", Label: "Operating activities", PreferredLabel: roleTerseLabel, Depth: 1},
			{Name: "acme:InvestingCash", Label: "Investing activities", PreferredLabel: roleTerseLabel, Depth: 1},
			{Name: "acme:FinancingCash", Label: "Financing activities", PreferredLabel: roleTerseLabel, Depth: 1},
			{Name: "acme:NetChange", Label: "Net change in cash", PreferredLabel: roleTotalLabel, Depth: 1},
			{Name: "acme:Cash", Label: "Cash, beginning of year", PreferredLabel: rolePeriodStartLabel, Depth: 1},
			{Name: "acme:Cash", Label: "Cash, end of year", PreferredLabel: rolePeriodEndLabel, Depth: 1},
		},
	}
	facts := []Fact{
		{Concept: "acme:OperatingCash", ContextRef: "dur", Numeric: true, Value: 100e6},
		{Concept: "acme:InvestingCash", ContextRef: "dur", Numeric: true, Value: -20e6},
		{Concept: "acme:FinancingCash", ContextRef: "dur", Numeric: true, Value: -30e6},
		{Concept: "acme:NetChange", ContextRef: "dur", Numeric: true, Value: 50e6},
		{Concept: "acme:Cash", ContextRef: "beg", Numeric: true, Value: 450e6},
		{Concept: "acme:Cash", ContextRef: "end", Numeric: true, Value: 500e6},
	}
	contexts := map[string]Context{
		"dur": {ID: "dur", Start: mustDate("2024-01-01"), End: mustDate("2024-12-31")},
		// Opening balance is the instant on the day before the period begins.
		"beg": {ID: "beg", Start: mustDate("2023-12-31"), End: mustDate("2023-12-31")},
		"end": {ID: "end", Start: mustDate("2024-12-31"), End: mustDate("2024-12-31")},
	}

	tbl := ProjectTable(role, facts, contexts)

	// The instant boundary contexts must not become columns; only the duration is.
	require.Len(t, tbl.Columns, 1)
	require.Equal(t, "2024", tbl.Columns[0].Label)
	require.False(t, tbl.Columns[0].Instant)
	require.Len(t, tbl.Rows, 6) // abstract root dropped

	begin, ok := rowByLabel(tbl, "Cash, beginning of year")
	require.True(t, ok)
	require.NotNil(t, begin.Values[0])
	require.InDelta(t, 450e6, *begin.Values[0], 1, "opening balance from the prior-period-end instant")

	end, _ := rowByLabel(tbl, "Cash, end of year")
	require.NotNil(t, end.Values[0])
	require.InDelta(t, 500e6, *end.Values[0], 1, "closing balance from the period-end instant")

	require.Equal(t, "high", tbl.Confidence.Level)
	require.Equal(t, 0, tbl.Confidence.UntaggedCellCount)
}

func TestProjectTable_AAPLIncome(t *testing.T) {
	const roleURI = "http://www.apple.com/role/CONSOLIDATEDSTATEMENTSOFOPERATIONS"
	tbl := projectFixture(t,
		"aapl-income_pre.xml", "aapl-income_lab.xml", "aapl-income-facts.htm",
		map[string]string{roleURI: "CONSOLIDATED STATEMENTS OF OPERATIONS"})

	// Exactly the 15 line items of the filed statement (structural nodes dropped).
	require.Len(t, tbl.Rows, 15)

	// Three comparative fiscal years, newest first.
	require.Len(t, tbl.Columns, 3)
	require.Equal(t, []string{"2024", "2023", "2022"},
		[]string{tbl.Columns[0].Label, tbl.Columns[1].Label, tbl.Columns[2].Label})

	// Net sales matches Apple's reported FY2024/2023/2022 figures exactly
	// (scale=6 already applied, so values are absolute dollars).
	sales, ok := rowByConcept(tbl, "us-gaap:RevenueFromContractWithCustomerExcludingAssessedTax")
	require.True(t, ok)
	require.Equal(t, "Net sales", sales.Label)
	require.Equal(t, RowData, sales.Type)
	require.InDelta(t, 391035e6, *sales.Values[0], 1)
	require.InDelta(t, 383285e6, *sales.Values[1], 1)
	require.InDelta(t, 394328e6, *sales.Values[2], 1)

	// "Total operating expenses" is a total; gross margin and net income too.
	opex, _ := rowByConcept(tbl, "us-gaap:OperatingExpenses")
	require.Equal(t, "Total operating expenses", opex.Label)
	require.Equal(t, RowTotal, opex.Type)
	require.InDelta(t, 57467e6, *opex.Values[0], 1)

	gp, _ := rowByConcept(tbl, "us-gaap:GrossProfit")
	require.Equal(t, RowTotal, gp.Type)
	require.InDelta(t, 180683e6, *gp.Values[0], 1)

	ni, _ := rowByConcept(tbl, "us-gaap:NetIncomeLoss")
	require.Equal(t, RowTotal, ni.Type)
	require.InDelta(t, 93736e6, *ni.Values[0], 1)

	// Every cell resolved → high confidence, nothing untagged.
	require.Equal(t, "high", tbl.Confidence.Level)
	require.Equal(t, 0, tbl.Confidence.UntaggedCellCount)
	require.InDelta(t, 1.0, tbl.Confidence.CellResolvedRate, 1e-9)
	require.InDelta(t, 1.0, tbl.Confidence.RowMatchRate, 1e-9)
}

func TestProjectTable_AAPLBalanceSheet(t *testing.T) {
	const roleURI = "http://www.apple.com/role/CONSOLIDATEDBALANCESHEETS"
	tbl := projectFixture(t,
		"aapl-balance_pre.xml", "aapl-balance_lab.xml", "aapl-balance-facts.htm",
		map[string]string{roleURI: "CONSOLIDATED BALANCE SHEETS"})

	// The fixture carries four instant contexts, but the statement presents only
	// the two fiscal-year ends; the column-coverage filter drops the two extra
	// period-ends that merely the cash line is also tagged at.
	require.Len(t, tbl.Columns, 2)
	require.Equal(t, []string{"2024", "2023"}, []string{tbl.Columns[0].Label, tbl.Columns[1].Label})
	require.True(t, tbl.Columns[0].Instant, "balance sheet columns are instants")

	// Total assets ties to total liabilities and equity — the sheet balances.
	assets, ok := rowByConcept(tbl, "us-gaap:Assets")
	require.True(t, ok)
	require.Equal(t, RowTotal, assets.Type)
	require.InDelta(t, 364980e6, *assets.Values[0], 1)
	require.InDelta(t, 352583e6, *assets.Values[1], 1)
	liabEq, _ := rowByConcept(tbl, "us-gaap:LiabilitiesAndStockholdersEquity")
	require.InDelta(t, *assets.Values[0], *liabEq.Values[0], 1)
	require.InDelta(t, *assets.Values[1], *liabEq.Values[1], 1)

	cash, _ := rowByConcept(tbl, "us-gaap:CashAndCashEquivalentsAtCarryingValue")
	require.InDelta(t, 29943e6, *cash.Values[0], 1)

	// Shares outstanding is tagged nested inside shares issued; the parser must
	// still surface it as its own resolved row.
	shares, ok := rowByConcept(tbl, "us-gaap:CommonStockSharesOutstanding")
	require.True(t, ok)
	require.NotNil(t, shares.Values[0])
	require.InDelta(t, 15116786e3, *shares.Values[0], 1)

	// Commitments and contingencies is an xsi:nil fact — present as a row, but
	// every cell null (never coerced to 0).
	commit, ok := rowByConcept(tbl, "us-gaap:CommitmentsAndContingencies")
	require.True(t, ok)
	require.Nil(t, commit.Values[0])
	require.Nil(t, commit.Values[1])

	// Two null cells (the commitments line) out of sixty → still high.
	require.Equal(t, "high", tbl.Confidence.Level)
	require.Equal(t, 2, tbl.Confidence.UntaggedCellCount)
}

func TestProjectTable_AAPLCashFlow(t *testing.T) {
	const roleURI = "http://www.apple.com/role/CONSOLIDATEDSTATEMENTSOFCASHFLOWS"
	tbl := projectFixture(t,
		"aapl-cashflow_pre.xml", "aapl-cashflow_lab.xml", "aapl-cashflow-facts.htm",
		map[string]string{roleURI: "CONSOLIDATED STATEMENTS OF CASH FLOWS"})

	// Three duration columns selected from a fixture that also holds the instant
	// cash-balance contexts (which must not become columns).
	require.Len(t, tbl.Columns, 3)
	require.Equal(t, []string{"2024", "2023", "2022"},
		[]string{tbl.Columns[0].Label, tbl.Columns[1].Label, tbl.Columns[2].Label})
	require.False(t, tbl.Columns[0].Instant, "cash flow columns are durations")

	op, ok := rowByConcept(tbl, "us-gaap:NetCashProvidedByUsedInOperatingActivities")
	require.True(t, ok)
	require.Equal(t, RowTotal, op.Type)
	require.InDelta(t, 118254e6, *op.Values[0], 1)

	// Opening and closing balances are instant facts placed into the duration
	// columns at the period boundaries. They tie across years: FY2024's opening
	// equals FY2023's closing.
	begin, ok := rowByLabelContains(tbl, "beginning balances")
	require.True(t, ok)
	end, ok := rowByLabelContains(tbl, "ending balances")
	require.True(t, ok)
	require.InDelta(t, 30737e6, *begin.Values[0], 1)
	require.InDelta(t, 29943e6, *end.Values[0], 1)
	require.InDelta(t, *begin.Values[0], *end.Values[1], 1, "FY2024 opening == FY2023 closing")

	// Every cell resolved across all three periods → high.
	require.Equal(t, "high", tbl.Confidence.Level)
	require.Equal(t, 0, tbl.Confidence.UntaggedCellCount)
}
