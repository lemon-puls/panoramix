package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type capturedRequest struct {
	auth  string
	body  chatBody
	query string
}

func streamingServer(t *testing.T, cap *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cap != nil {
			cap.auth = r.Header.Get("Authorization")
			cap.query = r.URL.RawQuery
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &cap.body)
			if strings.Contains(string(raw), "sk-secret") || strings.Contains(r.URL.String(), "sk-secret") {
				t.Errorf("API key leaked into URL/body")
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []string{"SELECT ", "* ", "FROM users;", ""} {
			if chunk == "" {
				fmt.Fprint(w, "data: [DONE]\n\n")
				continue
			}
			payload, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{"delta": map[string]string{"content": chunk}}},
			})
			fmt.Fprintf(w, "data: %s\n\n", payload)
		}
	}))
}

func TestGenerateStreaming(t *testing.T) {
	var captured capturedRequest
	srv := streamingServer(t, &captured)
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m", APIKey: "sk-secret", Temperature: 0.1})
	var deltas []string
	text, err := c.Generate(context.Background(), ChatRequest{SystemPrompt: "sys", UserPrompt: "user"}, func(s string) {
		deltas = append(deltas, s)
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if text != "SELECT * FROM users;" {
		t.Fatalf("text = %q", text)
	}
	if len(deltas) != 3 {
		t.Fatalf("deltas = %v", deltas)
	}
	if captured.auth != "Bearer sk-secret" {
		t.Fatalf("auth header = %q", captured.auth)
	}
	if captured.body.Model != "m" || !captured.body.Stream || len(captured.body.Messages) != 2 {
		t.Fatalf("bad request body: %+v", captured.body)
	}
	if captured.body.Messages[0].Content != "sys" {
		t.Fatalf("system prompt mismatch: %+v", captured.body.Messages[0])
	}
}

func TestGenerateNonStreamJSONFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"SELECT 1;"}}]}`)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m"})
	text, err := c.Generate(context.Background(), ChatRequest{SystemPrompt: "s", UserPrompt: "u"}, nil)
	if err != nil || text != "SELECT 1;" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestGenerateRefusesRedirects(t *testing.T) {
	var hits int32
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"leaked"}}]}`)
	}))
	defer public.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, public.URL, http.StatusTemporaryRedirect)
	}))
	defer local.Close()

	c := NewClient(ClientConfig{BaseURL: local.URL + "/v1", Model: "m"})
	_, err := c.Generate(context.Background(), ChatRequest{SystemPrompt: "s", UserPrompt: "u"}, nil)
	if err == nil {
		t.Fatal("redirect must not be followed")
	}
	if AsProviderError(err).Code != CodeRedirect {
		t.Fatalf("expected redirect error, got %v", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatal("payload must not reach the redirect target")
	}
}

func TestGenerateRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		big := strings.Repeat("a", maxResponseBytes+10)
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"%s"}}]}`, big)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m"})
	_, err := c.Generate(context.Background(), ChatRequest{SystemPrompt: "s", UserPrompt: "u"}, nil)
	if err == nil || AsProviderError(err).Code != CodeResponseLarge {
		t.Fatalf("expected response_too_large, got %v", err)
	}
}

func TestGenerateStatusMapping(t *testing.T) {
	cases := []struct {
		status int
		code   ErrorCode
		body   string
	}{
		{401, CodeAuth, `{"error":{"message":"Incorrect API key provided: sk-secret-abcd"}}`},
		{403, CodeForbidden, `{"error":{"message":"forbidden"}}`},
		{404, CodeNotFound, `{"error":{"message":"model not found"}}`},
		{429, CodeRateLimit, `{"error":{"message":"slow down"}}`},
		{500, CodeServer, `{"error":{"message":"boom"}}`},
		{418, CodeProvider, `{"error":{"message":"teapot"}}`},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m"})
			_, err := c.Generate(context.Background(), ChatRequest{SystemPrompt: "s", UserPrompt: "u"}, nil)
			pe := AsProviderError(err)
			if pe.Code != tc.code {
				t.Fatalf("code = %s, want %s (err=%v)", pe.Code, tc.code, err)
			}
			if strings.Contains(pe.Message, "sk-secret-abcd") {
				t.Fatalf("provider message leaked key: %s", pe.Message)
			}
		})
	}
}

func TestGenerateInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{not json`)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m"})
	_, err := c.Generate(context.Background(), ChatRequest{SystemPrompt: "s", UserPrompt: "u"}, nil)
	if AsProviderError(err).Code != CodeInvalidResp {
		t.Fatalf("expected invalid_response, got %v", err)
	}
}

func TestGenerateRejectsUnexpectedContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>oops</html>")
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m"})
	_, err := c.Generate(context.Background(), ChatRequest{SystemPrompt: "s", UserPrompt: "u"}, nil)
	if AsProviderError(err).Code != CodeInvalidResp {
		t.Fatalf("expected invalid_response, got %v", err)
	}
}

func TestGenerateTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"late"}}]}`)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m", Timeout: 50 * time.Millisecond})
	_, err := c.Generate(context.Background(), ChatRequest{SystemPrompt: "s", UserPrompt: "u"}, nil)
	if AsProviderError(err).Code != CodeTimeout {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestGenerateCanceled(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m", Timeout: 5 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	_, err := c.Generate(ctx, ChatRequest{SystemPrompt: "s", UserPrompt: "u"}, nil)
	if AsProviderError(err).Code != CodeCanceled {
		t.Fatalf("expected canceled, got %v", err)
	}
}

func TestGenerateRejectsUserInfoEndpoint(t *testing.T) {
	c := NewClient(ClientConfig{BaseURL: "http://user:pass@127.0.0.1:1/v1", Model: "m"})
	_, err := c.Generate(context.Background(), ChatRequest{SystemPrompt: "s", UserPrompt: "u"}, nil)
	if AsProviderError(err).Code != CodeConfig {
		t.Fatalf("expected config error, got %v", err)
	}
}

func TestTestConnectionModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"m"},{"id":"other"}]}`)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m"})
	if err := c.TestConnection(context.Background()); err != nil {
		t.Fatalf("test connection: %v", err)
	}
}

func TestTestConnectionModelNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"other"}]}`)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m"})
	err := c.TestConnection(context.Background())
	if AsProviderError(err).Code != CodeModelNotFound {
		t.Fatalf("expected model_not_found, got %v", err)
	}
}

func TestTestConnectionFallsBackToCompletion(t *testing.T) {
	var chatCalled int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		atomic.AddInt32(&chatCalled, 1)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"OK"}}]}`)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL + "/v1", Model: "m"})
	if err := c.TestConnection(context.Background()); err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if atomic.LoadInt32(&chatCalled) != 1 {
		t.Fatal("expected a minimal completion fallback")
	}
}
