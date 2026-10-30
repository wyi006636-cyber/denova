package writingtemplates

import "embed"

// builtinFS is immutable release material. User files are materialized into
// presetlayout.WritingTemplates and can override an ID without changing this
// embedded source.
//
//go:embed builtin/*.json
var builtinFS embed.FS
