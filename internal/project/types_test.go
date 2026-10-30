package project

import "testing"

func TestLayoutComparisonsDirUsesImmutableStoreRoot(t *testing.T) {
	layout := Layout{StoreRoot: `C:\data\stores\store-a`}
	if got, want := layout.ComparisonsDir(), `C:\data\stores\store-a\comparisons`; got != want {
		t.Fatalf("ComparisonsDir() = %q, want %q", got, want)
	}
}
