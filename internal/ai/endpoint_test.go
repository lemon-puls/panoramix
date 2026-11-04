package ai

import (
	"strings"
	"testing"
)

func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		scope   Scope
		wantErr bool
	}{
		{"loopback", "http://127.0.0.1:11434/v1", "http://127.0.0.1:11434/v1", ScopeLocal, false},
		{"trailing slash and case", "HTTP://LOCALHOST:11434/v1/", "http://localhost:11434/v1", ScopeLocal, false},
		{"lan rfc1918", "http://10.1.2.3:1234/v1", "http://10.1.2.3:1234/v1", ScopeLAN, false},
		{"cloud default port", "https://api.openai.com:443/v1", "https://api.openai.com/v1", ScopeCloud, false},
		{"cloud", "https://api.example.com/v1", "https://api.example.com/v1", ScopeCloud, false},
		{"ipv6 loopback", "http://[::1]:11434/v1", "http://[::1]:11434/v1", ScopeLocal, false},
		{"userinfo rejected", "http://user:pass@127.0.0.1/v1", "", ScopeLocal, true},
		{"scheme rejected", "ftp://127.0.0.1/v1", "", ScopeLocal, true},
		{"empty", "   ", "", ScopeLocal, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := NormalizeEndpoint(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", ep)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ep.Normalized != tc.want {
				t.Fatalf("normalized = %q, want %q", ep.Normalized, tc.want)
			}
			if ep.Scope != tc.scope {
				t.Fatalf("scope = %q, want %q", ep.Scope, tc.scope)
			}
			if len(ep.Hash) != 16 {
				t.Fatalf("hash length = %d", len(ep.Hash))
			}
		})
	}
}

func TestEndpointHashStability(t *testing.T) {
	a, _ := NormalizeEndpoint("http://127.0.0.1:11434/v1")
	b, _ := NormalizeEndpoint("http://127.0.0.1:11434/v1/?token=ignored#frag")
	if a.Hash != b.Hash {
		t.Fatalf("query/fragment must not affect endpoint hash: %s != %s", a.Hash, b.Hash)
	}
	c, _ := NormalizeEndpoint("http://127.0.0.1:11434/v2")
	if a.Hash == c.Hash {
		t.Fatal("different paths must produce different endpoint hashes")
	}
	d, _ := NormalizeEndpoint("http://127.0.0.1:11435/v1")
	if a.Hash == d.Hash {
		t.Fatal("different ports must produce different endpoint hashes")
	}
}

func TestKeyRefBoundToEndpoint(t *testing.T) {
	a, _ := NormalizeEndpoint("http://127.0.0.1:11434/v1")
	b, _ := NormalizeEndpoint("http://127.0.0.1:1234/v1")
	ka := KeyRef(ProviderOllama, a.Hash)
	kb := KeyRef(ProviderOllama, b.Hash)
	if ka == kb {
		t.Fatal("key refs for different endpoints must differ")
	}
	if !strings.Contains(ka, a.Hash) || !strings.Contains(ka, string(ProviderOllama)) {
		t.Fatalf("key ref should bind provider + endpoint hash: %s", ka)
	}
}

func TestChatCompletionsURL(t *testing.T) {
	u, err := ChatCompletionsURL("http://127.0.0.1:11434/v1")
	if err != nil || u != "http://127.0.0.1:11434/v1/chat/completions" {
		t.Fatalf("got %q err=%v", u, err)
	}
	u, err = ChatCompletionsURL("https://example.com/v1/chat/completions")
	if err != nil || u != "https://example.com/v1/chat/completions" {
		t.Fatalf("complete endpoint should be preserved, got %q err=%v", u, err)
	}
}

func TestNormalizeConfig(t *testing.T) {
	cfg, ep, warnings, err := NormalizeConfig(Config{Enabled: true, BaseURL: "http://127.0.0.1:11434/v1/", Model: "qwen"})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if cfg.Provider != ProviderOllama || cfg.TimeoutSec != 120 || cfg.Temperature != 0.1 || cfg.Concurrency != 1 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if ep.Scope != ScopeLocal {
		t.Fatalf("scope = %s", ep.Scope)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}

	if _, _, _, err := NormalizeConfig(Config{Provider: "bogus", BaseURL: "http://x/v1"}); err == nil {
		t.Fatal("unknown provider should be rejected")
	}
	if _, _, _, err := NormalizeConfig(Config{BaseURL: "http://x/v1", TimeoutSec: 5}); err == nil {
		t.Fatal("timeout out of range should be rejected")
	}
	if _, _, _, err := NormalizeConfig(Config{BaseURL: "http://x/v1", Concurrency: 3}); err == nil {
		t.Fatal("concurrency out of range should be rejected")
	}
	if _, _, _, err := NormalizeConfig(Config{BaseURL: "http://x/v1", Temperature: 3}); err == nil {
		t.Fatal("temperature out of range should be rejected")
	}
}
