package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

// callFabrix posts a prompt to the Fabrix LLM API and returns the response text.
func callFabrix(apiURL, apiKey, channelID, text string) (string, error) {
	form := url.Values{}
	form.Set("text", text)
	form.Set("channel_id", channelID)

	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "Fabrix API 호출 실패")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "failed to read response body")
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Fabrix API 오류 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// Try JSON — check common field names
	var jsonResp map[string]interface{}
	if err := json.Unmarshal(body, &jsonResp); err == nil {
		for _, field := range []string{"response", "text", "content", "message", "answer", "result"} {
			if val, ok := jsonResp[field]; ok {
				if s, ok := val.(string); ok && s != "" {
					return s, nil
				}
			}
		}
	}

	// Fall back to raw body
	result := strings.TrimSpace(string(body))
	if result == "" {
		return "", errors.New("Fabrix API가 빈 응답을 반환했습니다")
	}
	return result, nil
}
