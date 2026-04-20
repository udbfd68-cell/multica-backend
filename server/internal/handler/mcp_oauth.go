package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/crypto"
	"github.com/multica-ai/multica/server/internal/oauth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// MCP OAuth Flow — Google (Gmail, Calendar, Drive, etc.)
//
// 1. Frontend calls POST /api/v1/mcp/oauth/init with desired scopes
// 2. Server returns a Google OAuth consent URL with state param
// 3. User consents → Google redirects to /api/v1/mcp/oauth/callback
// 4. Server exchanges code for access+refresh tokens, encrypts them,
//    stores in vault_credential, and redirects back to the app
// ---------------------------------------------------------------------------

// googleOAuthScopes maps friendly names to Google OAuth2 scopes.
var googleOAuthScopes = map[string][]string{
	"gmail": {
		"https://www.googleapis.com/auth/gmail.modify",
		"https://www.googleapis.com/auth/gmail.send",
		"https://www.googleapis.com/auth/gmail.readonly",
	},
	"calendar": {
		"https://www.googleapis.com/auth/calendar",
		"https://www.googleapis.com/auth/calendar.events",
	},
	"drive": {
		"https://www.googleapis.com/auth/drive",
		"https://www.googleapis.com/auth/drive.file",
	},
	"sheets": {
		"https://www.googleapis.com/auth/spreadsheets",
	},
	"docs": {
		"https://www.googleapis.com/auth/documents",
	},
	"google-workspace": {
		"https://www.googleapis.com/auth/gmail.modify",
		"https://www.googleapis.com/auth/gmail.send",
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/calendar",
		"https://www.googleapis.com/auth/calendar.events",
		"https://www.googleapis.com/auth/drive",
		"https://www.googleapis.com/auth/drive.file",
		"https://www.googleapis.com/auth/spreadsheets",
		"https://www.googleapis.com/auth/documents",
	},
}

