package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	value                       string
	loadErr, saveErr, deleteErr error
}

func (s *memoryStore) Load() (string, error) {
	if s.loadErr != nil {
		return "", s.loadErr
	}
	if s.value == "" {
		return "", errNotFound
	}
	return s.value, nil
}
func (s *memoryStore) Save(v string) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.value = v
	return nil
}
func (s *memoryStore) Delete() error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.value = ""
	return nil
}

func okJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
}
func testUser(id int) map[string]any {
	return map[string]any{"id": id, "username": "tester", "display_name": "Test User", "status": 1, "group": "default", "quota": json.Number("9007199254740993"), "used_quota": 12, "request_count": 4}
}
func writeBundle(w http.ResponseWriter, id int, sid, cookie string) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: cookie, Path: "/api/user/auth", HttpOnly: true, MaxAge: 3600})
	okJSON(w, map[string]any{
		"access_token": "secret-access", "token_type": "Bearer", "access_expires_at": time.Now().Add(time.Minute).Unix(),
		"user": testUser(id), "session": map[string]any{"sid": sid, "current": true, "expires_at": time.Now().Add(time.Hour).Unix()},
	})
}

type fixture struct {
	client *Client
	store  *memoryStore
	mux    *http.ServeMux
	server *httptest.Server
}

func setup(t *testing.T) fixture {
	t.Helper()
	mux := http.NewServeMux()
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	store := &memoryStore{}
	c := newClient(s.URL, store, http.DefaultTransport)
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != s.URL || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected status credentials/origin")
		}
		okJSON(w, map[string]any{"password_login_enabled": true, "turnstile_check": false})
	})
	return fixture{c, store, mux, s}
}

func (f fixture) login(t *testing.T) {
	t.Helper()
	f.mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		var input map[string]string
		_ = json.NewDecoder(r.Body).Decode(&input)
		if r.Method != "POST" || input["username"] != "tester" || input["password"] != " secret-password " {
			t.Error("incorrect login request or password trimmed")
		}
		writeBundle(w, 7, "session-seven", "secret-refresh")
	})
}
func (f fixture) self(t *testing.T) {
	t.Helper()
	f.mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-access" || r.Header.Get("Cookie") != "" {
			t.Error("self must use access token, never refresh cookie")
		}
		okJSON(w, testUser(7))
	})
}
func signIn(c *Client) View { return c.Login(context.Background(), " tester ", " secret-password ") }

func TestLoginRefreshRestoreLogout(t *testing.T) {
	f := setup(t)
	f.login(t)
	f.self(t)
	f.mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie(refreshCookie)
		if r.Method != "POST" || cookie == nil || cookie.Value != "secret-refresh" || r.Header.Get("X-Auth-Session") != "session-seven" || r.Header.Get("Origin") != f.server.URL {
			t.Error("refresh missing bound identity/origin/cookie")
		}
		writeBundle(w, 7, "session-seven", "rotated-refresh")
	})
	f.mux.HandleFunc("/api/user/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie(refreshCookie)
		if r.Method != "POST" || cookie == nil || cookie.Value != "rotated-refresh" {
			t.Error("logout missing rotated cookie")
		}
		okJSON(w, nil)
	})
	v := signIn(f.client)
	if v.State != "signed_in" || v.User.Credits != "1801439850948.1986" || v.User.UsedCredits != "0.0024" || v.User.BillingTokens != "9007199254740993" || !v.Remembered || v.Stale {
		t.Fatalf("login: %+v", v)
	}
	raw, _ := json.Marshal(v)
	for _, secret := range []string{"secret-access", "secret-refresh", "secret-password"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("secret leaked in view")
		}
	}
	for _, secret := range []string{"secret-access", "secret-password"} {
		if strings.Contains(f.store.value, secret) {
			t.Fatal("secret persisted")
		}
	}
	restored := newClient(f.server.URL, f.store, http.DefaultTransport)
	v = restored.Status(context.Background())
	if v.State != "signed_in" || !strings.Contains(f.store.value, "rotated-refresh") {
		t.Fatalf("restore: %+v", v)
	}
	v = restored.Logout(context.Background())
	if v.State != "signed_out" || v.User != nil || f.store.value != "" || v.HasSession {
		t.Fatalf("logout: %+v", v)
	}
	if v := restored.Status(context.Background()); v.State != "signed_out" {
		t.Fatal("logout resurrected")
	}
}

