package main

import (
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

	"denova/internal/yanzhouadapter"
	"denova/internal/yanzhouprotocol"
)

const bootstrapTokenEnv = "YANZHOU_BOOTSTRAP_TOKEN"

func main() {
	logger := log.New(os.Stderr, "yanzhou-agent-sidecar: ", 0)
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		logger.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("yanzhou-agent-sidecar", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	runtimeRoot := flags.String("runtime-root", "", "isolated Yanzhou app-data runtime directory")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if _, err := prepareRuntimeRoot(*runtimeRoot); err != nil {
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

	_, err = reader.ReadFrame()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("WP1 sidecar accepts only the bootstrap handshake")
	}
	return fmt.Errorf("read post-handshake frame: %w", err)
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
