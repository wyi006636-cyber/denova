package yanzhouadapter

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestToolHandlerPanicReturnsError(t *testing.T) {
	if os.Getenv("DENOVA_TOOL_PANIC_HELPER") == "1" {
		target := ToolTarget{SchemaVersion: "1", Kind: "chapter", BookID: "book-1", TargetID: "chapter-1"}
		harness, err := NewToolHarness(ToolCapabilityManifest{
			SchemaVersion: "1", RunID: "run-panic", AgentID: "primary-writer", Target: target,
			DeniedByDefault: true,
			Capabilities: []ToolCapability{{
				ID: "story.get_target", Mode: ToolCapabilityRead, MaxCalls: 1, MaxResultBytes: 1024,
			}},
		}, map[string]ToolHandler{
			"story.get_target": func(context.Context, map[string]any) (ToolResult, error) {
				panic("fixture panic")
			},
		}, 1)
		if err != nil {
			t.Fatal(err)
		}
		_, err = harness.Execute(context.Background(), ToolCall{
			RunID: "run-panic", AgentID: "primary-writer", Target: target,
			ToolID: "story.get_target", Arguments: `{}`,
		}, ToolExecutionOptions{Timeout: 100 * time.Millisecond})
		if err == nil {
			t.Fatal("panicking tool handler returned no error")
		}
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestToolHandlerPanicReturnsError$")
	command.Env = append(os.Environ(), "DENOVA_TOOL_PANIC_HELPER=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("tool handler panic escaped its execution boundary: %v\n%s", err, output)
	}
}