func TestTwoFAIsNotAuthenticatedUntilVerified(t *testing.T) {
	f := setup(t)
	f.self(t)
	f.mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"require_verification": true, "flow_token": "secret-flow", "expires_at": time.Now().Add(time.Minute).Unix(), "methods": []any{map[string]any{"method": "2fa", "available": true}}})
	})
	f.mux.HandleFunc("/api/user/login/verify", func(w http.ResponseWriter, r *http.Request) {
		var data map[string]string
		_ = json.NewDecoder(r.Body).Decode(&data)
		if data["flow_token"] != "secret-flow" || data["method"] != "2fa" {
			t.Error("wrong challenge request")
		}
		if data["code"] != "123456" {
			_, _ = fmt.Fprint(w, `{"success":false,"message":"secret-flow"}`)
			return
		}
		writeBundle(w, 7, "session-seven", "secret-refresh")
	})
	v := signIn(f.client)
	if v.State != "verifying" || v.User != nil || f.store.value != "" {
		t.Fatalf("premature login: %+v", v)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "secret-flow") {
		t.Fatal("flow leaked")
	}
	v = f.client.Verify(context.Background(), "wrong")
	if v.State != "verifying" || v.ErrorCode != "invalid_code" {
		t.Fatalf("bad OTP: %+v", v)
	}
	v = f.client.Verify(context.Background(), "123456")
	if v.State != "signed_in" || !v.Remembered {
		t.Fatalf("verify: %+v", v)
	}
}

func TestLoginRejectsUnsafeResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
	}{
		{"password", `{"success":false,"message":"secret-password"}`, "invalid_credentials", 200},
		{"html", `<html>secret-password</html>`, "protocol", 200},
		{"missing bundle", `{"success":true,"data":{"id":7}}`, "protocol", 200},
		{"missing success", `{"data":{}}`, "invalid_credentials", 200},
		{"unknown challenge", `{"success":true,"data":{"require_verification":true,"methods":[{"method":"passkey","available":true}]}}`, "unsupported_verification", 200},
		{"rate", `secret-password`, "rate_limited", 429},
		{"server", `secret-password`, "unavailable", 503},
		{"large", strings.Repeat("a", (1<<20)+1), "protocol", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); _, _ = fmt.Fprint(w, tc.body) })
			v := signIn(f.client)
			if v.State != "signed_out" || v.User != nil || v.ErrorCode != tc.code || f.store.value != "" {
				t.Fatalf("got %+v", v)
			}
		})
	}
}

func TestRedirectNeverReceivesCredentials(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer target.Close()
	f := setup(t)
	f.mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if v := signIn(f.client); v.ErrorCode != "protocol" || called {
		t.Fatalf("redirect followed: %+v", v)
	}
}

func TestSelfValidation(t *testing.T) {
	for _, field := range []string{"id", "status", "quota", "used_quota", "request_count"} {
		t.Run(field, func(t *testing.T) {
			f := setup(t)
			f.login(t)
			f.mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
				u := testUser(7)
				switch field {
				case "id":
					u[field] = 8
				case "status":
					u[field] = 2
				case "used_quota":
					u[field] = -1
				default:
					delete(u, field)
				}
				okJSON(w, u)
			})
			v := signIn(f.client)
			if v.User != nil || v.State == "signed_in" || v.ErrorCode == "" || f.store.value != "" {
				t.Fatalf("invalid identity/balance accepted: %+v", v)
			}
		})
	}
	for value, expected := range map[string]string{"0": "0", "-42": "-0.0084"} {
		t.Run(value, func(t *testing.T) {
			f := setup(t)
			f.login(t)
			f.mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
				u := testUser(7)
				u["quota"] = json.Number(value)
				okJSON(w, u)
			})
			v := signIn(f.client)
			if v.User == nil || v.User.Credits != expected {
				t.Fatalf("valid quota lost: %+v", v)
			}
		})
	}
}

