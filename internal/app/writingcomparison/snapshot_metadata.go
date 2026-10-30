package writingcomparison

import (
	"context"
	"fmt"

	projectdomain "denova/internal/project"
)

// ResolveTargetMetadata is the small, side-effect-free part of snapshot
// preparation used by task creation. Full context freezing belongs to T4.
func ResolveTargetMetadata(_ context.Context, layout projectdomain.Layout, targetPath string) (TargetRef, error) {
	if layout.ProjectID == "" {
		return TargetRef{}, fmt.Errorf("Project scope is required")
	}
	return ValidateTarget(layout, targetPath)
}
