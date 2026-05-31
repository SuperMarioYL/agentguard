// Package corpus exposes the YAML payload-rules file that the detector
// loads at start-up.  It exists as its own tiny package because go:embed
// cannot reference files above the directive's package directory — the
// data file lives at corpus/payloads.yaml, so the embed host has to live
// alongside it.
package corpus

import _ "embed"

// Bytes is the raw YAML corpus.  Parsing is the detector's job; this
// package keeps no opinions about the schema.
//
//go:embed payloads.yaml
var Bytes []byte