func TestCreditsFromBillingTokens(t *testing.T) {
	for tokens, want := range map[string]string{
		"0": "0", "1": "0.0002", "4999": "0.9998", "5000": "1", "5001": "1.0002",
		"500000": "100", "-1": "-0.0002", "-5000": "-1", "-5001": "-1.0002",
		"9007199254740993": "1801439850948.1986", "9223372036854775807": "1844674407370955.1614",
		"-9223372036854775808": "-1844674407370955.1616",
	} {
		t.Run(tokens, func(t *testing.T) {
			if got := creditsFromTokens(tokens); got != want {
				t.Fatalf("%s billing tokens = %s credits; want %s", tokens, got, want)
			}
		})
	}
}

func TestOfflineStaleVersusRevoked(t *testing.T) {
	f := setup(t)
	f.login(t)
	var status = 200
	f.mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		okJSON(w, testUser(7))
	})
	if v := signIn(f.client); v.State != "signed_in" {
		t.Fatal(v)
	}
	status = 503
	v := f.client.Status(context.Background())
	if !v.Stale || v.User == nil || v.ErrorCode != "unavailable" {
		t.Fatalf("offline hid stale data: %+v", v)
	}
	status = 401
	v = f.client.Status(context.Background())
	if v.User != nil || v.State != "signed_out" || f.store.value != "" {
		t.Fatalf("revoked identity survived: %+v", v)
	}
}

func TestRefreshIdentityMismatchClearsSavedSession(t *testing.T) {
	f := setup(t)
	f.login(t)
	f.self(t)
	signIn(f.client)
	f.client.accessExpiry = 0
	f.mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) { writeBundle(w, 8, "other-session", "other-refresh") })
	v := f.client.Status(context.Background())
	if v.State != "signed_out" || v.User != nil || f.store.value != "" {
		t.Fatalf("cross-account restore: %+v", v)
	}
}

func TestUnavailableVaultAllowsMemoryOnlyLogin(t *testing.T) {
	f := setup(t)
	f.login(t)
	f.self(t)
	f.store.loadErr = errors.New("vault locked")
	f.store.saveErr = f.store.loadErr
	v := signIn(f.client)
	if v.State != "signed_in" || v.Remembered || len(v.WarningCodes) == 0 || f.store.value != "" {
		t.Fatalf("memory fallback: %+v", v)
	}
}

func TestLogoutFailuresAreVisibleAndRetryable(t *testing.T) {
	f := setup(t)
	f.login(t)
	f.self(t)
	signIn(f.client)
	f.store.deleteErr = errors.New("locked")
	status := 503
	f.mux.HandleFunc("/api/user/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		okJSON(w, nil)
	})
	v := f.client.Logout(context.Background())
	if v.State != "signed_out" || v.User != nil || len(v.WarningCodes) != 2 {
		t.Fatalf("lost logout failures: %+v", v)
	}
	if v = signIn(f.client); v.ErrorCode != "logout_first" {
		t.Fatal("new login overwrote pending cleanup")
	}
	f.store.deleteErr = nil
	status = 200
	v = f.client.Logout(context.Background())
	if len(v.WarningCodes) != 0 || f.store.value != "" || v.HasSession {
		t.Fatalf("retry failed: %+v", v)
	}
}

func TestConcurrentRefreshCannotResurrectAfterLogout(t *testing.T) {
	f := setup(t)
	f.login(t)
	f.self(t)
	signIn(f.client)
	f.client.accessExpiry = 0
	started := make(chan struct{})
	release := make(chan struct{})
	f.mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		writeBundle(w, 7, "session-seven", "rotated")
	})
	f.mux.HandleFunc("/api/user/auth/logout", func(w http.ResponseWriter, r *http.Request) { okJSON(w, nil) })
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); f.client.Status(context.Background()) }()
	<-started
	go func() { defer wg.Done(); f.client.Logout(context.Background()) }()
	close(release)
	wg.Wait()
	if v := f.client.Status(context.Background()); v.State != "signed_out" || f.store.value != "" {
		t.Fatalf("resurrected: %+v", v)
	}
}

