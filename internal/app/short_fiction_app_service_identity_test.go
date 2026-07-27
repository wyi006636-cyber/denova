package app

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"denova/internal/shortfiction"
	"denova/internal/workspacechange"
)

func TestFanqieGenerateRejectsDescriptorIdentityRacesBeforeModelCall(t *testing.T) {
	const hiddenSentinel = "hidden-descriptor-sentinel"
	tests := []struct {
		name      string
		workspace func(*testing.T) (string, string)
		openRoot  func(*testing.T, string) shortFictionRootOpener
	}{
		{
			name: "target swapped to hidden symlink",
			workspace: func(t *testing.T) (string, string) {
				workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
				writeShortFictionTestFile(t, workspace, "chapters/short.md", "visible target")
				writeShortFictionTestFile(t, workspace, ".hidden/secret.md", hiddenSentinel)
				return workspace, workspacechange.Revision([]byte(hiddenSentinel))
			},
			openRoot: func(t *testing.T, workspace string) shortFictionRootOpener {
				return swappingShortFictionRootOpener(t, workspace, func(delegate shortFictionRoot, openName, relative string) (*os.File, error) {
					target := filepath.Join(workspace, filepath.FromSlash(relative))
					original := target + ".original"
					if err := os.Rename(target, original); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("../.hidden/secret.md", target); err != nil {
						t.Fatal(err)
					}
					file, err := delegate.Open(openName)
					if removeErr := os.Remove(target); removeErr != nil {
						t.Fatal(removeErr)
					}
					if renameErr := os.Rename(original, target); renameErr != nil {
						t.Fatal(renameErr)
					}
					return file, err
				})
			},
		},
		{
			name: "parent swapped after Lstat before OpenRoot",
			workspace: func(t *testing.T) (string, string) {
				workspace := canonicalShortFictionTestWorkspace(t, t.TempDir())
				writeShortFictionTestFile(t, workspace, "chapters/short.md", "visible parent target")
				writeShortFictionTestFile(t, workspace, ".hidden-parent/short.md", hiddenSentinel)
				return workspace, workspacechange.Revision([]byte(hiddenSentinel))
			},
			openRoot: func(t *testing.T, workspace string) shortFictionRootOpener {
				return parentAfterLstatShortFictionRootOpener(t, workspace)
			},
		},
		{
			name: "workspace root swapped before open",
			workspace: func(t *testing.T) (string, string) {
				root := canonicalShortFictionTestWorkspace(t, t.TempDir())
				workspace := filepath.Join(root, "workspace")
				hiddenWorkspace := filepath.Join(root, "hidden-workspace")
				if err := os.MkdirAll(workspace, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(hiddenWorkspace, 0o755); err != nil {
					t.Fatal(err)
				}
				writeShortFictionTestFile(t, workspace, "chapters/short.md", "visible root target")
				writeShortFictionTestFile(t, hiddenWorkspace, "chapters/short.md", hiddenSentinel)
				return workspace, workspacechange.Revision([]byte(hiddenSentinel))
			},
			openRoot: func(t *testing.T, workspace string) shortFictionRootOpener {
				return func(path string) (shortFictionRoot, error) {
					original := workspace + ".original"
					hiddenWorkspace := filepath.Join(filepath.Dir(workspace), "hidden-workspace")
					if err := os.Rename(workspace, original); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(hiddenWorkspace, workspace); err != nil {
						t.Fatal(err)
					}
					root, err := os.OpenRoot(path)
					if removeErr := os.Remove(workspace); removeErr != nil {
						t.Fatal(removeErr)
					}
					if renameErr := os.Rename(original, workspace); renameErr != nil {
						t.Fatal(renameErr)
					}
					return &shortFictionTestRootAdapter{Root: root}, err
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace, revision := test.workspace(t)
			provider := newShortFictionTestProvider(t, "# forbidden candidate")
			application := newShortFictionTestApp(t, workspace, provider.server.URL)
			application.shortFiction().openRoot = test.openRoot(t, workspace)
			beforeTree := snapshotShortFictionTestTree(t, filepath.Dir(workspace))
			beforeVersions := listShortFictionTestVersions(t, application)
			beforeHistory := listShortFictionTestSessionHistory(t, application)
			logs := captureShortFictionTestLogs(t)

			candidate, err := application.GenerateShortFictionCandidate(context.Background(), shortfiction.GenerateRequest{
				ProfileID: shortfiction.ProfileFanqieShort,
				Source: shortfiction.SourcePacket{
					Workspace:    workspace,
					TargetPath:   "chapters/short.md",
					BaseRevision: revision,
					Brief:        "reject descriptor identity races",
				},
			}, "en-US")
			if err == nil {
				t.Fatal("descriptor identity race was accepted")
			}
			if provider.calls.Load() != 0 {
				t.Fatalf("provider calls = %d, want 0", provider.calls.Load())
			}
			if strings.Contains(candidate.Source+candidate.PreviewMarkdown+err.Error()+logs.String(), hiddenSentinel) {
				t.Fatalf("hidden sentinel escaped authority boundary: candidate=%#v err=%v logs=%q", candidate, err, logs.String())
			}
			assertShortFictionTestObservationsUnchanged(t, application, filepath.Dir(workspace), beforeTree, beforeVersions, beforeHistory)
		})
	}
}

type shortFictionTestRootAdapter struct {
	*os.Root
}

func (r *shortFictionTestRootAdapter) OpenRoot(name string) (shortFictionRoot, error) {
	child, err := r.Root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return &shortFictionTestRootAdapter{Root: child}, nil
}

type parentAfterLstatShortFictionRoot struct {
	shortFictionRoot
	t         *testing.T
	workspace string
	once      sync.Once
	swapped   bool
}

func (r *parentAfterLstatShortFictionRoot) Lstat(name string) (os.FileInfo, error) {
	info, err := r.shortFictionRoot.Lstat(name)
	if err == nil && name == "chapters" {
		r.once.Do(r.swapParent)
	}
	return info, err
}

func (r *parentAfterLstatShortFictionRoot) Open(name string) (*os.File, error) {
	file, err := r.shortFictionRoot.Open(name)
	r.restoreParent()
	return file, err
}

func (r *parentAfterLstatShortFictionRoot) OpenRoot(name string) (shortFictionRoot, error) {
	chained, ok := r.shortFictionRoot.(interface {
		OpenRoot(string) (shortFictionRoot, error)
	})
	if !ok {
		r.t.Fatal("root does not support component OpenRoot")
	}
	child, err := chained.OpenRoot(name)
	r.restoreParent()
	return child, err
}

func (r *parentAfterLstatShortFictionRoot) Close() error {
	r.restoreParent()
	return r.shortFictionRoot.Close()
}

func (r *parentAfterLstatShortFictionRoot) swapParent() {
	parent := filepath.Join(r.workspace, "chapters")
	original := parent + ".original"
	if err := os.Rename(parent, original); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Symlink(".hidden-parent", parent); err != nil {
		r.t.Fatal(err)
	}
	r.swapped = true
}

func (r *parentAfterLstatShortFictionRoot) restoreParent() {
	if !r.swapped {
		return
	}
	r.swapped = false
	parent := filepath.Join(r.workspace, "chapters")
	if err := os.Remove(parent); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Rename(parent+".original", parent); err != nil {
		r.t.Fatal(err)
	}
}

func parentAfterLstatShortFictionRootOpener(t *testing.T, workspace string) shortFictionRootOpener {
	t.Helper()
	return func(path string) (shortFictionRoot, error) {
		if path != workspace {
			t.Fatalf("open root path = %q, want %q", path, workspace)
		}
		root, err := os.OpenRoot(path)
		if err != nil {
			return nil, err
		}
		return &parentAfterLstatShortFictionRoot{
			shortFictionRoot: &shortFictionTestRootAdapter{Root: root},
			t:                t,
			workspace:        workspace,
		}, nil
	}
}

type swappingShortFictionRoot struct {
	shortFictionRoot
	once   *sync.Once
	prefix string
	onOpen func(shortFictionRoot, string, string) (*os.File, error)
}

func (r *swappingShortFictionRoot) Open(name string) (*os.File, error) {
	var (
		file *os.File
		err  error
		ran  bool
	)
	r.once.Do(func() {
		ran = true
		file, err = r.onOpen(r.shortFictionRoot, name, filepath.Join(r.prefix, name))
	})
	if ran {
		return file, err
	}
	return r.shortFictionRoot.Open(name)
}

func (r *swappingShortFictionRoot) OpenRoot(name string) (shortFictionRoot, error) {
	child, err := r.shortFictionRoot.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return &swappingShortFictionRoot{
		shortFictionRoot: child,
		once:             r.once,
		prefix:           filepath.Join(r.prefix, name),
		onOpen:           r.onOpen,
	}, nil
}

func swappingShortFictionRootOpener(
	t *testing.T,
	workspace string,
	onOpen func(shortFictionRoot, string, string) (*os.File, error),
) shortFictionRootOpener {
	t.Helper()
	return func(path string) (shortFictionRoot, error) {
		if path != workspace {
			t.Fatalf("open root path = %q, want %q", path, workspace)
		}
		root, err := os.OpenRoot(path)
		if err != nil {
			return nil, err
		}
		return &swappingShortFictionRoot{
			shortFictionRoot: &shortFictionTestRootAdapter{Root: root},
			once:             &sync.Once{},
			onOpen:           onOpen,
		}, nil
	}
}

func captureShortFictionTestLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})
	return &logs
}
