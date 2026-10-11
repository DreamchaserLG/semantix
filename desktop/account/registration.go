package account

import (
	"context"
	"encoding/json"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Registration uses the relay's existing user database and policy. No local
// account, credential, role or quota is created by the desktop.
type RegistrationOptions struct {
	Enabled           bool   `json:"enabled"`
	EmailVerification bool   `json:"emailVerification"`
	UserAgreement     bool   `json:"userAgreement"`
	PrivacyPolicy     bool   `json:"privacyPolicy"`
	WebsiteRequired   bool   `json:"websiteRequired"`
	ErrorCode         string `json:"errorCode,omitempty"`
}

type RegistrationResult struct {
	Success   bool   `json:"success"`
	ErrorCode string `json:"errorCode,omitempty"`
}

func (c *Client) registrationOptions(ctx context.Context) RegistrationOptions {
	data, _, code := c.request(ctx, "GET", "/api/status", nil, nil)
	var status struct {
		Register          *bool `json:"register_enabled"`
		PasswordRegister  *bool `json:"password_register_enabled"`
		EmailVerification bool  `json:"email_verification"`
		UserAgreement     bool  `json:"user_agreement_enabled"`
		PrivacyPolicy     bool  `json:"privacy_policy_enabled"`
		Turnstile         bool  `json:"turnstile_check"`
		Encryption        bool  `json:"password_login_encryption_enabled"`
	}
	if code == "" && (json.Unmarshal(data, &status) != nil || status.Register == nil || status.PasswordRegister == nil) {
		code = "protocol"
	}
	if code != "" {
		return RegistrationOptions{ErrorCode: code}
	}
	return RegistrationOptions{Enabled: *status.Register && *status.PasswordRegister,
		EmailVerification: status.EmailVerification, UserAgreement: status.UserAgreement,
		PrivacyPolicy: status.PrivacyPolicy, WebsiteRequired: status.Turnstile || status.Encryption}
}

func (c *Client) RegistrationOptions(ctx context.Context) RegistrationOptions {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.registrationOptions(ctx)
}

func (c *Client) registrationAllowed(ctx context.Context) (RegistrationOptions, string) {
	c.load()
	if c.session.UserID != 0 || c.logoutPending != nil || c.flow != "" {
		return RegistrationOptions{}, "logout_first"
	}
	for _, warning := range c.view.WarningCodes {
		if warning == "storage_delete" {
			return RegistrationOptions{}, warning
		}
	}
	options := c.registrationOptions(ctx)
	if options.ErrorCode != "" {
		return options, options.ErrorCode
	}
	if !options.Enabled {
		return options, "registration_disabled"
	}
	if options.WebsiteRequired {
		return options, "registration_website"
	}
	return options, ""
}

func validEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return len(email) <= 254 && err == nil && address.Address == email && strings.Contains(email, "@")
}

func (c *Client) Register(ctx context.Context, username, password, email, verificationCode string, agreed bool) RegistrationResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	username, email, verificationCode = strings.TrimSpace(username), strings.TrimSpace(email), strings.TrimSpace(verificationCode)
	if !utf8.ValidString(username) || !utf8.ValidString(password) || utf8.RuneCountInString(username) < 1 || utf8.RuneCountInString(username) > 20 || utf8.RuneCountInString(password) < 8 || utf8.RuneCountInString(password) > 20 {
		return RegistrationResult{ErrorCode: "registration_input"}
	}
	options, code := c.registrationAllowed(ctx)
	if code != "" {
		return RegistrationResult{ErrorCode: code}
	}
	if (options.UserAgreement || options.PrivacyPolicy) && !agreed {
		return RegistrationResult{ErrorCode: "registration_agreement"}
	}
	input := map[string]string{"username": username, "password": password}
	if options.EmailVerification {
		if !validEmail(email) || verificationCode == "" || len(verificationCode) > 32 {
			return RegistrationResult{ErrorCode: "registration_input"}
		}
		input["email"], input["verification_code"] = email, verificationCode
	}
	_, _, code = c.request(ctx, "POST", "/api/user/register", input, nil)
	// A transport failure may occur after the server committed the new account.
	// Never retry automatically or claim that the account was not created.
	if code == "network" || code == "unavailable" || code == "protocol" {
		code = "registration_unknown"
	} else if code == "rejected" {
		code = "registration_rejected"
	}
	return RegistrationResult{Success: code == "", ErrorCode: code}
}

func (c *Client) SendRegistrationCode(ctx context.Context, email string) RegistrationResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	email = strings.TrimSpace(email)
	if !validEmail(email) {
		return RegistrationResult{ErrorCode: "registration_input"}
	}
	options, code := c.registrationAllowed(ctx)
	if code == "" && !options.EmailVerification {
		code = "registration_input"
	}
	if code == "" {
		_, _, code = c.request(ctx, "GET", "/api/verification?email="+url.QueryEscape(email), nil, nil)
	}
	return RegistrationResult{Success: code == "", ErrorCode: code}
}
