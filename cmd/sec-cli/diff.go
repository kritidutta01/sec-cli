package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kritidutta01/sec-cli/internal/diff"
	"github.com/kritidutta01/sec-cli/internal/pipeline"
)

// diffCmd compares two filings of the same company — what moved in the numbers
// and what changed in the words. It runs the Phase 11 pipeline twice (each
// result likely served from the parsed cache), then diffs the two assembled
// documents and renders the change set. The diff itself never fetches or
// re-parses; it is a pure function over the two documents.
func diffCmd() *cobra.Command {
	var (
		form   string
		from   int
		to     int
		format string
		layer  string
	)
	cmd := &cobra.Command{
		Use:   "diff <ticker>",
		Short: "Compare two filings: numeric deltas by concept, text changes by section",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if from == 0 || to == 0 {
				return fmt.Errorf("both --from and --to are required (filing years)")
			}

			diffLayer := diff.Layer(layer)
			// Semantic is stubbed: return the clear error with exit code 64 (EX_USAGE).
			if diffLayer == diff.LayerSemantic {
				fmt.Fprintln(os.Stderr, diff.ErrSemanticNotImplemented.Error())
				os.Exit(64)
			}
			if diffLayer != diff.LayerStructural && diffLayer != diff.LayerLexical {
				return fmt.Errorf("unknown --layer %q (choose structural, lexical, semantic)", layer)
			}

			c, err := openCache(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()

			ctx := cmd.Context()
			prev, err := pipeline.Run(ctx, pipeline.Options{Ticker: args[0], Form: form, Year: from, Cache: c})
			if err != nil {
				return fmt.Errorf("from %d: %w", from, err)
			}
			curr, err := pipeline.Run(ctx, pipeline.Options{Ticker: args[0], Form: form, Year: to, Cache: c})
			if err != nil {
				return fmt.Errorf("to %d: %w", to, err)
			}

			cs, lexical, err := diff.WithLayer(prev, curr, diffLayer)
			if err != nil {
				if errors.Is(err, diff.ErrSemanticNotImplemented) {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(64)
				}
				return err
			}

			if diffLayer == diff.LayerLexical {
				return diff.LexicalRenderer{}.Render(cs, lexical, os.Stdout)
			}

			renderer, err := diff.RendererFor(format)
			if err != nil {
				return err
			}
			return renderer.Render(cs, os.Stdout)
		},
	}
	cmd.Flags().StringVarP(&form, "type", "t", "10-K", "filing form type (e.g. 10-K, 10-Q, 8-K)")
	cmd.Flags().IntVar(&from, "from", 0, "earlier filing year")
	cmd.Flags().IntVar(&to, "to", 0, "later filing year")
	cmd.Flags().StringVarP(&format, "output", "o", "json", "output format (json, md)")
	cmd.Flags().StringVarP(&format, "format", "f", "json", "output format (alias for --output)")
	cmd.Flags().StringVar(&layer, "layer", "structural", "diff layer: structural (default), lexical, semantic")
	return cmd
}