// InitMcpOAuth starts the OAuth flow for an MCP server connection.
// POST /api/v1/mcp/oauth/init
// Body: { "provider": "gmail"|"calendar"|"drive"|"google-workspace",
//         "agent_id": "uuid", "connector_id": "uuid",
//         "vault_id": "uuid", "redirect_url": "https://..." }
// Returns: { "auth_url": "https://accounts.google.com/..." }
func (h *Handler) InitMcpOAuth(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	var req struct {
		Provider    string `json:"provider"`
		AgentID     string `json:"agent_id"`
		ConnectorID string `json:"connector_id"`
		VaultID     string `json:"vault_id"`
		RedirectURL string `json:"redirect_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}

	scopes, ok := googleOAuthScopes[req.Provider]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported provider: %s (supported: gmail, calendar, drive, google-workspace)", req.Provider))
		return
	}

	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "Google OAuth not configured — set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET")
		return
	}

	// Generate random state with embedded metadata
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}
	nonce := hex.EncodeToString(stateBytes)

	// Encode state as: nonce|provider|agent_id|connector_id|vault_id|redirect_url
	// This is encrypted and stored as a cookie, not sent in the URL
	stateData := map[string]string{
		"nonce":        nonce,
		"provider":     req.Provider,
		"agent_id":     req.AgentID,
		"connector_id": req.ConnectorID,
		"vault_id":     req.VaultID,
		"workspace_id": ctxWorkspaceID(r.Context()),
		"redirect_url": req.RedirectURL,
	}
	stateJSON, _ := json.Marshal(stateData)
	encryptedState, err := crypto.Encrypt(string(stateJSON))
	if err != nil {
		slog.Error("failed to encrypt oauth state", "error", err)
		writeError(w, http.StatusInternalServerError, "encryption error")
		return
	}

	// Store encrypted state in a secure cookie (HttpOnly, SameSite=Lax)
	http.SetCookie(w, &http.Cookie{
		Name:     "mcp_oauth_state",
		Value:    encryptedState,
		Path:     "/api/v1/mcp/oauth/callback",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})

	// Build callback URL
	callbackURL := os.Getenv("MCP_OAUTH_CALLBACK_URL")
	if callbackURL == "" {
		// Auto-detect from request
		scheme := "https"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
			scheme = "http"
		}
		host := r.Host
		callbackURL = fmt.Sprintf("%s://%s/api/v1/mcp/oauth/callback", scheme, host)
	}

	// Always include openid + email so we can identify the user
	allScopes := append([]string{"openid", "email"}, scopes...)

	// Build Google OAuth2 URL
	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?"+
			"client_id=%s&"+
			"redirect_uri=%s&"+
			"response_type=code&"+
			"scope=%s&"+
			"state=%s&"+
			"access_type=offline&"+
			"prompt=consent",
		url.QueryEscape(clientID),
		url.QueryEscape(callbackURL),
		url.QueryEscape(strings.Join(allScopes, " ")),
		url.QueryEscape(nonce),
	)

	writeJSON(w, http.StatusOK, map[string]string{
		"auth_url": authURL,
		"state":    nonce,
	})
}

// McpOAuthCallback handles the OAuth callback from Google.
// GET /api/v1/mcp/oauth/callback?code=...&state=...
// Exchanges code for tokens, encrypts them, stores in vault_credential,
// and redirects back to the app.
func (h *Handler) McpOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateNonce := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	if errorParam != "" {
		slog.Warn("OAuth callback received error", "error", errorParam)
		http.Redirect(w, r, "/?error=oauth_denied", http.StatusTemporaryRedirect)
		return
	}

	if code == "" || stateNonce == "" {
		writeError(w, http.StatusBadRequest, "missing code or state parameter")
		return
	}

	// Retrieve and decrypt state from cookie
	cookie, err := r.Cookie("mcp_oauth_state")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing oauth state cookie — please restart the connection flow")
		return
	}

	decrypted, err := crypto.Decrypt(cookie.Value)
	if err != nil {
		slog.Error("failed to decrypt oauth state", "error", err)
		writeError(w, http.StatusBadRequest, "invalid oauth state")
		return
	}

	var stateData map[string]string
	if err := json.Unmarshal([]byte(decrypted), &stateData); err != nil {
		writeError(w, http.StatusBadRequest, "corrupt oauth state")
		return
	}

	// Verify nonce matches
	if stateData["nonce"] != stateNonce {
		writeError(w, http.StatusBadRequest, "oauth state mismatch — possible CSRF")
		return
	}

	// Clear the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "mcp_oauth_state",
		Value:    "",
		Path:     "/api/v1/mcp/oauth/callback",
		MaxAge:   -1,
		HttpOnly: true,
	})

	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	callbackURL := os.Getenv("MCP_OAUTH_CALLBACK_URL")
	if callbackURL == "" {
		scheme := "https"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
			scheme = "http"
		}
		callbackURL = fmt.Sprintf("%s://%s/api/v1/mcp/oauth/callback", scheme, r.Host)
	}

	// Exchange authorization code for tokens
	tokenResp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {callbackURL},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		slog.Error("google oauth token exchange failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange code with Google")
		return
	}
	defer tokenResp.Body.Close()

	body, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read token response")
		return
	}

	if tokenResp.StatusCode != http.StatusOK {
		slog.Error("google oauth token exchange error", "status", tokenResp.StatusCode, "body", string(body))
		writeError(w, http.StatusBadRequest, "Google rejected the authorization code")
		return
	}

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokenData); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse token response")
		return
	}

	if tokenData.AccessToken == "" {
		writeError(w, http.StatusBadGateway, "no access token received from Google")
		return
	}

	// Fetch user email for display
	email := fetchGoogleEmail(tokenData.AccessToken)

	// Build credential payload
	credPayload := map[string]any{
		"type":          "mcp_oauth",
		"provider":      stateData["provider"],
		"access_token":  tokenData.AccessToken,
		"refresh_token": tokenData.RefreshToken,
		"expires_in":    tokenData.ExpiresIn,
		"token_type":    tokenData.TokenType,
		"scope":         tokenData.Scope,
		"email":         email,
		"obtained_at":   time.Now().UTC().Format(time.RFC3339),
	}
	credJSON, _ := json.Marshal(credPayload)

	// Encrypt the credential
	encrypted, err := crypto.Encrypt(string(credJSON))
	if err != nil {
		slog.Error("failed to encrypt oauth credential", "error", err)
		writeError(w, http.StatusInternalServerError, "encryption failed")
		return
	}

	// Calculate expiry
	var expiresAt *time.Time
	if tokenData.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(tokenData.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	// Store in vault_credential
	vaultID := stateData["vault_id"]
	provider := stateData["provider"]
	connectorID := stateData["connector_id"]
	redirectURL := stateData["redirect_url"]

	if vaultID != "" {
		params := db.CreateVaultCredentialParams{
			VaultID:          parseUUID(vaultID),
			McpServerUrl:     fmt.Sprintf("google://%s", provider),
			AuthType:         "mcp_oauth",
			EncryptedPayload: []byte(encrypted),
		}
		if expiresAt != nil {
			params.ExpiresAt.Time = *expiresAt
			params.ExpiresAt.Valid = true
		}

		cred, err := h.Queries.CreateVaultCredential(r.Context(), params)
		if err != nil {
			slog.Error("failed to store oauth credential", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to store credential")
			return
		}

		// If connector_id provided, link credential to connector
		if connectorID != "" {
			workspaceID := stateData["workspace_id"]
			if workspaceID != "" {
				h.Queries.UpdateAgentMcpConnector(r.Context(), db.UpdateAgentMcpConnectorParams{
					ID:                parseUUID(connectorID),
					WorkspaceID:       parseUUID(workspaceID),
					VaultCredentialID: cred.ID,
				})
				// Update connector status to connected
				statusMsg := fmt.Sprintf("OAuth connected as %s", email)
				h.Queries.UpdateAgentMcpConnectorStatus(r.Context(), parseUUID(connectorID), "connected", &statusMsg)
			}
		}

		slog.Info("MCP OAuth credential stored",
			"provider", provider,
			"email", email,
			"vault_id", vaultID,
			"credential_id", uuidToString(cred.ID),
			"connector_id", connectorID)
	}

	// Redirect back to the app
	if redirectURL == "" {
		redirectURL = "/"
	}
	// Append success indicator
	sep := "?"
	if strings.Contains(redirectURL, "?") {
		sep = "&"
	}
	redirectURL += sep + "mcp_oauth=success&provider=" + url.QueryEscape(provider) + "&email=" + url.QueryEscape(email)

	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

// RefreshGoogleToken refreshes an expired Google OAuth token.
// Delegates to the oauth package to avoid circular imports.
var RefreshGoogleToken = oauth.RefreshGoogleToken

// fetchGoogleEmail fetches the user's email from the Google userinfo endpoint.
func fetchGoogleEmail(accessToken string) string {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var info struct {
		Email string `json:"email"`
	}
	json.NewDecoder(resp.Body).Decode(&info)
	return info.Email
}


