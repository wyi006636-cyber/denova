package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"denova/config"
	"denova/internal/book"
	"denova/internal/workspace/documentreview"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

const (
	maxAgentReviewCommentItems = 32
	maxAgentReviewQuoteBytes   = 32 * 1024
	maxAgentReviewContextBytes = 8 * 1024
	maxAgentReviewBodyBytes    = 64 * 1024
	maxAgentReviewFileBytes    = 8 * 1024 * 1024
)

type addReviewCommentsInput struct {
	Items []addReviewCommentItem `json:"items,omitempty" jsonschema_description:"Independent review comments. Each item is attempted separately; one invalid quote does not prevent other items from being created."`
}

type addReviewCommentItem struct {
	Path   string `json:"path" jsonschema_description:"Workspace-relative chapter or text file path. Absolute paths and paths outside the current Project are rejected."`
	Quote  string `json:"quote" jsonschema_description:"Exact UTF-8 text copied from the current file. It must occur exactly once, or prefix/suffix context must identify one occurrence."`
	Prefix string `json:"prefix,omitempty" jsonschema_description:"Optional exact text immediately before quote to disambiguate repeated passages."`
	Suffix string `json:"suffix,omitempty" jsonschema_description:"Optional exact text immediately after quote to disambiguate repeated passages."`
	Body   string `json:"body" jsonschema_description:"Actionable review feedback for the author."`
}

type addReviewCommentSuccess struct {
	Path      string `json:"path"`
	CommentID string `json:"comment_id"`
	Quote     string `json:"quote"`
}

type addReviewCommentFailure struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type addReviewCommentsResult struct {
	Created []addReviewCommentSuccess `json:"created"`
	Failed  []addReviewCommentFailure `json:"failed"`
}

// newDocumentReviewTools exposes the Writing Agent's only document-review
// mutation. It is intentionally kept out of the Game and General factories.
func newDocumentReviewTools(workspace, projectStoreRoot string) ([]agenttool.ToolDefinition, error) {
	tool, err := agenttool.InferTool("add_review_comments", "Add multiple Agent review comments anchored to exact passages in current workspace chapter or text files. Each item is independent: successful items are persisted and failed items return a concise English correction. This tool never edits document content and never accepts lore items.", func(ctx context.Context, input addReviewCommentsInput) (agentschema.ToolResult, error) {
		if err := validateAddReviewCommentsInput(input); err != nil {
			return agentschema.ToolResult{}, err
		}
		if strings.TrimSpace(workspace) == "" {
			return agentschema.ToolResult{}, fmt.Errorf("cannot add review comments because the current workspace is unavailable")
		}
		if strings.TrimSpace(projectStoreRoot) == "" {
			return agentschema.ToolResult{}, fmt.Errorf("cannot add review comments because the Project Store is unavailable")
		}
		files := book.NewService(workspace)
		service, serviceErr := documentreview.ForWorkspaceAt(workspace, projectStoreRoot)
		if serviceErr != nil {
			return agentschema.ToolResult{}, fmt.Errorf("open document review ledger: %w", serviceErr)
		}
		scope := WorkspaceChangeScopeFromContext(ctx)
		result := addReviewCommentsResult{
			Created: make([]addReviewCommentSuccess, 0, len(input.Items)),
			Failed:  make([]addReviewCommentFailure, 0),
		}
		for _, item := range input.Items {
			path := strings.TrimSpace(item.Path)
			if err := validateReviewCommentItem(item); err != nil {
				result.Failed = append(result.Failed, addReviewCommentFailure{Path: path, Message: err.Error()})
				continue
			}
			if err := validateReviewableFilePath(path); err != nil {
				result.Failed = append(result.Failed, addReviewCommentFailure{Path: path, Message: err.Error()})
				continue
			}
			content, revision, readErr := files.ReadFileWithRevision(path)
			if readErr != nil {
				result.Failed = append(result.Failed, addReviewCommentFailure{Path: path, Message: "cannot read review target file; verify that the path names an existing text file"})
				continue
			}
			if len(content) > maxAgentReviewFileBytes || !utf8.ValidString(content) || strings.IndexByte(content, 0) >= 0 {
				result.Failed = append(result.Failed, addReviewCommentFailure{Path: path, Message: "review target must be a UTF-8 text file within the size limit"})
				continue
			}
			anchor, anchorErr := documentreview.AnchorFromQuote(documentreview.Snapshot{Content: content, Revision: revision}, item.Quote, item.Prefix, item.Suffix)
			if anchorErr != nil {
				result.Failed = append(result.Failed, addReviewCommentFailure{Path: path, Message: anchorErr.Error()})
				continue
			}
			_, comment, addErr := service.AddAgentComment(ctx, documentreview.AddCommentRequest{
				Target: documentreview.Target{Kind: documentreview.TargetKindWorkspaceFile, ID: path},
				Body:   item.Body,
				Anchor: anchor,
			}, documentreview.Snapshot{Content: content, Revision: revision}, documentreview.AgentCommentMetadata{
				SessionID: scope.SessionID,
				RunID:     scope.RunID,
			})
			if addErr != nil {
				result.Failed = append(result.Failed, addReviewCommentFailure{Path: path, Message: addErr.Error()})
				continue
			}
			result.Created = append(result.Created, addReviewCommentSuccess{Path: path, CommentID: comment.ID, Quote: anchor.DisplayQuote})
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return agentschema.ToolResult{}, fmt.Errorf("encode review comment result: %w", err)
		}
		toolResult := agentschema.TextToolResult(string(encoded))
		toolResult.Details = encoded
		return toolResult, nil
	})
	if err != nil {
		return nil, err
	}
	definition, err := defineTool(tool, documentReviewDescriptor(config.AgentToolDocumentReview, agenttool.ToolRecoveryReconcilable))
	if err != nil {
		return nil, err
	}
	return []agenttool.ToolDefinition{definition}, nil
}

func validateAddReviewCommentsInput(input addReviewCommentsInput) error {
	if len(input.Items) == 0 {
		return fmt.Errorf("items must contain at least one review comment")
	}
	if len(input.Items) > maxAgentReviewCommentItems {
		return fmt.Errorf("items exceeds the maximum of %d review comments", maxAgentReviewCommentItems)
	}
	return nil
}

func validateReviewCommentItem(item addReviewCommentItem) error {
	if strings.TrimSpace(item.Path) == "" {
		return fmt.Errorf("path is required")
	}
	if item.Quote == "" || len(item.Quote) > maxAgentReviewQuoteBytes {
		return fmt.Errorf("quote must be between 1 and %d bytes", maxAgentReviewQuoteBytes)
	}
	if len(item.Prefix) > maxAgentReviewContextBytes || len(item.Suffix) > maxAgentReviewContextBytes {
		return fmt.Errorf("prefix and suffix must be at most %d bytes", maxAgentReviewContextBytes)
	}
	if strings.TrimSpace(item.Body) == "" || len(item.Body) > maxAgentReviewBodyBytes {
		return fmt.Errorf("body must be between 1 and %d bytes", maxAgentReviewBodyBytes)
	}
	return nil
}

func validateReviewableFilePath(value string) error {
	normalized, err := documentreview.NormalizeTarget(documentreview.Target{Kind: documentreview.TargetKindWorkspaceFile, ID: value})
	if err != nil {
		return fmt.Errorf("invalid review target path: %v", err)
	}
	ext := strings.ToLower(filepath.Ext(normalized.ID))
	switch ext {
	case ".md", ".markdown", ".txt", ".text":
		return nil
	default:
		return fmt.Errorf("review comments support chapter and text files only (got %q)", normalized.ID)
	}
}
