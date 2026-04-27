package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newAnthropicTest(t *testing.T, handler http.Handler) *Anthropic {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Anthropic{
		HTTP:    srv.Client(),
		BaseURL: srv.URL,
		APIKey:  "sk-test-key",
		Model:   "claude-sonnet-4-7",
	}
}

// captureRequest records the incoming JSON body so a test can assert on it.
type captured struct {
	headers http.Header
	path    string
	body    map[string]any
}

func captureHandler(t *testing.T, status int, respBody string) (*captured, http.Handler) {
	t.Helper()
	c := &captured{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.headers = r.Header.Clone()
		c.path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &c.body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	})
	return c, h
}

const anthropicSuccess = `{
  "type": "message",
  "content": [{"type": "text", "text": "hello world"}],
  "usage": {"input_tokens": 12, "output_tokens": 7}
}`

func TestAnthropicHeadersAndPath(t *testing.T) {
	cap, h := captureHandler(t, 200, anthropicSuccess)
	a := newAnthropicTest(t, h)
	_, err := a.Complete(context.Background(), Request{
		MaxTokens: 256,
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/v1/messages" {
		t.Errorf("path = %q", cap.path)
	}
	if cap.headers.Get("x-api-key") != "sk-test-key" {
		t.Errorf("x-api-key = %q", cap.headers.Get("x-api-key"))
	}
	if cap.headers.Get("anthropic-version") != anthropicAPIVersion {
		t.Errorf("anthropic-version = %q", cap.headers.Get("anthropic-version"))
	}
	if !strings.HasPrefix(cap.headers.Get("Content-Type"), "application/json") {
		t.Errorf("content-type = %q", cap.headers.Get("Content-Type"))
	}
}

func TestAnthropicBodyShape(t *testing.T) {
	cap, h := captureHandler(t, 200, anthropicSuccess)
	a := newAnthropicTest(t, h)
	_, err := a.Complete(context.Background(), Request{
		Model:       "claude-haiku-4-5",
		MaxTokens:   500,
		Temperature: 0.7,
		Messages: []Message{
			{Role: RoleSystem, Content: "you are concise"},
			{Role: RoleUser, Content: "hi"},
			{Role: RoleAssistant, Content: "hello"},
			{Role: RoleUser, Content: "again"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cap.body["model"]; got != "claude-haiku-4-5" {
		t.Errorf("model = %v", got)
	}
	if got := cap.body["max_tokens"]; got != float64(500) {
		t.Errorf("max_tokens = %v", got)
	}
	if got := cap.body["temperature"]; got != 0.7 {
		t.Errorf("temperature = %v", got)
	}
	if got := cap.body["system"]; got != "you are concise" {
		t.Errorf("system = %q", got)
	}
	msgs, ok := cap.body["messages"].([]any)
	if !ok || len(msgs) != 3 {
		t.Fatalf("messages = %#v", cap.body["messages"])
	}
	first := msgs[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "hi" {
		t.Errorf("first message = %+v", first)
	}
}

func TestAnthropicMultipleSystemMessagesJoined(t *testing.T) {
	cap, h := captureHandler(t, 200, anthropicSuccess)
	a := newAnthropicTest(t, h)
	_, err := a.Complete(context.Background(), Request{
		MaxTokens: 100,
		Messages: []Message{
			{Role: RoleSystem, Content: "rule one"},
			{Role: RoleSystem, Content: "rule two"},
			{Role: RoleUser, Content: "go"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cap.body["system"]; got != "rule one\n\nrule two" {
		t.Errorf("system join = %q", got)
	}
}

func TestAnthropicJSONModeAddsInstruction(t *testing.T) {
	cap, h := captureHandler(t, 200, anthropicSuccess)
	a := newAnthropicTest(t, h)
	_, err := a.Complete(context.Background(), Request{
		MaxTokens: 100,
		JSONMode:  true,
		Messages: []Message{
			{Role: RoleSystem, Content: "be helpful"},
			{Role: RoleUser, Content: "give me a structured answer"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sys, _ := cap.body["system"].(string)
	if !strings.HasPrefix(sys, "be helpful") {
		t.Errorf("system lost original: %q", sys)
	}
	if !strings.Contains(sys, "valid JSON") {
		t.Errorf("system missing JSON instruction: %q", sys)
	}
}

func TestAnthropicJSONModeWithoutSystem(t *testing.T) {
	cap, h := captureHandler(t, 200, anthropicSuccess)
	a := newAnthropicTest(t, h)
	_, err := a.Complete(context.Background(), Request{
		MaxTokens: 100,
		JSONMode:  true,
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sys, _ := cap.body["system"].(string)
	if !strings.Contains(sys, "valid JSON") {
		t.Errorf("system missing JSON instruction: %q", sys)
	}
}

func TestAnthropicResponseParsing(t *testing.T) {
	_, h := captureHandler(t, 200, anthropicSuccess)
	a := newAnthropicTest(t, h)
	resp, err := a.Complete(context.Background(), Request{
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello world" {
		t.Errorf("content = %q", resp.Content)
	}
	if resp.InputTokens != 12 || resp.OutputTokens != 7 {
		t.Errorf("usage = in=%d out=%d", resp.InputTokens, resp.OutputTokens)
	}
}

func TestAnthropicMultipleContentBlocksConcatenated(t *testing.T) {
	const body = `{
      "type": "message",
      "content": [
        {"type": "text", "text": "first"},
        {"type": "text", "text": " second"}
      ],
      "usage": {"input_tokens": 1, "output_tokens": 2}
    }`
	_, h := captureHandler(t, 200, body)
	a := newAnthropicTest(t, h)
	resp, err := a.Complete(context.Background(), Request{
		MaxTokens: 10,
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "first second" {
		t.Errorf("content = %q", resp.Content)
	}
}

func TestAnthropicNonTextBlocksIgnored(t *testing.T) {
	const body = `{
      "type": "message",
      "content": [
        {"type": "text", "text": "hi"},
        {"type": "tool_use", "text": "should be ignored"}
      ],
      "usage": {"input_tokens": 1, "output_tokens": 1}
    }`
	_, h := captureHandler(t, 200, body)
	a := newAnthropicTest(t, h)
	resp, err := a.Complete(context.Background(), Request{
		MaxTokens: 10,
		Messages:  []Message{{Role: RoleUser, Content: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hi" {
		t.Errorf("content = %q (non-text block leaked)", resp.Content)
	}
}

func TestAnthropicErrorResponse(t *testing.T) {
	const body = `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens too high"}}`
	_, h := captureHandler(t, 400, body)
	a := newAnthropicTest(t, h)
	_, err := a.Complete(context.Background(), Request{
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid_request_error") {
		t.Errorf("error type missing: %v", err)
	}
	if !strings.Contains(err.Error(), "max_tokens too high") {
		t.Errorf("error message missing: %v", err)
	}
}

func TestAnthropicValidationModelRequired(t *testing.T) {
	a := &Anthropic{HTTP: http.DefaultClient, BaseURL: "http://invalid", APIKey: "x"}
	_, err := a.Complete(context.Background(), Request{
		MaxTokens: 10,
		Messages:  []Message{{Role: RoleUser, Content: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "model not set") {
		t.Errorf("expected model-not-set error, got %v", err)
	}
}

func TestAnthropicValidationMaxTokensRequired(t *testing.T) {
	a := &Anthropic{HTTP: http.DefaultClient, BaseURL: "http://invalid", APIKey: "x", Model: "m"}
	_, err := a.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "MaxTokens") {
		t.Errorf("expected MaxTokens error, got %v", err)
	}
}

func TestAnthropicUnknownRoleRejected(t *testing.T) {
	a := newAnthropicTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	_, err := a.Complete(context.Background(), Request{
		MaxTokens: 10,
		Messages:  []Message{{Role: "weirdo", Content: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown role") {
		t.Errorf("expected unknown-role error, got %v", err)
	}
}

func TestAnthropicName(t *testing.T) {
	a := &Anthropic{}
	if a.Name() != "anthropic" {
		t.Errorf("name = %q", a.Name())
	}
}

func TestNewAnthropicMissingKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := NewAnthropic("claude-sonnet-4-7")
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("expected env-var error, got %v", err)
	}
}

func TestNewAnthropicReadsEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	a, err := NewAnthropic("claude-sonnet-4-7")
	if err != nil {
		t.Fatal(err)
	}
	if a.APIKey != "from-env" {
		t.Errorf("APIKey = %q", a.APIKey)
	}
	if a.Model != "claude-sonnet-4-7" {
		t.Errorf("Model = %q", a.Model)
	}
}
