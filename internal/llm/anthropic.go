package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	anthropicAPIVersion     = "2023-06-01"
	anthropicJSONInstruction = "Respond with a single valid JSON value only. No prose, no markdown fences, no commentary."
)

type Anthropic struct {
	HTTP    *http.Client
	BaseURL string
	APIKey  string
	Model   string
}

func NewAnthropic(model string) (*Anthropic, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	return &Anthropic{
		HTTP:    &http.Client{Timeout: 120 * time.Second},
		BaseURL: anthropicDefaultBaseURL,
		APIKey:  key,
		Model:   model,
	}, nil
}

func (a *Anthropic) Name() string { return "anthropic" }

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature,omitempty"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicResponse struct {
	Type    string                  `json:"type"`
	Content []anthropicContentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicErrorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (a *Anthropic) Complete(ctx context.Context, req Request) (*Response, error) {
	model := req.Model
	if model == "" {
		model = a.Model
	}
	if model == "" {
		return nil, fmt.Errorf("anthropic: model not set")
	}
	if req.MaxTokens <= 0 {
		return nil, fmt.Errorf("anthropic: MaxTokens must be > 0")
	}

	body := anthropicRequest{
		Model:       model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	var systemParts []string
	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			systemParts = append(systemParts, m.Content)
		case RoleUser, RoleAssistant:
			body.Messages = append(body.Messages, anthropicMessage{Role: m.Role, Content: m.Content})
		default:
			return nil, fmt.Errorf("anthropic: unknown role %q", m.Role)
		}
	}
	body.System = strings.Join(systemParts, "\n\n")
	if req.JSONMode {
		if body.System == "" {
			body.System = anthropicJSONInstruction
		} else {
			body.System += "\n\n" + anthropicJSONInstruction
		}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	httpResp, err := a.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: http: %w", err)
	}
	defer httpResp.Body.Close()
	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode != http.StatusOK {
		var ae anthropicErrorResponse
		if err := json.Unmarshal(respBody, &ae); err == nil && ae.Error.Message != "" {
			return nil, fmt.Errorf("anthropic %d %s: %s", httpResp.StatusCode, ae.Error.Type, ae.Error.Message)
		}
		return nil, fmt.Errorf("anthropic %d: %s", httpResp.StatusCode, string(respBody))
	}

	var resp anthropicResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("anthropic: parse response: %w", err)
	}
	var content strings.Builder
	for _, c := range resp.Content {
		if c.Type == "text" {
			content.WriteString(c.Text)
		}
	}
	return &Response{
		Content:      content.String(),
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
	}, nil
}
