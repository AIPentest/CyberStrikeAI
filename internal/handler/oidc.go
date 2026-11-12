package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/security"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
)

type OIDCHandler struct {
	cfg     config.OIDCConfig
	manager *security.AuthManager
	client  *http.Client
}

func NewOIDCHandler(cfg config.OIDCConfig, manager *security.AuthManager) *OIDCHandler {
	if secret := os.Getenv("CYBERSTRIKE_OIDC_CLIENT_SECRET"); secret != "" {
		cfg.ClientSecret = secret
	}
	return &OIDCHandler{cfg: cfg, manager: manager, client: &http.Client{Timeout: 10 * time.Second}}
}

type discoveryDocument struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

func (h *OIDCHandler) discovery(ctx context.Context) (discoveryDocument, error) {
	var d discoveryDocument
	issuer := strings.TrimRight(h.cfg.Issuer, "/")
	if !h.allowedOIDCURL(issuer) {
		return d, errors.New("OIDC issuer requires HTTPS unless allow_http is enabled")
	}
	discoveryURL := strings.TrimSpace(h.cfg.DiscoveryURL)
	if discoveryURL == "" {
		discoveryURL = issuer + "/.well-known/openid-configuration"
	}
	if !h.allowedOIDCURL(discoveryURL) {
		return d, errors.New("OIDC discovery URL requires HTTPS unless allow_http is enabled")
	}
	err := h.getJSON(ctx, discoveryURL, "", &d)
	if err != nil {
		return d, err
	}
	if d.Issuer != issuer {
		return d, errors.New("OIDC issuer mismatch")
	}
	for _, v := range []string{d.AuthorizationEndpoint, d.TokenEndpoint, d.UserinfoEndpoint, d.JWKSURI} {
		if v != "" && !h.allowedOIDCURL(v) {
			return d, errors.New("OIDC endpoint requires HTTPS unless allow_http is enabled")
		}
	}
	return d, nil
}

// allowedOIDCURL only relaxes transport scheme in explicitly opted-in environments.
func (h *OIDCHandler) allowedOIDCURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	return u.Scheme == "https" || (h.cfg.AllowHTTP && u.Scheme == "http")
}

