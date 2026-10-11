// Package account connects the desktop's product identity to the Semantix New API
// account service. Dashboard credentials never become model-provider credentials.
package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Origin = "https://sem.ensureok.ai"
const refreshCookie = "new_api_refresh"
const TokensPerCredit int64 = 5000

// View is the only account data exposed to the WebView. Numeric quotas remain
// decimal strings across the bridge, including values beyond JS's safe integer.
type View struct {
	State        string   `json:"state"`
	SiteURL      string   `json:"siteURL"`
	User         *User    `json:"user,omitempty"`
	SyncedAt     string   `json:"syncedAt,omitempty"`
	Stale        bool     `json:"stale"`
	Remembered   bool     `json:"remembered"`
	HasSession   bool     `json:"hasSession"`
	ErrorCode    string   `json:"errorCode,omitempty"`
	WarningCodes []string `json:"warningCodes"`
}

type User struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	DisplayName   string `json:"displayName"`
	Group         string `json:"group"`
	Credits       string `json:"credits"`
	UsedCredits   string `json:"usedCredits"`
	BillingTokens string `json:"billingTokens"`
	RequestCount  string `json:"requestCount"`
}

type userResponse struct {
	ID           int64        `json:"id"`
	Username     string       `json:"username"`
	DisplayName  string       `json:"display_name"`
	Group        string       `json:"group"`
	Status       int          `json:"status"`
	Quota        *json.Number `json:"quota"`
	UsedQuota    *json.Number `json:"used_quota"`
	RequestCount *json.Number `json:"request_count"`
}

type bundle struct {
	AccessToken     string       `json:"access_token"`
	TokenType       string       `json:"token_type"`
	AccessExpiresAt int64        `json:"access_expires_at"`
	User            userResponse `json:"user"`
	Session         struct {
		SID       string `json:"sid"`
		Current   bool   `json:"current"`
		ExpiresAt int64  `json:"expires_at"`
	} `json:"session"`
	RequireVerification bool   `json:"require_verification"`
	FlowToken           string `json:"flow_token"`
	ExpiresAt           int64  `json:"expires_at"`
	Methods             []struct {
		Method    string `json:"method"`
		Available bool   `json:"available"`
	} `json:"methods"`
}

// savedSession is stored only in the OS credential vault; no password or access token.
type savedSession struct {
	Origin    string `json:"origin"`
	UserID    int64  `json:"userID"`
	SID       string `json:"sid"`
	Cookie    string `json:"cookie"`
	ExpiresAt int64  `json:"expiresAt"`
}

type secretStore interface {
	Load() (string, error)
	Save(string) error
	Delete() error
}

var errNotFound = errors.New("account credential not found")

type Client struct {
	mu            sync.Mutex
	store         secretStore
	http          *http.Client
	origin        string
	loaded        bool
	view          View
	session       savedSession
	access        string
	accessExpiry  int64
	flow          string
	flowExpiry    int64
	logoutPending *savedSession
}

func New() *Client { return newClient(Origin, vaultStore{}, http.DefaultTransport) }

