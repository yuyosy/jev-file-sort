package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"jev-file-sort/internal/config"
)

type Client struct {
	Config config.Jev
	APIKey string
	HTTP   *http.Client
}

type ChoiceResult struct {
	Choice        string
	Confidence    float64
	Probabilities map[string]float64
	Model         string
}

type request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]question `json:"questions"`
}

type question struct {
	Type         string         `json:"type"`
	Instructions any            `json:"instructions"`
	Criteria     map[string]any `json:"criteria"`
}

type response struct {
	Model   string            `json:"model"`
	Answers map[string]answer `json:"answers"`
}

type answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

func (c Client) Choose(ctx context.Context, state, instructions any, criteria map[string]any) (ChoiceResult, error) {
	body, err := json.Marshal(request{State: state, Model: c.Config.Model, Questions: map[string]question{"category": {Type: "choice", Instructions: instructions, Criteria: criteria}}})
	if err != nil {
		return ChoiceResult{}, err
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: time.Duration(c.Config.TimeoutSeconds) * time.Second}
	}
	var lastErr error
	for attempt := 0; attempt <= c.Config.MaxRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Config.Endpoint, bytes.NewReader(body))
		if err != nil {
			return ChoiceResult{}, err
		}
		request.Header.Set("Authorization", "Bearer "+c.APIKey)
		request.Header.Set("Content-Type", "application/json")
		result, err := httpClient.Do(request)
		if err != nil {
			lastErr = err
			if attempt < c.Config.MaxRetries {
				if err := wait(ctx, attempt, 0); err != nil {
					return ChoiceResult{}, err
				}
				continue
			}
			break
		}
		data, readErr := io.ReadAll(io.LimitReader(result.Body, 1<<20))
		result.Body.Close()
		if readErr != nil {
			return ChoiceResult{}, readErr
		}
		if result.StatusCode == http.StatusTooManyRequests || result.StatusCode == 529 {
			lastErr = fmt.Errorf("TypeSafe returned %s", result.Status)
			if attempt < c.Config.MaxRetries {
				delay := retryAfter(result.Header.Get("Retry-After"))
				if err := wait(ctx, attempt, delay); err != nil {
					return ChoiceResult{}, err
				}
				continue
			}
		}
		if result.StatusCode < 200 || result.StatusCode >= 300 {
			return ChoiceResult{}, fmt.Errorf("TypeSafe returned %s: %s", result.Status, strings.TrimSpace(string(data)))
		}
		var decoded response
		if err := json.Unmarshal(data, &decoded); err != nil {
			return ChoiceResult{}, fmt.Errorf("decode TypeSafe response: %w", err)
		}
		answer, exists := decoded.Answers["category"]
		if !exists || answer.Type != "choice" {
			return ChoiceResult{}, fmt.Errorf("TypeSafe response omitted the choice answer")
		}
		if _, exists := criteria[answer.Choice]; !exists {
			return ChoiceResult{}, fmt.Errorf("TypeSafe returned unknown choice %q", answer.Choice)
		}
		if answer.Confidence < 0 || answer.Confidence > 1 {
			return ChoiceResult{}, fmt.Errorf("TypeSafe returned invalid confidence %v", answer.Confidence)
		}
		total := 0.0
		for option, probability := range answer.Probabilities {
			if _, exists := criteria[option]; !exists || probability < 0 || probability > 1 {
				return ChoiceResult{}, fmt.Errorf("TypeSafe returned invalid probability distribution")
			}
			total += probability
		}
		if len(answer.Probabilities) != len(criteria) || math.Abs(total-1) > 0.01 {
			return ChoiceResult{}, fmt.Errorf("TypeSafe returned incomplete probability distribution")
		}
		return ChoiceResult{Choice: answer.Choice, Confidence: answer.Confidence, Probabilities: answer.Probabilities, Model: decoded.Model}, nil
	}
	return ChoiceResult{}, lastErr
}

func retryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
func wait(ctx context.Context, attempt int, requested time.Duration) error {
	delay := time.Second << min(attempt, 5)
	if requested > delay {
		delay = requested
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
