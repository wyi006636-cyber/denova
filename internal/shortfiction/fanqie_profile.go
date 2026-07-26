package shortfiction

import "strings"

func FanqieSystemPrompt() string {
	return `Write one complete Markdown story in Chinese for the Fanqie short-fiction profile.

Build the story around a clear premise, a protagonist desire, immediate pressure, an early hook, and a satisfying payoff. Return only the story itself. Do not use a code fence. Do not claim to write to a workspace or use tools.`
}

func FormatSourcePacket(source SourcePacket) string {
	var builder strings.Builder
	builder.WriteString("Brief:\n")
	builder.WriteString(source.Brief)
	if source.Source != "" {
		builder.WriteString("\n\nSource:\n")
		builder.WriteString(source.Source)
	}
	return builder.String()
}
