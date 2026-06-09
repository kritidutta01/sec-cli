package diff

import "errors"

// ErrSemanticNotImplemented is returned when --layer semantic is requested.
// Semantic (embedding-distance) ranking is deferred to v1.0.1: shipping a
// half-baked embedding pipeline would compromise the "no LLM dependency in the
// core" principle. The structural layer surfaces what changed; the lexical
// layer shows exactly how. The semantic ranking layer (which paragraphs changed
// the most in meaning, noise-filtered) is a planned follow-up.
var ErrSemanticNotImplemented = errors.New(
	"sec-cli: --layer semantic is not yet implemented; use --layer structural (default) or --layer lexical. " +
		"Semantic embedding-distance ranking is planned for v1.0.1.",
)
