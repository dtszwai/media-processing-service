package webhook

import "testing"

func TestNormalizeURL_RequiresHTTPSHost(t *testing.T) {
	cases := []string{
		"http://example.com/hook",
		"ftp://example.com/hook",
		"https:///hook",
		"https:example.com/hook",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			if got, err := NormalizeURL(raw); err == nil {
				t.Fatalf("NormalizeURL(%q) = %q, nil error", raw, got)
			}
		})
	}
}

func TestNormalizeURL_TrimsValidHTTPSURL(t *testing.T) {
	got, err := NormalizeURL("  https://example.com/hook?tenant=t  ")
	if err != nil {
		t.Fatalf("NormalizeURL: %v", err)
	}
	if got != "https://example.com/hook?tenant=t" {
		t.Fatalf("NormalizeURL = %q", got)
	}
}
