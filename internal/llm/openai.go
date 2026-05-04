package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const openaiDefaultBaseURL = "https://api.openai.com"

type OpenAI struct {
	HTTP    *http.Client
	BaseURL string
	APIKey  string
	Model   string
}

func NewOpenAI(model string) (*OpenAI, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}
	return &OpenAI{
		HTTP:    &http.Client{Timeout: 120 * time.Second},
		BaseURL: openaiDefaultBaseURL,
		APIKey:  key,
		Model:   model,
	}, nil
}

func (o *OpenAI) Name() string { return "openai" }

type openaiRequest struct {
	Model          string                `json:"model"`
	Messages       []openaiMessage       `json:"messages"`
	MaxTokens      int                   `json:"max_completion_tokens,omitempty"`
	Temperature    *float64              `json:"temperature,omitempty"`
	ResponseFormat *openaiResponseFormat `json:"response_format,omitempty"`
}

// openaiMessage covers both shapes the API accepts: Content is a string
// for plain text messages and a slice of content blocks for vision
// requests. Marshaling picks whichever is non-nil.
type openaiMessage struct {
	Role         string
	Text         string
	ContentParts []openaiContentPart
}

type openaiContentPart struct {
	Type     string             `json:"type"`
	Text     string             `json:"text,omitempty"`
	ImageURL *openaiImageURLRef `json:"image_url,omitempty"`
}

type openaiImageURLRef struct {
	URL string `json:"url"`
}

func (m openaiMessage) MarshalJSON() ([]byte, error) {
	type withText struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type withParts struct {
		Role    string              `json:"role"`
		Content []openaiContentPart `json:"content"`
	}
	if len(m.ContentParts) > 0 {
		return json.Marshal(withParts{Role: m.Role, Content: m.ContentParts})
	}
	return json.Marshal(withText{Role: m.Role, Content: m.Text})
}

type openaiResponseFormat struct {
	Type string `json:"type"`
}

// openaiResponseMessage is the response-side shape: completions always
// return plain string content, regardless of whether the request used
// multipart vision content blocks.
type openaiResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Choices []struct {
		Message openaiResponseMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openaiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (o *OpenAI) Complete(ctx context.Context, req Request) (*Response, error) {
	model := req.Model
	if model == "" {
		model = o.Model
	}
	if model == "" {
		return nil, fmt.Errorf("openai: model not set")
	}

	body := openaiRequest{
		Model:     model,
		MaxTokens: req.MaxTokens,
	}
	if req.Temperature != 0 {
		t := req.Temperature
		body.Temperature = &t
	}
	if req.JSONMode {
		body.ResponseFormat = &openaiResponseFormat{Type: "json_object"}
	}
	// Translate llm.Messages to openaiMessages. Images attach to the LAST
	// user-role message — same convention OpenAI's reference docs use for
	// vision requests.
	lastUserIdx := -1
	for i, m := range req.Messages {
		if m.Role == RoleUser {
			lastUserIdx = i
		}
	}
	if len(req.Images) > 0 && lastUserIdx == -1 {
		return nil, fmt.Errorf("openai: Images set but no user-role message to attach them to")
	}
	for i, m := range req.Messages {
		switch m.Role {
		case RoleSystem, RoleAssistant:
			body.Messages = append(body.Messages, openaiMessage{Role: m.Role, Text: m.Content})
		case RoleUser:
			if i == lastUserIdx && len(req.Images) > 0 {
				parts := []openaiContentPart{{Type: "text", Text: m.Content}}
				for _, img := range req.Images {
					ref, err := openaiImageRef(img)
					if err != nil {
						return nil, fmt.Errorf("openai: image %d: %w", len(parts)-1, err)
					}
					parts = append(parts, openaiContentPart{Type: "image_url", ImageURL: ref})
				}
				body.Messages = append(body.Messages, openaiMessage{Role: m.Role, ContentParts: parts})
			} else {
				body.Messages = append(body.Messages, openaiMessage{Role: m.Role, Text: m.Content})
			}
		default:
			return nil, fmt.Errorf("openai: unknown role %q", m.Role)
		}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)

	httpResp, err := o.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: http: %w", err)
	}
	defer httpResp.Body.Close()
	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode != http.StatusOK {
		herr := &HTTPError{Provider: "openai", StatusCode: httpResp.StatusCode, Body: string(respBody)}
		var oe openaiErrorResponse
		if err := json.Unmarshal(respBody, &oe); err == nil && oe.Error.Message != "" {
			herr.ErrType = oe.Error.Type
			herr.Message = oe.Error.Message
		}
		return nil, herr
	}

	var resp openaiResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("openai: parse response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices in response")
	}
	return &Response{
		Content:      resp.Choices[0].Message.Content,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}, nil
}

// openaiImageRef turns an llm.ImageInput into the OpenAI `image_url`
// payload. Local Path is preferred — the file is read and embedded as a
// data URL so the wire request is self-contained even when the original
// (signed) URL has expired. URL is the fallback for callers that only
// have a remote reference.
func openaiImageRef(img ImageInput) (*openaiImageURLRef, error) {
	if img.Path != "" {
		data, err := os.ReadFile(img.Path)
		if err != nil {
			return nil, fmt.Errorf("read image %s: %w", img.Path, err)
		}
		mime := img.MIME
		if mime == "" {
			mime = guessImageMIME(img.Path)
		}
		dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
		return &openaiImageURLRef{URL: dataURL}, nil
	}
	if img.URL != "" {
		return &openaiImageURLRef{URL: img.URL}, nil
	}
	return nil, fmt.Errorf("image has neither Path nor URL")
}

// guessImageMIME returns the image/* type based on a file extension.
// Defaults to image/png when unrecognized — that's the safe default for
// Firecrawl screenshots, which are PNG.
func guessImageMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/png"
	}
}
