package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/kritidutta01/sec-cli/internal/model"
)

// JSON renders the canonical, schema-stable serialization: deterministic field
// order (from the struct tags), HTML left unescaped so free text stays
// readable, and a trailing newline. This is the format the cache stores and the
// differ and Python wrapper read.
type JSON struct{}

// Render writes doc as indented JSON.
func (JSON) Render(doc *model.Document, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("render: encode json: %w", err)
	}
	return nil
}
