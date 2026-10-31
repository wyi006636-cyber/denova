package app

import (
	"context"
	"fmt"
	"strings"

	agentrun "denova/internal/agents/run"
	apptask "denova/internal/app/task"
	projectdomain "denova/internal/project"
)

// StartWritingComparisonRun owns the detached task used by comparison event
// streams. It is separate from foreground writing sessions and remains
// addressable after completion for replay until the App exits.
func (a *App) StartWritingComparisonRun(ctx context.Context, layout projectdomain.Layout, taskID string, run func(context.Context, func(agentrun.Event))) (*apptask.Task, error) {
	if a == nil || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("comparison task identity is required")
	}
	var display *apptask.Task
	display, err := apptask.NewDeferredWithContext(ctx, func(task *apptask.Task) error {
		if err := a.registerProjectTask(task, layout.ProjectID, layout.ContentRoot, layout.StoreRoot); err != nil {
			return err
		}
		a.mu.Lock()
		if a.writingComparisonTasks == nil {
			a.writingComparisonTasks = make(map[string]*apptask.Task)
		}
		a.writingComparisonTasks[comparisonTaskKey(layout.ProjectID, taskID)] = task
		a.mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := display.Start(func(taskCtx context.Context, task *apptask.Task, emit func(agentrun.Event)) {
		defer a.unregisterProjectTask(task)
		if run != nil {
			run(taskCtx, emit)
		}
	}); err != nil {
		a.unregisterProjectTask(display)
		return nil, err
	}
	return display, nil
}

func (a *App) WritingComparisonDisplayTask(projectID, taskID string) *apptask.Task {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.writingComparisonTasks[comparisonTaskKey(projectID, taskID)]
}

func comparisonTaskKey(projectID, taskID string) string {
	return strings.TrimSpace(projectID) + "\x00" + strings.TrimSpace(taskID)
}