func newClient(origin string, store secretStore, transport http.RoundTripper) *Client {
	return &Client{origin: origin, store: store, view: View{State: "signed_out", SiteURL: origin}, http: &http.Client{
		Timeout: 15 * time.Second, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *Client) snapshot() View {
	v := c.view
	v.HasSession = c.session.UserID != 0 || c.logoutPending != nil
	v.WarningCodes = append([]string{}, c.view.WarningCodes...)
	if v.User != nil {
		u := *v.User
		v.User = &u
	}
	return v
}

func (c *Client) warn(code string) {
	for _, existing := range c.view.WarningCodes {
		if existing == code {
			return
		}
	}
	c.view.WarningCodes = append(c.view.WarningCodes, code)
}

func (c *Client) clearLocal() {
	c.loaded = true
	c.session, c.access, c.accessExpiry, c.flow, c.flowExpiry = savedSession{}, "", 0, "", 0
	c.view = View{State: "signed_out", SiteURL: c.origin}
	if err := c.store.Delete(); err != nil && !errors.Is(err, errNotFound) {
		c.warn("storage_delete")
	}
}

func (c *Client) persist() {
	data, _ := json.Marshal(c.session)
	c.view.Remembered = c.store.Save(string(data)) == nil
	if !c.view.Remembered {
		c.warn("storage_unavailable")
	} else {
		kept := c.view.WarningCodes[:0]
		for _, warning := range c.view.WarningCodes {
			if warning != "storage_unavailable" {
				kept = append(kept, warning)
			}
		}
		c.view.WarningCodes = kept
	}
}

func (c *Client) load() {
	if c.loaded {
		return
	}
	c.loaded = true
	raw, err := c.store.Load()
	if errors.Is(err, errNotFound) {
		return
	}
	if err != nil {
		c.warn("storage_unavailable")
		return
	}
	var s savedSession
	if len(raw) > 8192 || json.Unmarshal([]byte(raw), &s) != nil || s.Origin != c.origin || s.UserID <= 0 || !validSecret(s.SID, 128) || !validSecret(s.Cookie, 4096) || s.ExpiresAt <= time.Now().Unix() {
		c.clearLocal()
		c.view.ErrorCode = "session_expired"
		return
	}
	c.session = s
	c.view.Remembered = true
}

func validSecret(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for _, b := range []byte(s) {
		if b <= 32 || b >= 127 || b == ';' || b == ',' || b == '"' || b == '\\' {
			return false
		}
	}
	return true
}

// Status verifies restored sessions before exposing identity or balance.
func (c *Client) Status(ctx context.Context) View {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.view.ErrorCode = ""
	c.load()
	if c.view.State == "verifying" {
		if c.flowExpiry <= time.Now().Unix() {
			c.clearLocal()
			c.view.ErrorCode = "session_expired"
		}
		return c.snapshot()
	}
	if c.session.UserID == 0 {
		return c.snapshot()
	}
	if c.accessExpiry <= time.Now().Unix()+30 {
		data, cookie, code := c.request(ctx, "POST", "/api/user/auth/refresh", nil, &c.session)
		if code == "" {
			code = c.acceptBundle(data, cookie, true)
		}
		if code != "" {
			c.fail(code)
			return c.snapshot()
		}
		// A rotated refresh secret must be saved even if the next self query fails.
		c.persist()
	}
	if code := c.fetchSelf(ctx); code != "" {
		c.fail(code)
	} else if !c.view.Remembered {
		c.persist()
	}
	return c.snapshot()
}

func (c *Client) Login(ctx context.Context, username, password string) View {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	if c.session.UserID != 0 || c.logoutPending != nil {
		c.view.ErrorCode = "logout_first"
		return c.snapshot()
	}
	for _, warning := range c.view.WarningCodes {
		if warning == "storage_delete" {
			c.view.ErrorCode = "storage_delete"
			return c.snapshot()
		}
	}
	c.flow, c.flowExpiry, c.view.State, c.view.ErrorCode = "", 0, "signed_out", ""
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 128 || password == "" || len(password) > 1024 {
		c.view.ErrorCode = "invalid_input"
		return c.snapshot()
	}
	data, _, code := c.request(ctx, "GET", "/api/status", nil, nil)
	var status struct {
		PasswordLogin *bool `json:"password_login_enabled"`
		Turnstile     bool  `json:"turnstile_check"`
		Encryption    bool  `json:"password_login_encryption_enabled"`
	}
	if code == "" && (json.Unmarshal(data, &status) != nil || status.PasswordLogin == nil) {
		code = "protocol"
	}
	if code == "" && (!*status.PasswordLogin || status.Turnstile || status.Encryption) {
		code = "unsupported_verification"
	}
	if code != "" {
		c.view.ErrorCode = code
		return c.snapshot()
	}
	data, cookie, code := c.request(ctx, "POST", "/api/user/login", map[string]string{"username": username, "password": password}, nil)
	if code == "rejected" || code == "session_expired" {
		code = "invalid_credentials"
	}
	if code == "" {
		code = c.completeLogin(ctx, data, cookie, true)
	}
	c.view.ErrorCode = code
	return c.snapshot()
}

func (c *Client) Verify(ctx context.Context, code string) View {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.view.State != "verifying" {
		c.view.ErrorCode = "invalid_input"
		return c.snapshot()
	}
	if c.flowExpiry <= time.Now().Unix() {
		c.clearLocal()
		c.view.ErrorCode = "session_expired"
		return c.snapshot()
	}
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 128 {
		c.view.ErrorCode = "invalid_input"
		return c.snapshot()
	}
	data, cookie, failure := c.request(ctx, "POST", "/api/user/login/verify", map[string]string{"flow_token": c.flow, "method": "2fa", "code": code}, nil)
	if failure == "rejected" {
		failure = "invalid_code"
	}
	if failure == "" {
		failure = c.completeLogin(ctx, data, cookie, false)
	}
	if failure == "session_expired" {
		c.clearLocal()
	}
	c.view.ErrorCode = failure
	return c.snapshot()
}

func (c *Client) completeLogin(ctx context.Context, data json.RawMessage, cookie *http.Cookie, allowChallenge bool) string {
	var b bundle
	if json.Unmarshal(data, &b) != nil {
		return "protocol"
	}
	if b.RequireVerification {
		available := false
		for _, m := range b.Methods {
			available = available || (m.Method == "2fa" && m.Available)
		}
		if !allowChallenge || !available || !validSecret(b.FlowToken, 4096) || b.ExpiresAt <= time.Now().Unix() {
			return "unsupported_verification"
		}
		c.flow, c.flowExpiry = b.FlowToken, min(b.ExpiresAt, time.Now().Add(10*time.Minute).Unix())
		c.view.State = "verifying"
		return ""
	}
	if code := c.acceptBundle(data, cookie, false); code != "" {
		return code
	}
	c.flow, c.flowExpiry = "", 0
	c.view.State = "signed_out"
	if code := c.fetchSelf(ctx); code != "" {
		c.fail(code)
		return code
	}
	c.persist()
	return ""
}

func (c *Client) acceptBundle(data json.RawMessage, cookie *http.Cookie, restoring bool) string {
	var b bundle
	if json.Unmarshal(data, &b) != nil || b.RequireVerification || b.TokenType != "Bearer" || !validSecret(b.AccessToken, 8192) || b.AccessExpiresAt <= time.Now().Unix() || b.User.ID <= 0 || b.User.Status != 1 || b.User.Username == "" || !b.Session.Current || !validSecret(b.Session.SID, 128) || b.Session.ExpiresAt <= time.Now().Unix() || cookie == nil || !validSecret(cookie.Value, 4096) || cookie.MaxAge < 0 {
		return "protocol"
	}
	if restoring && (b.User.ID != c.session.UserID || b.Session.SID != c.session.SID) {
		return "session_expired"
	}
	expiry := b.Session.ExpiresAt
	if !cookie.Expires.IsZero() {
		expiry = min(expiry, cookie.Expires.Unix())
	}
	if cookie.MaxAge > 0 {
		expiry = min(expiry, time.Now().Unix()+int64(cookie.MaxAge))
	}
	if expiry <= time.Now().Unix() {
		return "session_expired"
	}
	c.session = savedSession{Origin: c.origin, UserID: b.User.ID, SID: b.Session.SID, Cookie: cookie.Value, ExpiresAt: expiry}
	c.access, c.accessExpiry = b.AccessToken, b.AccessExpiresAt
	return ""
}

func (c *Client) fetchSelf(ctx context.Context) string {
	data, _, code := c.request(ctx, "GET", "/api/user/self", nil, &c.session)
	if code != "" {
		return code
	}
	var u userResponse
	if json.Unmarshal(data, &u) != nil || u.Username == "" || len(u.Username) > 256 || len(u.DisplayName) > 512 || len(u.Group) > 256 {
		return "protocol"
	}
	if u.ID != c.session.UserID || u.Status != 1 {
		return "session_expired"
	}
	quota, ok := decimal(u.Quota, true)
	used, usedOK := decimal(u.UsedQuota, false)
	requests, requestsOK := decimal(u.RequestCount, false)
	if !ok || !usedOK || !requestsOK {
		return "protocol"
	}
	c.view.User = &User{ID: strconv.FormatInt(u.ID, 10), Username: u.Username, DisplayName: u.DisplayName, Group: u.Group, Credits: creditsFromTokens(quota), UsedCredits: creditsFromTokens(used), BillingTokens: quota, RequestCount: requests}
	c.view.State, c.view.Stale, c.view.SyncedAt = "signed_in", false, time.Now().UTC().Format(time.RFC3339)
	return ""
}

func decimal(n *json.Number, signed bool) (string, bool) {
	if n == nil {
		return "", false
	}
	v, err := strconv.ParseInt(string(*n), 10, 64)
	return strconv.FormatInt(v, 10), err == nil && (signed || v >= 0)
}

// creditsFromTokens accepts a validated int64 string. Splitting before negating
// handles MinInt64 without overflow; one billing token is exactly 0.0002 credits.
func creditsFromTokens(tokens string) string {
	v, _ := strconv.ParseInt(tokens, 10, 64)
	whole, remainder := v/TokensPerCredit, v%TokensPerCredit
	if whole < 0 {
		whole = -whole
	}
	if remainder < 0 {
		remainder = -remainder
	}
	result := strconv.FormatInt(whole, 10)
	if remainder != 0 {
		fraction := strconv.FormatInt(remainder*2, 10)
		result += "." + strings.TrimRight(strings.Repeat("0", 4-len(fraction))+fraction, "0")
	}
	if v < 0 {
		result = "-" + result
	}
	return result
}

func (c *Client) fail(code string) {
	if code == "session_expired" {
		c.clearLocal()
	} else {
		c.view.Stale = true
	}
	c.view.ErrorCode = code
}

func (c *Client) Logout(ctx context.Context) View {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	s := c.session
	if c.logoutPending != nil {
		s = *c.logoutPending
	}
	code := ""
	if s.UserID != 0 {
		_, _, code = c.request(ctx, "POST", "/api/user/auth/logout", nil, &s)
	}
	c.clearLocal()
	c.logoutPending = nil
	if code != "" && code != "session_expired" {
		c.logoutPending = &s
		c.warn("remote_logout")
	}
	return c.snapshot()
}

// request deliberately never forwards raw server errors or network errors to the
// bridge. Redirects are refused before cookies, passwords or tokens can leave origin.
func (c *Client) request(ctx context.Context, method, path string, input any, session *savedSession) (json.RawMessage, *http.Cookie, string) {
	var body []byte
	if input != nil {
		body, _ = json.Marshal(input)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, "network"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", c.origin)
	req.Header.Set("Cache-Control", "no-store")
	req.Header.Set("User-Agent", "Semantix-Desktop")
	if session != nil {
		if c.access != "" {
			req.Header.Set("Authorization", "Bearer "+c.access)
		}
		if strings.HasPrefix(path, "/api/user/auth/") {
			req.Header.Set("X-Auth-Session", session.SID)
			req.AddCookie(&http.Cookie{Name: refreshCookie, Value: session.Cookie})
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, "network"
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, nil, "session_expired"
	}
	if resp.StatusCode == 429 {
		return nil, nil, "rate_limited"
	}
	if resp.StatusCode >= 500 {
		return nil, nil, "unavailable"
	}
	if resp.StatusCode != 200 {
		return nil, nil, "protocol"
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, nil, "network"
	}
	if len(raw) > 1<<20 {
		return nil, nil, "protocol"
	}
	var response struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return nil, nil, "protocol"
	}
	if !response.Success {
		return nil, nil, "rejected"
	}
	var cookie *http.Cookie
	for _, item := range resp.Cookies() {
		if item.Name == refreshCookie {
			cookie = item
		}
	}
	return response.Data, cookie, ""
}
