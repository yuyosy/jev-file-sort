package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

type ChoiceQuery struct {
	ID           string
	State        any
	Instructions any
	Criteria     map[string]any
}

var errRequestTooBig = errors.New("TypeSafe request exceeds the token limit")

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
	decoded, err := c.send(ctx, request{State: state, Model: c.Config.Model, Questions: map[string]question{"category": {Type: "choice", Instructions: instructions, Criteria: criteria}}})
	if err != nil {
		return ChoiceResult{}, err
	}
	return decodeChoice(decoded, "category", criteria)
}

func (c Client) ChooseMany(ctx context.Context, queries []ChoiceQuery) (map[string]ChoiceResult, error) {
	if len(queries) == 0 {
		return map[string]ChoiceResult{}, nil
	}
	states := make(map[string]any, len(queries))
	questions := make(map[string]question, len(queries))
	for _, query := range queries {
		if query.ID == "" {
			return nil, fmt.Errorf("choice query ID must not be empty")
		}
		if _, exists := questions[query.ID]; exists {
			return nil, fmt.Errorf("duplicate choice query ID %q", query.ID)
		}
		states[query.ID] = query.State
		questions[query.ID] = question{Type: "choice", Instructions: query.Instructions, Criteria: query.Criteria}
	}
	decoded, err := c.send(ctx, request{State: map[string]any{"entries": states}, Model: c.Config.Model, Questions: questions})
	if errors.Is(err, errRequestTooBig) && len(queries) > 1 {
		half := (len(queries) + 1) / 2
		left, leftErr := c.ChooseMany(ctx, queries[:half])
		if leftErr != nil {
			return nil, leftErr
		}
		right, rightErr := c.ChooseMany(ctx, queries[half:])
		if rightErr != nil {
			return nil, rightErr
		}
		for id, result := range right {
			left[id] = result
		}
		return left, nil
	}
	if err != nil {
		return nil, err
	}
	results := make(map[string]ChoiceResult, len(queries))
	for _, query := range queries {
		result, err := decodeChoice(decoded, query.ID, query.Criteria)
		if err != nil {
			return nil, err
		}
		results[query.ID] = result
	}
	return results, nil
}

func decodeChoice(decoded response, id string, criteria map[string]any) (ChoiceResult, error) {
	answer, exists := decoded.Answers[id]
	if !exists || answer.Type != "choice" {
		return ChoiceResult{}, fmt.Errorf("TypeSafe response omitted choice answer %q", id)
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

func (c Client) send(ctx context.Context, value request) (response, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return response{}, err
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: time.Duration(c.Config.TimeoutSeconds) * time.Second}
	}
	var lastErr error
	for attempt := 0; attempt <= c.Config.MaxRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Config.Endpoint, bytes.NewReader(body))
		if err != nil {
			return response{}, err
		}
		request.Header.Set("Authorization", "Bearer "+c.APIKey)
		request.Header.Set("Content-Type", "application/json")
		result, err := httpClient.Do(request)
		if err != nil {
			lastErr = err
			if attempt < c.Config.MaxRetries {
				if err := wait(ctx, attempt, 0); err != nil {
					return response{}, err
				}
				continue
			}
			break
		}
		data, readErr := io.ReadAll(io.LimitReader(result.Body, 1<<20))
		result.Body.Close()
		if readErr != nil {
			return response{}, readErr
		}
		if result.StatusCode == http.StatusTooManyRequests || result.StatusCode == 529 {
			lastErr = fmt.Errorf("TypeSafe returned %s", result.Status)
			if attempt < c.Config.MaxRetries {
				delay := retryAfter(result.Header.Get("Retry-After"))
				if err := wait(ctx, attempt, delay); err != nil {
					return response{}, err
				}
				continue
			}
		}
		if result.StatusCode < 200 || result.StatusCode >= 300 {
			message := strings.TrimSpace(string(data))
			if result.StatusCode == http.StatusBadRequest && strings.Contains(message, "max_tokens_exceeded") {
				return response{}, fmt.Errorf("%w: %s", errRequestTooBig, message)
			}
			return response{}, fmt.Errorf("TypeSafe returned %s: %s", result.Status, message)
		}
		var decoded response
		if err := json.Unmarshal(data, &decoded); err != nil {
			return response{}, fmt.Errorf("decode TypeSafe response: %w", err)
		}
		return decoded, nil
	}
	return response{}, lastErr
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