func (h *OIDCHandler) callbackCookieSecure() bool {
	u, err := url.Parse(h.cfg.RedirectURI)
	return err == nil && u.Scheme == "https"
}
func (h *OIDCHandler) getJSON(ctx context.Context, endpoint, bearer string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("OIDC endpoint returned %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}
func randomURLToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (h *OIDCHandler) Config(c *gin.Context) {
	label := strings.TrimSpace(h.cfg.LoginLabel)
	if label == "" {
		label = "Sign in with SSO"
	}
	c.JSON(http.StatusOK, gin.H{"enabled": h.cfg.Enabled, "login_label": label})
}
func (h *OIDCHandler) Login(c *gin.Context) {
	if !h.cfg.Enabled {
		c.AbortWithStatus(404)
		return
	}
	if h.cfg.ClientID == "" || h.cfg.ClientSecret == "" || !h.allowedOIDCURL(h.cfg.RedirectURI) {
		c.AbortWithStatus(503)
		return
	}
	d, err := h.discovery(c.Request.Context())
	if err != nil {
		c.AbortWithStatus(502)
		return
	}
	state, err := randomURLToken()
	if err != nil {
		c.AbortWithStatus(500)
		return
	}
	nonce, err := randomURLToken()
	if err != nil {
		c.AbortWithStatus(500)
		return
	}
	verifier, err := randomURLToken()
	if err != nil {
		c.AbortWithStatus(500)
		return
	}
	// The transaction is kept in HttpOnly cookies; never persist OAuth secrets in query strings.
	secure := h.callbackCookieSecure()
	for key, value := range map[string]string{"oidc_state": state, "oidc_nonce": nonce, "oidc_verifier": verifier} {
		http.SetCookie(c.Writer, &http.Cookie{Name: key, Value: value, Path: "/api/auth/oidc/", MaxAge: 300, Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	challenge := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {h.cfg.ClientID}, "redirect_uri": {h.cfg.RedirectURI}, "scope": {"openid profile email"}, "state": {state}, "nonce": {nonce}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}}
	c.Redirect(302, d.AuthorizationEndpoint+"?"+q.Encode())
}
func (h *OIDCHandler) Callback(c *gin.Context) {
	if !h.cfg.Enabled {
		c.AbortWithStatus(404)
		return
	}
	cookies := map[string]string{}
	for _, key := range []string{"oidc_state", "oidc_nonce", "oidc_verifier"} {
		cookie, err := c.Request.Cookie(key)
		if err == nil {
			cookies[key] = cookie.Value
		}
		http.SetCookie(c.Writer, &http.Cookie{Name: key, Path: "/api/auth/oidc/", MaxAge: -1, Secure: h.callbackCookieSecure(), HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	state := c.Query("state")
	if state == "" || len(cookies["oidc_state"]) != len(state) || subtle.ConstantTimeCompare([]byte(state), []byte(cookies["oidc_state"])) != 1 || cookies["oidc_nonce"] == "" || cookies["oidc_verifier"] == "" {
		c.AbortWithStatus(401)
		return
	}
	if c.Query("error") != "" || c.Query("code") == "" {
		c.AbortWithStatus(401)
		return
	}
	d, err := h.discovery(c.Request.Context())
	if err != nil {
		c.AbortWithStatus(502)
		return
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {c.Query("code")}, "redirect_uri": {h.cfg.RedirectURI}, "client_id": {h.cfg.ClientID}, "client_secret": {h.cfg.ClientSecret}, "code_verifier": {cookies["oidc_verifier"]}}
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		c.AbortWithStatus(500)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := h.client.Do(req)
	if err != nil {
		c.AbortWithStatus(502)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		c.AbortWithStatus(401)
		return
	}
	var tokens struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tokens) != nil || tokens.IDToken == "" {
		c.AbortWithStatus(401)
		return
	}
	claims, err := h.verifyIDToken(c.Request.Context(), d, tokens.IDToken, cookies["oidc_nonce"])
	if err != nil {
		c.AbortWithStatus(401)
		return
	}
	if d.UserinfoEndpoint != "" && tokens.AccessToken != "" {
		var info map[string]interface{}
		if err = h.getJSON(c.Request.Context(), d.UserinfoEndpoint, tokens.AccessToken, &info); err != nil {
			c.AbortWithStatus(502)
			return
		}
		if info["sub"] != claims["sub"] {
			c.AbortWithStatus(401)
			return
		}
		for k, v := range info {
			claims[k] = v
		}
	}
	username, _ := claims[h.cfg.UsernameClaim()].(string)
	display, _ := claims[h.cfg.DisplayNameClaim()].(string)
	subject, _ := claims["sub"].(string)
	if subject == "" {
		c.AbortWithStatus(401)
		return
	}
	token, expiry, err := h.manager.AuthenticateOIDC(username, display, d.Issuer, subject)
	if err != nil {
		c.AbortWithStatusJSON(403, gin.H{"error": "OIDC account not authorized or username conflict"})
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: "auth_token", Value: token, Path: "/", Expires: expiry, Secure: h.callbackCookieSecure(), HttpOnly: true, SameSite: http.SameSiteLaxMode})
	c.Redirect(302, "/")
}

// verifyIDToken delegates JWT parsing, JWKS key selection, signature and
// standard OIDC claim checks to the maintained go-oidc verifier.
func (h *OIDCHandler) verifyIDToken(ctx context.Context, d discoveryDocument, raw, nonce string) (map[string]interface{}, error) {
	if d.JWKSURI == "" {
		return nil, errors.New("OIDC JWKS URI is missing")
	}
	ctx = oidc.ClientContext(ctx, h.client)
	verifier := oidc.NewVerifier(d.Issuer, oidc.NewRemoteKeySet(ctx, d.JWKSURI), &oidc.Config{
		ClientID:             h.cfg.ClientID,
		SupportedSigningAlgs: []string{"RS256"},
	})
	idToken, err := verifier.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}
	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}
	if idToken.Nonce != nonce || nonce == "" {
		return nil, errors.New("OIDC nonce mismatch")
	}
	return claims, nil
}
