package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupportsVision(t *testing.T) {
	cases := []struct {
		desc     string
		provider string
		model    string
		want     bool
	}{
		{"openai gpt-4o", "openai", "gpt-4o", true},
		{"openai gpt-4o dated suffix", "openai", "gpt-4o-2024-11-20", true},
		{"openai gpt-4o-mini", "openai", "gpt-4o-mini", true},
		{"openai gpt-4-turbo", "openai", "gpt-4-turbo", true},
		{"openai gpt-5", "openai", "gpt-5", true},
		{"openai gpt-3.5", "openai", "gpt-3.5-turbo", false},
		{"anthropic claude-opus-4-7", "anthropic", "claude-opus-4-7", false},
		{"unknown provider", "perplexity", "anything", false},
		{"empty model", "openai", "", false},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			if got := SupportsVision(c.provider, c.model); got != c.want {
				t.Errorf("SupportsVision(%q, %q) = %v, want %v", c.provider, c.model, got, c.want)
			}
		})
	}
}

// Vision request smoke test: the OpenAI provider should send the user
// message as a content-parts array containing one text part and one
// image_url part when Request.Images is non-empty. The image_url URL
// should be a data: URL when Path is set (so the request is
// self-contained even if the original signed URL has expired).
func TestOpenAIVisionRequestShape(t *testing.T) {
	tmp := t.TempDir()
	imgPath := filepath.Join(tmp, "shot.png")
	imgBytes := []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02} // arbitrary bytes; not a real PNG, but read+encoded faithfully
	if err := os.WriteFile(imgPath, imgBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	cap, h := captureHandler(t, 200, openaiSuccess)
	o := newOpenAITest(t, h)
	o.Model = "gpt-4o"

	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 1024,
		Messages: []Message{
			{Role: RoleUser, Content: "what's in this image?"},
		},
		Images: []ImageInput{{Path: imgPath}},
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs, ok := cap.body["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages = %#v", cap.body["messages"])
	}
	user := msgs[0].(map[string]any)
	if user["role"] != "user" {
		t.Fatalf("role = %v", user["role"])
	}
	parts, ok := user["content"].([]any)
	if !ok {
		t.Fatalf("expected content to be array (vision request), got %#v", user["content"])
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 content parts (text + image), got %d", len(parts))
	}

	textPart := parts[0].(map[string]any)
	if textPart["type"] != "text" || textPart["text"] != "what's in this image?" {
		t.Errorf("text part: %+v", textPart)
	}

	imgPart := parts[1].(map[string]any)
	if imgPart["type"] != "image_url" {
		t.Errorf("image part type: %v", imgPart["type"])
	}
	imgRef, ok := imgPart["image_url"].(map[string]any)
	if !ok {
		t.Fatalf("image_url shape: %#v", imgPart["image_url"])
	}
	url, _ := imgRef["url"].(string)
	wantPrefix := "data:image/png;base64,"
	if !strings.HasPrefix(url, wantPrefix) {
		t.Fatalf("expected data URL with PNG MIME, got: %q", url)
	}
	encoded := strings.TrimPrefix(url, wantPrefix)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if string(decoded) != string(imgBytes) {
		t.Errorf("decoded bytes don't match original image bytes")
	}
}

// When Path is empty and only URL is set, OpenAI should pass the URL
// straight through (no base64 encoding, no file read). That's the
// fallback path for callers that only have a remote reference.
func TestOpenAIVisionRequestPassesThroughURL(t *testing.T) {
	cap, h := captureHandler(t, 200, openaiSuccess)
	o := newOpenAITest(t, h)
	o.Model = "gpt-4o"

	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 1024,
		Messages:  []Message{{Role: RoleUser, Content: "describe"}},
		Images:    []ImageInput{{URL: "https://example.com/photo.jpg"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs := cap.body["messages"].([]any)
	parts := msgs[0].(map[string]any)["content"].([]any)
	imgRef := parts[1].(map[string]any)["image_url"].(map[string]any)
	if imgRef["url"] != "https://example.com/photo.jpg" {
		t.Errorf("expected URL passthrough, got %v", imgRef["url"])
	}
}

// Plain text (no images) keeps the legacy `content: <string>` shape so
// the body is identical to pre-vision builds for non-vision tasks. This
// catches accidental shape regression that might confuse JSON-mode
// parsers or token counters downstream.
func TestOpenAIPlainTextStillUsesStringContent(t *testing.T) {
	cap, h := captureHandler(t, 200, openaiSuccess)
	o := newOpenAITest(t, h)

	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := cap.body["messages"].([]any)
	user := msgs[0].(map[string]any)
	content := user["content"]
	if _, isStr := content.(string); !isStr {
		t.Errorf("plain text request should keep content as string, got %T: %#v", content, content)
	}
}

// When Images is set but no user-role message exists, the provider
// should fail loudly rather than silently dropping the images.
func TestOpenAIVisionWithoutUserMessageErrors(t *testing.T) {
	o := newOpenAITest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	_, err := o.Complete(context.Background(), Request{
		MaxTokens: 100,
		Messages:  []Message{{Role: RoleSystem, Content: "be terse"}},
		Images:    []ImageInput{{URL: "https://example.com/x.png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "no user-role message") {
		t.Errorf("expected user-message error, got %v", err)
	}
}

// guessImageMIME unit test — small but worth pinning since the dataURL
// prefix correctness depends on it.
func TestGuessImageMIME(t *testing.T) {
	cases := []struct{ path, want string }{
		{"shot.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.JPEG", "image/jpeg"},
		{"img.webp", "image/webp"},
		{"banner.gif", "image/gif"},
		{"unknown.bin", "image/png"}, // safe default for screenshots
		{"noext", "image/png"},
	}
	for _, c := range cases {
		if got := guessImageMIME(c.path); got != c.want {
			t.Errorf("guessImageMIME(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// pin httptest import for symmetry with other test files
var _ = httptest.NewServer

// pin json import for symmetry — vision tests rely on json decoding via
// the captureHandler helper; making the dep explicit so removing
// captureHandler in future doesn't surprise this file.
var _ = json.Unmarshal
