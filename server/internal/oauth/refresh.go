package oauth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// RefreshGoogleToken uses a refresh token to obtain a new access token.
// Requires GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET environment variables.
func RefreshGoogleToken(refreshToken string) (accessToken string, expiresIn int, err error) {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		return "", 0, fmt.Errorf("Google OAuth not configured — missing GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET")
	}

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return "", 0, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenData struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenData); err != nil {
		return "", 0, fmt.Errorf("failed to parse refresh response: %w", err)
	}

	return tokenData.AccessToken, tokenData.ExpiresIn, nil
}
