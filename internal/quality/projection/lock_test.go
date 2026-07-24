package projection

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestProjectionFileLockSerializesAcrossProcesses(t *testing.T) {
	workspace := t.TempDir()
	writeProjectionTestSource(t, workspace, "chapters/ch1.md", "cross-process lock source")

	holder := projectionLockHelperCommand(t, workspace, "hold")
	stdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	holder.Stderr = os.Stderr
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			_, _ = stdin.Write([]byte{'x'})
			_ = holder.Wait()
		}
	}()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "LOCKED" {
		t.Fatalf("holder signal=%q err=%v", line, err)
	}

	contender := projectionLockHelperCommand(t, workspace, "try-busy")
	output, err := contender.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "BUSY") {
		t.Fatalf("contender output=%q err=%v", output, err)
	}
	if _, err := stdin.Write([]byte{'x'}); err != nil {
		t.Fatal(err)
	}
	if err := holder.Wait(); err != nil {
		t.Fatal(err)
	}
	released = true

	after := projectionLockHelperCommand(t, workspace, "try-free")
	output, err = after.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "ACQUIRED") {
		t.Fatalf("post-release output=%q err=%v", output, err)
	}
}

func TestProjectionFileLockHelper(t *testing.T) {
	mode := os.Getenv("DENOVA_PROJECTION_LOCK_HELPER")
	if mode == "" {
		return
	}
	workspace := os.Getenv("DENOVA_PROJECTION_LOCK_WORKSPACE")
	switch mode {
	case "hold":
		lock, err := acquireProjectionFileLock(workspace, false)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("LOCKED")
		if _, err := bufio.NewReader(os.Stdin).ReadByte(); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
	case "try-busy":
		lock, err := acquireProjectionFileLock(workspace, true)
		if lock != nil {
			lock.Close()
		}
		if !errors.Is(err, ErrProjectionLocked) {
			t.Fatalf("try-busy err=%v", err)
		}
		fmt.Println("BUSY")
	case "try-free":
		lock, err := acquireProjectionFileLock(workspace, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		fmt.Println("ACQUIRED")
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

func projectionLockHelperCommand(t *testing.T, workspace, mode string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestProjectionFileLockHelper$")
	command.Env = append(os.Environ(),
		"DENOVA_PROJECTION_LOCK_HELPER="+mode,
		"DENOVA_PROJECTION_LOCK_WORKSPACE="+workspace,
	)
	return command
}
