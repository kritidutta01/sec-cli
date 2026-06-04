package ixbrl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// readFixture returns the raw bytes of a testdata file.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

// conceptByName returns the role concept with the given taxonomy name.
func conceptByName(role LinkbaseRole, name string) (RoleConcept, bool) {
	for _, c := range role.Concepts {
		if c.Name == name {
			return c, true
		}
	}
	return RoleConcept{}, false
}

func TestParseLinkbase_Synthetic(t *testing.T) {
	roles, err := ParseLinkbase(
		readFixture(t, "synthetic-income_pre.xml"),
		readFixture(t, "synthetic-income_lab.xml"),
		nil,
	)
	require.NoError(t, err)
	require.Len(t, roles, 1)

	role := roles[0]
	require.Equal(t, "http://acme.example/role/StatementsOfIncome", role.RoleURI)
	// No schema titles supplied → title derived from the role URI tail.
	require.Equal(t, "StatementsOfIncome", role.Title)

	// Presentation order, depth-first by arc order, including the structural root.
	wantOrder := []string{
		"acme:IncomeStatementAbstract",
		"acme:Revenues",
		"acme:CostOfSales",
		"acme:GrossProfit",
		"acme:CostsAndExpenses",
		"acme:ResearchExpense",
		"acme:OperatingExpenses",
		"acme:NetIncome",
	}
	got := make([]string, len(role.Concepts))
	for i, c := range role.Concepts {
		got[i] = c.Name
	}
	require.Equal(t, wantOrder, got)

	// The abstract root is structural; line items are not.
	root, _ := conceptByName(role, "acme:IncomeStatementAbstract")
	require.True(t, root.Structural)
	require.True(t, root.HasChildren)
	require.Equal(t, 0, root.Depth)

	rev, _ := conceptByName(role, "acme:Revenues")
	require.False(t, rev.Structural)
	require.False(t, rev.HasChildren)
	require.Equal(t, 1, rev.Depth)
	// Preferred label is terse → "Revenue", not the standard "Revenue, net".
	require.Equal(t, "Revenue", rev.Label)

	// A non-abstract parent that carries its own value: nested one level deeper.
	ce, _ := conceptByName(role, "acme:CostsAndExpenses")
	require.True(t, ce.HasChildren)
	require.False(t, ce.Structural)
	require.Equal(t, "Costs and expenses", ce.Label)

	rd, _ := conceptByName(role, "acme:ResearchExpense")
	require.Equal(t, 2, rd.Depth)

	// Total-label preferred role resolves the filing's wording.
	opex, _ := conceptByName(role, "acme:OperatingExpenses")
	require.Equal(t, roleTotalLabel, opex.PreferredLabel)
	require.Equal(t, "Total operating expenses", opex.Label)
}

func TestParseRoleTitles(t *testing.T) {
	xsd := []byte(`<?xml version="1.0"?>
<xsd:schema xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:link="http://www.xbrl.org/2003/linkbase">
  <xsd:annotation><xsd:appinfo>
    <link:roleType roleURI="http://acme.example/role/StatementsOfIncome" id="r1">
      <link:definition>9952151 - Statement - CONSOLIDATED STATEMENTS OF OPERATIONS</link:definition>
      <link:usedOn>link:presentationLink</link:usedOn>
    </link:roleType>
    <link:roleType roleURI="http://acme.example/role/BalanceSheet" id="r2">
      <link:definition>9952153 - Statement - CONSOLIDATED BALANCE SHEETS</link:definition>
      <link:usedOn>link:presentationLink</link:usedOn>
    </link:roleType>
  </xsd:appinfo></xsd:annotation>
</xsd:schema>`)

	titles, err := ParseRoleTitles(xsd)
	require.NoError(t, err)
	require.Equal(t, "CONSOLIDATED STATEMENTS OF OPERATIONS", titles["http://acme.example/role/StatementsOfIncome"])
	require.Equal(t, "CONSOLIDATED BALANCE SHEETS", titles["http://acme.example/role/BalanceSheet"])
}

func TestParseLinkbase_AAPLIncome(t *testing.T) {
	const roleURI = "http://www.apple.com/role/CONSOLIDATEDSTATEMENTSOFOPERATIONS"
	roles, err := ParseLinkbase(
		readFixture(t, "aapl-income_pre.xml"),
		readFixture(t, "aapl-income_lab.xml"),
		map[string]string{roleURI: "CONSOLIDATED STATEMENTS OF OPERATIONS"},
	)
	require.NoError(t, err)
	require.Len(t, roles, 1)

	role := roles[0]
	require.Equal(t, roleURI, role.RoleURI)
	require.Equal(t, "CONSOLIDATED STATEMENTS OF OPERATIONS", role.Title)

	// Filing-specific labels resolved from the preferred label roles.
	rev, ok := conceptByName(role, "us-gaap:RevenueFromContractWithCustomerExcludingAssessedTax")
	require.True(t, ok)
	require.Equal(t, "Net sales", rev.Label)
	require.False(t, rev.Structural)

	gp, _ := conceptByName(role, "us-gaap:GrossProfit")
	require.Equal(t, "Gross margin", gp.Label)
	require.Equal(t, roleTotalLabel, gp.PreferredLabel)

	opex, _ := conceptByName(role, "us-gaap:OperatingExpenses")
	require.Equal(t, "Total operating expenses", opex.Label)

	// Dimensional and abstract nodes are flagged structural.
	for _, name := range []string{
		"us-gaap:StatementTable",
		"srt:ProductOrServiceAxis",
		"us-gaap:ProductMember",
		"us-gaap:StatementLineItems",
		"us-gaap:OperatingExpensesAbstract",
	} {
		c, ok := conceptByName(role, name)
		require.True(t, ok, "expected %s in role", name)
		require.True(t, c.Structural, "%s should be structural", name)
	}
}
