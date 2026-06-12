package diff

import (
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Lexical returns a human-readable word-level diff between two paragraph
// texts. Equal spans are shown in-line; insertions are wrapped in [+...+] and
// deletions in [-...-]. The result is suitable for the --layer lexical view
// where an analyst wants to compare specific wording changes.
func Lexical(prev, curr string) string {
	dmp := diffmatchpatch.New()
	wPrev, wCurr, lines := dmp.DiffLinesToChars(prev, curr)
	diffs := dmp.DiffMain(wPrev, wCurr, false)
	diffs = dmp.DiffCharsToLines(diffs, lines)
	diffs = dmp.DiffCleanupSemantic(diffs)

	var b strings.Builder
	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			fmt.Fprintf(&b, "[+%s+]", d.Text)
		case diffmatchpatch.DiffDelete:
			fmt.Fprintf(&b, "[-%s-]", d.Text)
		case diffmatchpatch.DiffEqual:
			b.WriteString(d.Text)
		}
	}
	return b.String()
}

// LexicalSectionDiff is the lexical-layer change to one section: the section
// coordinates and the word-level diff strings for paragraphs that changed.
type LexicalSectionDiff struct {
	Item       string             `json:"item,omitempty"`
	Title      string             `json:"title,omitempty"`
	Paragraphs []LexicalParagraph `json:"paragraphs,omitempty"`
}

// LexicalParagraph is one paragraph's word-level diff. Text carries the
// annotated form: equal spans are plain, insertions are [+..+], deletions [-...-].
type LexicalParagraph struct {
	Text string `json:"text"`
}

// LexicalSections produces a word-level diff for the modified sections from
// a structural ChangeSet. Added/removed sections are omitted (their full text
// appears in the structural report); only sections with at least one changed
// paragraph are included.
func LexicalSections(cs *ChangeSet, prev, curr []string) []LexicalSectionDiff {
	// Build prev/curr paragraph maps by item id.
	type sectionText struct{ item, title string }
	prevByItem := make(map[string]sectionText, len(prev))
	currByItem := make(map[string]sectionText, len(curr))

	for _, sd := range cs.Sections {
		// We only annotate sections that the structural layer classified as
		// changed (not added or removed-only).
		hasPrev := false
		hasCurr := false
		for _, p := range sd.Paragraphs {
			switch p.Kind {
			case Removed:
				hasPrev = true
			case Added:
				hasCurr = true
			}
		}
		if hasPrev && hasCurr {
			prevByItem[sd.Item] = sectionText{item: sd.Item, title: sd.Title}
			currByItem[sd.Item] = sectionText{item: sd.Item, title: sd.Title}
		}
	}
	_ = prevByItem
	_ = currByItem

	// For the lexical layer, we diff the full paragraph texts in the ChangeSet.
	var out []LexicalSectionDiff
	for _, sd := range cs.Sections {
		// Collect removed and added paragraphs; pair them positionally.
		var removed, added []string
		for _, p := range sd.Paragraphs {
			switch p.Kind {
			case Removed:
				removed = append(removed, p.Text)
			case Added:
				added = append(added, p.Text)
			}
		}
		if len(removed) == 0 || len(added) == 0 {
			continue
		}
		// Pair up paragraphs; extras go as empty-vs-text.
		n := len(removed)
		if len(added) > n {
			n = len(added)
		}
		lsd := LexicalSectionDiff{Item: sd.Item, Title: sd.Title}
		for i := 0; i < n; i++ {
			var p, c string
			if i < len(removed) {
				p = removed[i]
			}
			if i < len(added) {
				c = added[i]
			}
			lsd.Paragraphs = append(lsd.Paragraphs, LexicalParagraph{
				Text: Lexical(p, c),
			})
		}
		out = append(out, lsd)
	}
	return out
}
