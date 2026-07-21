package skills

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestRestrictedSkillInstallClientRejectsNonPublicDestinations(t *testing.T) {
	client := newSkillInstallHTTPClient()
	for _, rawURL := range []string{
		"https://127.0.0.1/archive.zip",
		"https://[::1]/archive.zip",
		"https://169.254.169.254/latest/meta-data/",
		"https://10.0.0.1/archive.zip",
	} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Do(req); err == nil || !strings.Contains(err.Error(), "non-public") {
			t.Fatalf("client.Do(%q) error = %v, want non-public destination rejection", rawURL, err)
		}
	}
}

func TestNewRestrictedRemoteHTTPClientPreservesDestinationRestrictions(t *testing.T) {
	client := NewRestrictedRemoteHTTPClient()
	for _, rawURL := range []string{"https://127.0.0.1/archive.zip", "https://10.0.0.1/archive.zip", "https://169.254.169.254/archive.zip", "https://[::1]/archive.zip", "https://[fe80::1]/archive.zip", "https://[fd00::1]/archive.zip"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Do(req); err == nil || !strings.Contains(err.Error(), "non-public") {
			t.Fatalf("client.Do(%q) error = %v, want non-public rejection", rawURL, err)
		}
	}
	redirect, _ := url.Parse("http://example.com/archive.zip")
	if err := client.CheckRedirect(&http.Request{URL: redirect}, nil); err == nil {
		t.Fatal("exported client accepted non-HTTPS redirect")
	}
	redirect, _ = url.Parse("https://example.com/archive.zip")
	if err := client.CheckRedirect(&http.Request{URL: redirect}, make([]*http.Request, 10)); err == nil {
		t.Fatal("exported client accepted excessive redirects")
	}
}

func TestXiapingPublicCatalogAllowsOnlyItsManagedNetworkRoute(t *testing.T) {
	managed := netip.MustParseAddr("198.18.0.132")
	if err := validateXiapingCatalogAddr("xiaping.coze.com", managed); err != nil {
		t.Fatalf("Xiaping managed route rejected: %v", err)
	}
	for _, host := range []string{"example.com", "127.0.0.1"} {
		if err := validateXiapingCatalogAddr(host, managed); err == nil || !strings.Contains(err.Error(), "non-public") {
			t.Fatalf("host %q error = %v, want non-public rejection", host, err)
		}
	}
}

func TestSkillInstallRedirectPolicyRevalidatesEveryHop(t *testing.T) {
	for _, rawURL := range []string{
		"http://example.com/archive.zip",
		"file:///tmp/archive.zip",
	} {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		if err := skillInstallRedirectPolicy(&http.Request{URL: parsed}, nil); err == nil {
			t.Fatalf("redirect to %q should be rejected", rawURL)
		}
	}
	parsed, _ := url.Parse("https://example.com/archive.zip")
	via := make([]*http.Request, 10)
	if err := skillInstallRedirectPolicy(&http.Request{URL: parsed}, via); err == nil {
		t.Fatal("redirect chain longer than the client limit should be rejected")
	}
}
