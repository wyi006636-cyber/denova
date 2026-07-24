package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"denova/internal/buildinfo"
	"denova/internal/quality/projection"
)

// runCommand dispatches commands that must complete before normal application
// configuration, Agent, HTTP, browser, or frontend startup.
func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer, startApplication func()) int {
	if len(args) > 0 && args[0] == "reindex" {
		return runReindexCommand(ctx, args[1:], stdout, stderr)
	}
	if hasVersionArg(args) {
		fmt.Fprintln(stdout, buildinfo.Version)
		return 0
	}
	startApplication()
	return 0
}

func runReindexCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("denova reindex", flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspace := flags.String("workspace", "", "workspace path / 作品工作目录")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "错误 / Error: unexpected reindex arguments: %v\n", flags.Args())
		return 2
	}
	if *workspace == "" {
		fmt.Fprintln(stderr, "错误 / Error: reindex requires --workspace <path>")
		return 2
	}

	service, err := projection.NewService(projection.Options{
		Workspace:          *workspace,
		WorkspaceInspector: projection.WorkspaceSchemaInspectorOptions(buildinfo.Version),
	})
	if err != nil {
		fmt.Fprintf(stderr, "投影重建失败 / Projection rebuild failed: %v\n", err)
		return 1
	}
	result, err := service.Rebuild(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "投影重建失败 / Projection rebuild failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "投影重建完成 / Projection rebuilt")
	fmt.Fprintf(stdout, "工作区 / Workspace: %s\n", filepath.Dir(filepath.Dir(result.DatabasePath)))
	fmt.Fprintf(stdout, "数据库 / Database: %s\n", result.DatabasePath)
	fmt.Fprintf(stdout, "文档 / Documents: %d\n", result.DocumentCount)
	fmt.Fprintf(stdout, "源快照 / Source snapshot: %s\n", result.SourceSnapshotHash)
	fmt.Fprintf(stdout, "SQLite: %s\n", result.SQLiteVersion)
	if len(result.QuarantinePaths) > 0 {
		fmt.Fprintf(stdout, "隔离诊断 / Quarantined diagnostics: %d\n", len(result.QuarantinePaths))
		for _, path := range result.QuarantinePaths {
			fmt.Fprintf(stdout, "- %s\n", path)
		}
	}
	return 0
}