func TestRestoreNeverTrustsOfflineOrWrongOriginRecord(t *testing.T) {
	f := setup(t)
	f.login(t)
	f.self(t)
	signIn(f.client)
	f.mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	c := newClient(f.server.URL, f.store, http.DefaultTransport)
	v := c.Status(context.Background())
	if v.User != nil || v.State == "signed_in" || v.ErrorCode != "unavailable" {
		t.Fatalf("offline restore trusted: %+v", v)
	}
	c = newClient("https://different.invalid", f.store, http.DefaultTransport)
	v = c.Status(context.Background())
	if v.HasSession || f.store.value != "" {
		t.Fatal("wrong origin record restored")
	}
}

func TestExpiredChallengeAndBadPersistedSecret(t *testing.T) {
	f := setup(t)
	f.client.view.State = "verifying"
	f.client.flow, f.client.flowExpiry = "secret-flow", time.Now().Unix()-1
	v := f.client.Verify(context.Background(), "123456")
	if v.State != "signed_out" || v.ErrorCode != "session_expired" || f.client.flow != "" {
		t.Fatalf("expired challenge survived: %+v", v)
	}
	s := savedSession{Origin: f.server.URL, UserID: 7, SID: "session-seven", Cookie: "bad\r\nInjected: value", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	raw, _ := json.Marshal(s)
	f.store.value = string(raw)
	c := newClient(f.server.URL, f.store, http.DefaultTransport)
	v = c.Status(context.Background())
	if v.HasSession || v.ErrorCode != "session_expired" || f.store.value != "" {
		t.Fatalf("invalid vault secret restored: %+v", v)
	}
}

func TestLoginRequiresRefreshCookie(t *testing.T) {
	f := setup(t)
	f.mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"access_token": "secret-access", "token_type": "Bearer", "access_expires_at": time.Now().Add(time.Hour).Unix(), "user": testUser(7), "session": map[string]any{"sid": "session-seven", "current": true, "expires_at": time.Now().Add(time.Hour).Unix()}})
	})
	if v := signIn(f.client); v.State != "signed_out" || v.ErrorCode != "protocol" {
		t.Fatalf("missing refresh cookie accepted: %+v", v)
	}
}

func TestChallengePolicyNeverBypassed(t *testing.T) {
	for _, flag := range []string{"turnstile_check", "password_login_encryption_enabled", "password_login_enabled"} {
		t.Run(flag, func(t *testing.T) {
			mux := http.NewServeMux()
			s := httptest.NewServer(mux)
			defer s.Close()
			mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
				status := map[string]any{"password_login_enabled": true}
				status[flag] = flag != "password_login_enabled"
				okJSON(w, status)
			})
			mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
				t.Error("password sent despite unsupported login policy")
			})
			c := newClient(s.URL, &memoryStore{}, http.DefaultTransport)
			if v := signIn(c); v.ErrorCode != "unsupported_verification" {
				t.Fatalf("policy ignored: %+v", v)
			}
		})
	}
}

func TestRefreshRotationSavedWhenSelfTemporarilyFails(t *testing.T) {
	f := setup(t)
	f.login(t)
	f.mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	// An interrupted login stays unverified and does not persist a successful identity.
	v := signIn(f.client)
	if v.State == "signed_in" || v.User != nil || f.store.value != "" || !v.HasSession {
		t.Fatalf("unverified login exposed: %+v", v)
	}
	f.client.accessExpiry = 0
	f.mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) { writeBundle(w, 7, "session-seven", "rotated-refresh") })
	v = f.client.Status(context.Background())
	if v.User != nil || v.State == "signed_in" || !strings.Contains(f.store.value, "rotated-refresh") {
		t.Fatalf("rotated credential lost or unverified user exposed: %+v", v)
	}
}

func TestRecoverInterruptedLoginAndIgnoreStrayVerification(t *testing.T) {
	f := setup(t)
	f.login(t)
	broken := true
	f.mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		if broken {
			w.WriteHeader(503)
			return
		}
		okJSON(w, testUser(7))
	})
	if v := signIn(f.client); v.User != nil || f.store.value != "" {
		t.Fatal("interrupted login persisted identity")
	}
	broken = false
	v := f.client.Status(context.Background())
	if v.State != "signed_in" || !v.Remembered || f.store.value == "" {
		t.Fatalf("recovered login not saved: %+v", v)
	}
	v = f.client.Verify(context.Background(), "123456")
	if v.State != "signed_in" || v.User == nil || f.store.value == "" {
		t.Fatal("stray verify erased current session")
	}
}
