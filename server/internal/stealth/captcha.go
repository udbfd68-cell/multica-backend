package stealth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CaptchaSolver provides CAPTCHA solving capabilities via external services.
type CaptchaSolver struct {
	apiKey  string
	service string // "2captcha", "anticaptcha", "capsolver"
	client  *http.Client
}

// CaptchaResult contains the solved CAPTCHA token.
type CaptchaResult struct {
	Token   string `json:"token"`
	TaskID  string `json:"task_id"`
	Cost    string `json:"cost"`
	Elapsed time.Duration
}

// NewCaptchaSolver creates a CAPTCHA solver for the given service.
func NewCaptchaSolver(service, apiKey string) *CaptchaSolver {
	return &CaptchaSolver{
		apiKey:  apiKey,
		service: service,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// SolveRecaptchaV2 solves a reCAPTCHA v2 challenge.
func (s *CaptchaSolver) SolveRecaptchaV2(ctx context.Context, siteKey, pageURL string) (*CaptchaResult, error) {
	start := time.Now()

	switch s.service {
	case "2captcha":
		return s.solve2CaptchaRecaptcha(ctx, siteKey, pageURL, "NoCaptchaTaskProxyless", start)
	case "capsolver":
		return s.solveCapsolverRecaptcha(ctx, siteKey, pageURL, "ReCaptchaV2TaskProxyLess", start)
	default:
		return nil, fmt.Errorf("unsupported captcha service: %s", s.service)
	}
}

// SolveHCaptcha solves an hCaptcha challenge.
func (s *CaptchaSolver) SolveHCaptcha(ctx context.Context, siteKey, pageURL string) (*CaptchaResult, error) {
	start := time.Now()

	switch s.service {
	case "2captcha":
		return s.solve2CaptchaRecaptcha(ctx, siteKey, pageURL, "HCaptchaTaskProxyless", start)
	case "capsolver":
		return s.solveCapsolverRecaptcha(ctx, siteKey, pageURL, "HCaptchaTaskProxyLess", start)
	default:
		return nil, fmt.Errorf("unsupported captcha service: %s", s.service)
	}
}

func (s *CaptchaSolver) solve2CaptchaRecaptcha(ctx context.Context, siteKey, pageURL, taskType string, start time.Time) (*CaptchaResult, error) {
	// Create task
	body := map[string]any{
		"clientKey": s.apiKey,
		"task": map[string]any{
			"type":       taskType,
			"websiteURL": pageURL,
			"websiteKey": siteKey,
		},
	}
	bodyJSON, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.2captcha.com/createTask", strings.NewReader(string(bodyJSON)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create task failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var createResp struct {
		ErrorID int    `json:"errorId"`
		TaskID  int    `json:"taskId"`
		ErrorDesc string `json:"errorDescription"`
	}
	if err := json.Unmarshal(respBody, &createResp); err != nil {
		return nil, fmt.Errorf("parse create response: %w", err)
	}
	if createResp.ErrorID != 0 {
		return nil, fmt.Errorf("2captcha error: %s", createResp.ErrorDesc)
	}

	// Poll for result (max 120s)
	for i := 0; i < 40; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}

		getBody := map[string]any{
			"clientKey": s.apiKey,
			"taskId":    createResp.TaskID,
		}
		getJSON, _ := json.Marshal(getBody)
		getReq, _ := http.NewRequestWithContext(ctx, "POST", "https://api.2captcha.com/getTaskResult", strings.NewReader(string(getJSON)))
		getReq.Header.Set("Content-Type", "application/json")

		getResp, err := s.client.Do(getReq)
		if err != nil {
			continue
		}
		getRespBody, _ := io.ReadAll(getResp.Body)
		getResp.Body.Close()

		var result struct {
			Status   string `json:"status"`
			Solution struct {
				Token string `json:"gRecaptchaResponse"`
			} `json:"solution"`
			Cost string `json:"cost"`
		}
		if err := json.Unmarshal(getRespBody, &result); err != nil {
			continue
		}
		if result.Status == "ready" {
			return &CaptchaResult{
				Token:   result.Solution.Token,
				TaskID:  fmt.Sprintf("%d", createResp.TaskID),
				Cost:    result.Cost,
				Elapsed: time.Since(start),
			}, nil
		}
	}
	return nil, fmt.Errorf("captcha solving timed out after 120s")
}

func (s *CaptchaSolver) solveCapsolverRecaptcha(ctx context.Context, siteKey, pageURL, taskType string, start time.Time) (*CaptchaResult, error) {
	body := map[string]any{
		"clientKey": s.apiKey,
		"task": map[string]any{
			"type":       taskType,
			"websiteURL": pageURL,
			"websiteKey": siteKey,
		},
	}
	bodyJSON, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.capsolver.com/createTask", strings.NewReader(string(bodyJSON)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create task failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var createResp struct {
		ErrorID int    `json:"errorId"`
		TaskID  string `json:"taskId"`
		ErrorDesc string `json:"errorDescription"`
	}
	if err := json.Unmarshal(respBody, &createResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if createResp.ErrorID != 0 {
		return nil, fmt.Errorf("capsolver error: %s", createResp.ErrorDesc)
	}

	for i := 0; i < 40; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}

		getBody := map[string]any{"clientKey": s.apiKey, "taskId": createResp.TaskID}
		getJSON, _ := json.Marshal(getBody)
		getReq, _ := http.NewRequestWithContext(ctx, "POST", "https://api.capsolver.com/getTaskResult", strings.NewReader(string(getJSON)))
		getReq.Header.Set("Content-Type", "application/json")

		getResp, err := s.client.Do(getReq)
		if err != nil {
			continue
		}
		getRespBody, _ := io.ReadAll(getResp.Body)
		getResp.Body.Close()

		var result struct {
			Status   string `json:"status"`
			Solution struct {
				Token string `json:"gRecaptchaResponse"`
			} `json:"solution"`
		}
		if err := json.Unmarshal(getRespBody, &result); err != nil {
			continue
		}
		if result.Status == "ready" {
			return &CaptchaResult{
				Token:   result.Solution.Token,
				TaskID:  createResp.TaskID,
				Elapsed: time.Since(start),
			}, nil
		}
	}
	return nil, fmt.Errorf("captcha solving timed out after 120s")
}
