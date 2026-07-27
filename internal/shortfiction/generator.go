package shortfiction

import "context"

// GenerateRequest identifies the fixed profile and source for one preview generation.
type GenerateRequest struct {
	ProfileID ProfileID    `json:"profile_id"`
	Source    SourcePacket `json:"source"`
}

// Generator produces preview Markdown but has no authority to write it.
type Generator interface {
	Generate(context.Context, SourcePacket) (Generation, error)
}
