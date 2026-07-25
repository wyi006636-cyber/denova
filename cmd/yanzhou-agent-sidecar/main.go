package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"denova/internal/yanzhouadapter"
	"denova/internal/yanzhouprotocol"
)

const bootstrapTokenEnv = "YANZHOU_BOOTSTRAP_TOKEN"

type synchronizedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (writer *synchronizedWriter) Write(value []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.writer.Write(value)
}

func main() {
	logger := log.New(os.Stderr, "yanzhou-agent-sidecar: ", 0)
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		logger.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	stdout = &synchronizedWriter{writer: stdout}
	flags := flag.NewFlagSet("yanzhou-agent-sidecar", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	runtimeRoot := flags.String("runtime-root", "", "isolated Yanzhou app-data runtime directory")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	preparedRuntimeRoot, err := prepareRuntimeRoot(*runtimeRoot)
	if err != nil {
		return err
	}

	token := os.Getenv(bootstrapTokenEnv)
	gate, err := yanzhouadapter.NewBootstrapTokenGate(token)
	if err != nil {
		return err
	}
	_ = os.Unsetenv(bootstrapTokenEnv)
	token = ""

	policy := yanzhouadapter.NewBootstrapPolicy()
	if _, err := yanzhouadapter.SmokeBuildBootstrap(ctx, policy); err != nil {
		return fmt.Errorf("bootstrap builder smoke failed: %w", err)
	}

	reader := yanzhouprotocol.NewReader(stdin, yanzhouprotocol.DefaultMaxFrameBytes)
	frame, err := reader.ReadFrame()
	if err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	if frame.Kind != yanzhouprotocol.KindHandshakeRequest {
		return fmt.Errorf("first frame must be %s", yanzhouprotocol.KindHandshakeRequest)
	}

	var request yanzhouprotocol.HandshakeRequest
	if err := json.Unmarshal(frame.Payload, &request); err != nil {
		return fmt.Errorf("decode handshake: %w", err)
	}
	response, err := yanzhouadapter.Handshake(request, gate, provenanceFromEnvironment(), sidecarBuild())
	if err != nil {
		return fmt.Errorf("handshake rejected: %w", err)
	}
	responsePayload, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode handshake response: %w", err)
	}
	if err := yanzhouprotocol.WriteFrame(stdout, yanzhouprotocol.Envelope{
		Kind:            yanzhouprotocol.KindHandshakeResponse,
		ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID:       frame.RequestID,
		Payload:         responsePayload,
	}); err != nil {
		return fmt.Errorf("write handshake response: %w", err)
	}

	eventStore, err := yanzhouadapter.NewFileRuntimeEventStore(preparedRuntimeRoot)
	if err != nil {
		return fmt.Errorf("open runtime event store: %w", err)
	}
	defer eventStore.Close()
	planRuntime, err := yanzhouadapter.NewPlanFrameRuntime(eventStore, nil)
	if err != nil {
		return fmt.Errorf("initialize plan runtime: %w", err)
	}
	writingRuntime, err := yanzhouadapter.NewWritingFrameRuntime(eventStore, nil)
	if err != nil {
		return fmt.Errorf("initialize writing runtime: %w", err)
	}
	planModeNegotiated := containsFeature(response.SupportedFeatures, "plan-mode")
	writingHarnessNegotiated := containsFeature(response.SupportedFeatures, "writing-harness")
	var writingRuns sync.WaitGroup
	defer writingRuns.Wait()
	for {
		frame, err := reader.ReadFrame()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read post-handshake frame: %w", err)
		}
		if frame.Kind == yanzhouprotocol.KindToolResponse {
			if !writingHarnessNegotiated || writingRuntime.HandleToolResponse(frame) != nil {
				if err := writeRuntimeError(stdout, frame, "tool_response_rejected"); err != nil {
					return err
				}
			}
			continue
		}
		if frame.Kind == yanzhouprotocol.KindRunStart {
			planMode, decodeErr := runStartPlanMode(frame.Payload)
			switch {
			case decodeErr != nil:
				if err := writeRuntimeError(stdout, frame, "run_start_invalid"); err != nil {
					return err
				}
			case planMode && planModeNegotiated:
				if err := planRuntime.HandleFrame(ctx, frame, stdout); err != nil {
					if writeErr := writeRuntimeError(stdout, frame, "plan_frame_rejected"); writeErr != nil {
						return writeErr
					}
				}
			case !planMode && writingHarnessNegotiated:
				writingRuns.Add(1)
				go func(input yanzhouprotocol.Envelope) {
					defer writingRuns.Done()
					if handleErr := writingRuntime.HandleFrame(ctx, input, stdout); handleErr != nil {
						_ = writeRuntimeError(stdout, input, "writing_frame_rejected")
					}
				}(frame)
			default:
				if err := writeRuntimeError(stdout, frame, "feature_not_negotiated"); err != nil {
					return err
				}
			}
			continue
		}
		if !planModeNegotiated || frame.Kind != yanzhouprotocol.KindRunResume {
			if err := writeRuntimeError(stdout, frame, "feature_not_negotiated"); err != nil {
				return err
			}
			continue
		}
		if err := planRuntime.HandleFrame(ctx, frame, stdout); err != nil {
			if writeErr := writeRuntimeError(stdout, frame, "plan_frame_rejected"); writeErr != nil {
				return writeErr
			}
		}
	}
}

