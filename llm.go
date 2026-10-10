package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func LLMRefine(code, compileErr string) (string, error) {
	base := os.Getenv("LLM_BASE_URL")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	base = strings.TrimRight(base, "/")

	key := os.Getenv("LLM_API_KEY")
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	if key == "" {
		return "", fmt.Errorf("set LLM_API_KEY or OPENAI_API_KEY")
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}

	reqBody := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: SystemPrompt},
			{Role: "user", Content: UserPrompt(code, compileErr)},
		},
		Temperature: 0.1,
		Stream:      false,
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := base + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	if referer := os.Getenv("LLM_REFERER"); referer != "" {
		req.Header.Set("HTTP-Referer", referer)
	}
	if title := os.Getenv("LLM_TITLE"); title != "" {
		req.Header.Set("X-Title", title)
	}

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode: %v: %s", err, string(body))
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("api: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}

	return StripFences(parsed.Choices[0].Message.Content), nil
}

func StripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	if strings.HasPrefix(lines[0], "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
