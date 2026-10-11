package account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistrationUsesExistingAPIWithoutSessionOrQuota(t *testing.T) {
	status := map[string]any{"register_enabled": true, "password_register_enabled": true}
	var received map[string]string
	var requests int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("registration leaked account secrets")
		}
		switch r.URL.Path {
		case "/api/status":
			okJSON(w, status)
		case "/api/user/register":
			requests++
			if r.Method != "POST" {
				t.Error("register must POST")
			}
			_ = json.NewDecoder(r.Body).Decode(&received)
			okJSON(w, nil)
		default:
			t.Error("unexpected endpoint", r.URL.Path)
		}
	}))
	defer s.Close()
	store := &memoryStore{}
	c := newClient(s.URL, store, http.DefaultTransport)
	result := c.Register(context.Background(), " new-user ", " password ", "unused@example.com", "unused", false)
	if !result.Success || requests != 1 || len(received) != 2 || received["username"] != "new-user" || received["password"] != " password " {
		t.Fatalf("unexpected result/payload: %+v, keys %d", result, len(received))
	}
	if store.value != "" || c.snapshot().State != "signed_out" || c.snapshot().HasSession {
		t.Fatal("registration must not sign in or persist credentials")
	}
	status["email_verification"], status["user_agreement_enabled"] = true, true
	result = c.Register(context.Background(), "new-user", "password", "user@example.com", "123456", false)
	if result.ErrorCode != "registration_agreement" || requests != 1 {
		t.Fatal("agreement was bypassed")
	}
	result = c.Register(context.Background(), "new-user", "password", "user@example.com", "123456", true)
	if !result.Success || received["email"] != "user@example.com" || received["verification_code"] != "123456" || len(received) != 4 {
		t.Fatal("verified-email registration payload incorrect")
	}
}

func TestRegistrationRechecksPolicyAndValidatesInput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   map[string]any
		expected string
	}{
		{"closed", map[string]any{"register_enabled": false, "password_register_enabled": true}, "registration_disabled"},
		{"password closed", map[string]any{"register_enabled": true, "password_register_enabled": false}, "registration_disabled"},
		{"challenge", map[string]any{"register_enabled": true, "password_register_enabled": true, "turnstile_check": true}, "registration_website"},
		{"encryption", map[string]any{"register_enabled": true, "password_register_enabled": true, "password_login_encryption_enabled": true}, "registration_website"},
		{"unknown", map[string]any{}, "protocol"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/status" {
					t.Error("policy bypassed")
				}
				okJSON(w, tc.status)
			}))
			defer s.Close()
			c := newClient(s.URL, &memoryStore{}, http.DefaultTransport)
			if got := c.Register(context.Background(), "user", "password", "", "", true); got.ErrorCode != tc.expected {
				t.Fatalf("%+v", got)
			}
			if got := c.SendRegistrationCode(context.Background(), "user@example.com"); got.ErrorCode != tc.expected {
				t.Fatalf("%+v", got)
			}
		})
	}
	c := newClient("http://unused.invalid", &memoryStore{}, http.DefaultTransport)
	for _, input := range [][2]string{{"", "password"}, {strings.Repeat("中", 21), "password"}, {"user", "short"}, {"user", strings.Repeat("x", 21)}} {
		if got := c.Register(context.Background(), input[0], input[1], "", "", false); got.ErrorCode != "registration_input" {
			t.Fatalf("%+v", got)
		}
	}
	if got := c.SendRegistrationCode(context.Background(), "User <user@example.com>"); got.ErrorCode != "registration_input" {
		t.Fatal("display-name email accepted")
	}
	c.session.UserID = 7
	c.loaded = true
	if got := c.Register(context.Background(), "user", "password", "", "", false); got.ErrorCode != "logout_first" {
		t.Fatal("active account registration allowed")
	}
}

func TestRegistrationEmailAndAmbiguousCommit(t *testing.T) {
	registerCount := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			okJSON(w, map[string]any{"register_enabled": true, "password_register_enabled": true, "email_verification": true})
		case "/api/verification":
			if r.Method != "GET" || r.URL.Query().Get("email") != "user+tag@example.com" || len(r.URL.Query()) != 1 {
				t.Error("email query not encoded")
			}
			okJSON(w, nil)
		case "/api/user/register":
			registerCount++
			w.WriteHeader(503)
		default:
			t.Error("unexpected endpoint")
		}
	}))
	defer s.Close()
	c := newClient(s.URL, &memoryStore{}, http.DefaultTransport)
	if got := c.SendRegistrationCode(context.Background(), " user+tag@example.com "); !got.Success {
		t.Fatalf("%+v", got)
	}
	if got := c.Register(context.Background(), "user", "password", "user@example.com", "", false); got.ErrorCode != "registration_input" {
		t.Fatal("empty verification code accepted")
	}
	if got := c.Register(context.Background(), "user", "password", "user@example.com", "123456", false); got.ErrorCode != "registration_unknown" || registerCount != 1 {
		t.Fatalf("unsafe retry or result: %+v", got)
	}
}