func runStartPlanMode(payload json.RawMessage) (bool, error) {
	var value struct {
		PlanMode *bool `json:"planMode"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&value); err != nil || value.PlanMode == nil {
		return false, errors.New("run.start planMode is invalid")
	}
	return *value.PlanMode, nil
}

func containsFeature(features []string, expected string) bool {
	for _, feature := range features {
		if feature == expected {
			return true
		}
	}
	return false
}

func writeRuntimeError(output io.Writer, input yanzhouprotocol.Envelope, code string) error {
	payload, err := json.Marshal(map[string]string{
		"code":    code,
		"message": "sidecar frame was rejected",
	})
	if err != nil {
		return fmt.Errorf("encode runtime error: %w", err)
	}
	frame := yanzhouprotocol.Envelope{
		Kind:            yanzhouprotocol.KindRuntimeError,
		ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID:       input.RequestID,
		RunID:           input.RunID,
		Payload:         payload,
	}
	if err := yanzhouprotocol.WriteFrame(output, frame); err != nil {
		return fmt.Errorf("write runtime error: %w", err)
	}
	return nil
}

func prepareRuntimeRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("runtime root must be an absolute isolated app-data or temp path")
	}
	clean := filepath.Clean(root)
	if clean == string(filepath.Separator) {
		return "", fmt.Errorf("runtime root cannot be the filesystem root")
	}
	if err := os.MkdirAll(clean, 0o700); err != nil {
		return "", fmt.Errorf("create runtime root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("resolve runtime root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("runtime root is not a directory")
	}
	return resolved, nil
}

func provenanceFromEnvironment() yanzhouprotocol.Provenance {
	return yanzhouprotocol.Provenance{
		SchemaVersion:      "1",
		UpstreamRepository: os.Getenv("YANZHOU_UPSTREAM_REPOSITORY"),
		UpstreamBaseSHA:    os.Getenv("YANZHOU_UPSTREAM_BASE_SHA"),
		AdapterCommitSHA:   os.Getenv("YANZHOU_ADAPTER_COMMIT_SHA"),
		SourceTreeSHA:      os.Getenv("YANZHOU_SOURCE_TREE_SHA"),
		BinarySHA256:       os.Getenv("YANZHOU_BINARY_SHA256"),
		SkillsManifestSHA:  os.Getenv("YANZHOU_SKILLS_MANIFEST_SHA"),
		GoVersion:          runtime.Version(),
		TargetOS:           runtime.GOOS,
		TargetArch:         runtime.GOARCH,
		BuiltAt:            os.Getenv("YANZHOU_BUILT_AT"),
	}
}

func sidecarBuild() string {
	if build := strings.TrimSpace(os.Getenv("YANZHOU_SIDECAR_BUILD")); build != "" {
		return build
	}
	return "development"
}
