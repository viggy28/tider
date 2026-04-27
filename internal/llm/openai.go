package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponseFormat struct {
	Type string `json:"type"`
}

type openaiResponse struct {
	Choices []struct {
		Message openaiMessage `json:"message"`
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
	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem, RoleUser, RoleAssistant:
			body.Messages = append(body.Messages, openaiMessage{Role: m.Role, Content: m.Content})
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
		var oe openaiErrorResponse
		if err := json.Unmarshal(respBody, &oe); err == nil && oe.Error.Message != "" {
			return nil, fmt.Errorf("openai %d %s: %s", httpResp.StatusCode, oe.Error.Type, oe.Error.Message)
		}
		return nil, fmt.Errorf("openai %d: %s", httpResp.StatusCode, string(respBody))
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
