package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newOpenAITest(t *testing.T, handler http.Handler) *OpenAI {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &OpenAI{
		HTTP:    srv.Client(),
		BaseURL: srv.URL,
		APIKey:  "sk-openai-test",
		Model:   "gpt-4o-mini",
	}
}

const openaiSuccess = `{
  "id": "chatcmpl-1",
  "choices": [{"index": 0, "message": {"role": "assistant", "content": "the answer is 42"}}],
  "usage": {"prompt_tokens": 9, "completion_tokens": 4, "total_tokens": 13}
}`

func TestOpenAIHeadersAndPath(t *testing.T) {
	cap, h := captureHandler(t, 200, openaiSuccess)
	o := newOpenAITest(t, h)
	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/v1/chat/completions" {
		t.Errorf("path = %q", cap.path)
	}
	if cap.headers.Get("Authorization") != "Bearer sk-openai-test" {
		t.Errorf("authorization = %q", cap.headers.Get("Authorization"))
	}
	if !strings.HasPrefix(cap.headers.Get("Content-Type"), "application/json") {
		t.Errorf("content-type = %q", cap.headers.Get("Content-Type"))
	}
}

func TestOpenAIBodyShape(t *testing.T) {
	cap, h := captureHandler(t, 200, openaiSuccess)
	o := newOpenAITest(t, h)
	_, err := o.Complete(context.Background(), Request{
		Model:       "gpt-5",
		MaxTokens:   1024,
		Temperature: 0.5,
		Messages: []Message{
			{Role: RoleSystem, Content: "be terse"},
			{Role: RoleUser, Content: "hi"},
			{Role: RoleAssistant, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cap.body["model"]; got != "gpt-5" {
		t.Errorf("model = %v", got)
	}
	if got := cap.body["max_completion_tokens"]; got != float64(1024) {
		t.Errorf("max_completion_tokens = %v", got)
	}
	if got := cap.body["temperature"]; got != 0.5 {
		t.Errorf("temperature = %v", got)
	}
	msgs, ok := cap.body["messages"].([]any)
	if !ok || len(msgs) != 3 {
		t.Fatalf("messages = %#v", cap.body["messages"])
	}
	sys := msgs[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != "be terse" {
		t.Errorf("system message = %+v", sys)
	}
}

func TestOpenAIDefaultModelUsed(t *testing.T) {
	cap, h := captureHandler(t, 200, openaiSuccess)
	o := newOpenAITest(t, h)
	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 50,
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cap.body["model"]; got != "gpt-4o-mini" {
		t.Errorf("default model not used: %v", got)
	}
}

func TestOpenAIJSONModeSetsResponseFormat(t *testing.T) {
	cap, h := captureHandler(t, 200, openaiSuccess)
	o := newOpenAITest(t, h)
	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 50,
		JSONMode:  true,
		Messages:  []Message{{Role: RoleUser, Content: "structured"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rf, ok := cap.body["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format missing or wrong type: %#v", cap.body["response_format"])
	}
	if rf["type"] != "json_object" {
		t.Errorf("response_format.type = %v", rf["type"])
	}
}

func TestOpenAIJSONModeOmittedWhenFalse(t *testing.T) {
	cap, h := captureHandler(t, 200, openaiSuccess)
	o := newOpenAITest(t, h)
	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 50,
		Messages:  []Message{{Role: RoleUser, Content: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := cap.body["response_format"]; present {
		t.Errorf("response_format should be omitted when JSONMode=false")
	}
}

func TestOpenAITemperatureOmittedWhenZero(t *testing.T) {
	cap, h := captureHandler(t, 200, openaiSuccess)
	o := newOpenAITest(t, h)
	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 50,
		Messages:  []Message{{Role: RoleUser, Content: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := cap.body["temperature"]; present {
		t.Errorf("temperature should be omitted when zero (let server default apply)")
	}
}

func TestOpenAIResponseParsing(t *testing.T) {
	_, h := captureHandler(t, 200, openaiSuccess)
	o := newOpenAITest(t, h)
	resp, err := o.Complete(context.Background(), Request{
		MaxTokens: 50,
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "the answer is 42" {
		t.Errorf("content = %q", resp.Content)
	}
	if resp.InputTokens != 9 || resp.OutputTokens != 4 {
		t.Errorf("usage = in=%d out=%d", resp.InputTokens, resp.OutputTokens)
	}
}

func TestOpenAIErrorResponse(t *testing.T) {
	const body = `{"error":{"message":"invalid api key","type":"invalid_request_error","code":"invalid_api_key"}}`
	_, h := captureHandler(t, 401, body)
	o := newOpenAITest(t, h)
	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 10,
		Messages:  []Message{{Role: RoleUser, Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid_request_error") {
		t.Errorf("error type missing: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error message missing: %v", err)
	}
}

func TestOpenAIEmptyChoicesError(t *testing.T) {
	const body = `{"choices": [], "usage": {"prompt_tokens": 0, "completion_tokens": 0}}`
	_, h := captureHandler(t, 200, body)
	o := newOpenAITest(t, h)
	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 10,
		Messages:  []Message{{Role: RoleUser, Content: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Errorf("expected no-choices error, got %v", err)
	}
}

func TestOpenAIUnknownRoleRejected(t *testing.T) {
	o := newOpenAITest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 10,
		Messages:  []Message{{Role: "weirdo", Content: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown role") {
		t.Errorf("expected unknown-role error, got %v", err)
	}
}

func TestOpenAIName(t *testing.T) {
	o := &OpenAI{}
	if o.Name() != "openai" {
		t.Errorf("name = %q", o.Name())
	}
}

func TestNewOpenAIMissingKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := NewOpenAI("gpt-4o-mini")
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("expected env-var error, got %v", err)
	}
}

func TestNewOpenAIReadsEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "from-env")
	o, err := NewOpenAI("gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}
	if o.APIKey != "from-env" {
		t.Errorf("APIKey = %q", o.APIKey)
	}
}

// pin httptest import in case other tests are skipped
var _ = httptest.NewServer
