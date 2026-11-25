// Package auth signs the CLI in with the PTB website (OAuth 2.1 Authorization
// Code + PKCE S256 on a loopback redirect, RFC 8252) and keeps the tokens in
// the OS keychain. PTB_API_KEY (a ptb_… key from Integrasi → Keamanan) takes
// precedence for servers and CI.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/ealfarozi/ptb-cli/internal/config"
)

const (
	keyringService = "ptb-cli"
	Scope          = "ptb:market:read ptb:ticker:read ptb:analysis:read ptb:watchlist:read"
	ClientName     = "PTB CLI"
)

// ErrNotLoggedIn: no API key and no stored session.
var ErrNotLoggedIn = errors.New("belum login: jalankan `ptb login` atau set PTB_API_KEY")

// Session is what the keychain stores for a profile.
type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	ClientID     string    `json:"client_id"`
	Issuer       string    `json:"issuer"`
}

// ─── Storage ─────────────────────────────────────────────────────────────────

func fallbackPath(profile string) (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "credentials-"+profile+".json"), nil
}

// Save keeps the session in the keychain, or in a 0600 file when no keychain
// exists (headless Linux); Where tells which.
func Save(profile string, s *Session) (where string, err error) {
	b, _ := json.Marshal(s)
	if err = keyring.Set(keyringService, profile, string(b)); err == nil {
		return "keychain", nil
	}
	p, perr := fallbackPath(profile)
	if perr != nil {
		return "", perr
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, b, 0o600)
}

func Load(profile string) (*Session, error) {
	var raw string
	if v, err := keyring.Get(keyringService, profile); err == nil {
		raw = v
	} else {
		p, perr := fallbackPath(profile)
		if perr != nil {
			return nil, ErrNotLoggedIn
		}
		b, ferr := os.ReadFile(p)
		if ferr != nil {
			return nil, ErrNotLoggedIn
		}
		raw = string(b)
	}
	var s Session
	if err := json.Unmarshal([]byte(raw), &s); err != nil || s.AccessToken == "" {
		return nil, ErrNotLoggedIn
	}
	return &s, nil
}

func Delete(profile string) {
	_ = keyring.Delete(keyringService, profile)
	if p, err := fallbackPath(profile); err == nil {
		_ = os.Remove(p)
	}
}

// ─── Credentials for a request ───────────────────────────────────────────────

// Source says where the bearer came from.
type Source string

const (
	SourceAPIKey Source = "PTB_API_KEY"
	SourceOAuth  Source = "login browser"
)

// Bearer returns a valid bearer token, refreshing the session if needed.
func Bearer(ctx context.Context, profile string, prof *config.Profile) (string, Source, error) {
	if k := strings.TrimSpace(os.Getenv("PTB_API_KEY")); k != "" {
		return k, SourceAPIKey, nil
	}
	s, err := Load(profile)
	if err != nil {
		return "", "", err
	}
	if time.Until(s.Expiry) > time.Minute {
		return s.AccessToken, SourceOAuth, nil
	}
	if s.RefreshToken == "" {
		return "", "", errors.New("sesi kedaluwarsa: jalankan `ptb login` lagi")
	}
	issuer := s.Issuer
	if issuer == "" {
		issuer = prof.Issuer
	}
	tok, err := tokenRequest(ctx, issuer, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {s.RefreshToken}, "client_id": {s.ClientID},
	})
	if err != nil {
		return "", "", fmt.Errorf("sesi tidak bisa diperpanjang (%v): jalankan `ptb login` lagi", err)
	}
	s.AccessToken, s.Expiry = tok.AccessToken, time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second)
	if tok.RefreshToken != "" {
		s.RefreshToken = tok.RefreshToken // rotated on every refresh
	}
	if _, err := Save(profile, s); err != nil {
		return "", "", err
	}
	return s.AccessToken, SourceOAuth, nil
}

// ─── OAuth ───────────────────────────────────────────────────────────────────

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func tokenRequest(ctx context.Context, issuer string, form url.Values) (*tokenResponse, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, issuer+"/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var t tokenResponse
	if err := json.NewDecoder(res.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("respons token tidak valid (HTTP %d)", res.StatusCode)
	}
	if res.StatusCode != http.StatusOK || t.AccessToken == "" {
		if t.Error == "" {
			t.Error = res.Status
		}
		return nil, fmt.Errorf("%s %s", t.Error, t.Description)
	}
	return &t, nil
}

// Register creates the public OAuth client of the CLI (RFC 7591). Loopback
// redirects with any port are accepted by the PTB server (RFC 8252 §7.3).
func Register(ctx context.Context, issuer string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"client_name":                ClientName,
		"redirect_uris":              []string{"http://127.0.0.1/callback"},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      Scope,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, issuer+"/oauth/register", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		ClientID string `json:"client_id"`
		Error    string `json:"error_description"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if out.ClientID == "" {
		return "", fmt.Errorf("pendaftaran klien gagal (HTTP %d) %s", res.StatusCode, out.Error)
	}
	return out.ClientID, nil
}

func randomURLSafe(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Login runs the browser flow. open is called with the authorization URL
// (it should launch the browser; the URL is also printed if that fails).
func Login(ctx context.Context, issuer, clientID string, open func(string)) (*Session, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)
	state, verifier := randomURLSafe(24), randomURLSafe(48)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "scope": {Scope},
		"state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	authURL := issuer + "/oauth/authorize?" + q.Encode()

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		v := r.URL.Query()
		page := func(title, msg string) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>PTB CLI</title><body style="font-family:system-ui;background:#131519;color:#eee;display:grid;place-items:center;height:100vh;margin:0"><div style="text-align:center"><h1>%s</h1><p>%s</p></div>`, html.EscapeString(title), html.EscapeString(msg))
		}
		switch {
		case v.Get("state") != state:
			page("Login gagal", "State tidak cocok. Ulangi ptb login.")
			done <- result{err: errors.New("state tidak cocok (kemungkinan permintaan palsu)")}
		case v.Get("error") != "":
			page("Login dibatalkan", v.Get("error_description"))
			done <- result{err: fmt.Errorf("login ditolak: %s %s", v.Get("error"), v.Get("error_description"))}
		case v.Get("code") == "":
			page("Login gagal", "Kode otorisasi tidak ada.")
			done <- result{err: errors.New("kode otorisasi tidak ada")}
		default:
			page("✓ PTB CLI tersambung", "Kamu boleh menutup tab ini dan kembali ke terminal.")
			done <- result{code: v.Get("code")}
		}
	})
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	open(authURL)

	var r result
	select {
	case r = <-done:
	case <-ctx.Done():
		return nil, errors.New("login dibatalkan atau melewati batas waktu")
	}
	if r.err != nil {
		return nil, r.err
	}
	tok, err := tokenRequest(ctx, issuer, url.Values{
		"grant_type": {"authorization_code"}, "code": {r.code}, "redirect_uri": {redirect},
		"client_id": {clientID}, "code_verifier": {verifier},
	})
	if err != nil {
		return nil, err
	}
	return &Session{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, ClientID: clientID, Issuer: issuer,
		Expiry: time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)}, nil
}

// Revoke asks the server to revoke a refresh token (RFC 7009).
func Revoke(ctx context.Context, issuer, clientID, refresh string) error {
	form := url.Values{"token": {refresh}, "token_type_hint": {"refresh_token"}, "client_id": {clientID}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, issuer+"/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return nil
}
