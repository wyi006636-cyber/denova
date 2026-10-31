package writingtemplates

import "embed"

// builtinFS is immutable release material. User files are persisted below
// presetlayout.WritingTemplates and can override an ID without changing this
// embedded source.
//
//go:embed builtin/*.json
var builtinFS embed.FS
