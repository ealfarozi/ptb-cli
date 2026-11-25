// Package api calls the PTB member API (/api/v2/mcp), the same REST API the
// PTB MCP server uses: identical access rules, history limit and token billing.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ealfarozi/ptb-cli/internal/version"
)

// Exit codes (stable, see `ptb help exit-codes`).
const (
	ExitOK          = 0
	ExitGeneric     = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitEntitlement = 4
	ExitTokens      = 5
	ExitMarket      = 6
	ExitRateLimit   = 7
	ExitNetwork     = 8
	ExitCancelled   = 10
)

// Error is a refusal from the server (or the network).
type Error struct {
	Status  int    `json:"status,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// ExitCode maps an error to the CLI exit code.
func ExitCode(err error) int {
	var e *Error
	if !errors.As(err, &e) {
		return ExitGeneric
	}
	switch e.Code {
	case "AUTH_REQUIRED", "UNAUTHORIZED":
		return ExitAuth
	case "NO_ACCESS", "MEMBERSHIP_INACTIVE", "ADMIN_ONLY", "FORBIDDEN", "SUSPENDED":
		return ExitEntitlement
	case "INSUFFICIENT_TOKENS", "BUDGET_EXCEEDED":
		return ExitTokens
	case "MARKET_CLOSED", "HISTORY_LIMIT", "NO_DATA":
		return ExitMarket
	case "RATE_LIMITED", "TOO_MANY_REQUESTS", "REQUEST_IN_PROGRESS":
		return ExitRateLimit
	case "NETWORK", "INTERNAL", "TOOL_UNAVAILABLE", "RETRY_SAFE":
		return ExitNetwork
	case "INVALID_ARGUMENT", "IDEMPOTENCY_MISMATCH":
		return ExitUsage
	}
	switch {
	case e.Status == http.StatusUnauthorized:
		return ExitAuth
	case e.Status == http.StatusForbidden:
		return ExitEntitlement
	case e.Status == http.StatusPaymentRequired:
		return ExitTokens
	case e.Status == http.StatusTooManyRequests:
		return ExitRateLimit
	case e.Status >= 500:
		return ExitNetwork
	case e.Status >= 400:
		return ExitUsage
	}
	return ExitGeneric
}

type Client struct {
	Base   string
	Bearer string
	HTTP   *http.Client
}

func New(base, bearer string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{Base: strings.TrimRight(base, "/"), Bearer: bearer, HTTP: &http.Client{Timeout: timeout}}
}

// Options of one call.
type Options struct {
	MaxTokens      int64  // sent as max_tokens (0 = none)
	IdempotencyKey string // sent as Idempotency-Key
}

// Response is the decoded answer.
type Response struct {
	Data     json.RawMessage
	Replayed bool // answer replayed from an earlier identical request (no new charge)
}

// NewIdempotencyKey returns a random key for one logical call.
func NewIdempotencyKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "ptbcli-" + hex.EncodeToString(b)
}

// Get calls path (relative to the base) with query q. Network failures of a
// call with an idempotency key are retried once with the same key, so a
// charge is never taken twice.
func (c *Client) Get(ctx context.Context, path string, q url.Values, o Options) (*Response, error) {
	if q == nil {
		q = url.Values{}
	}
	if o.MaxTokens > 0 {
		q.Set("max_tokens", strconv.FormatInt(o.MaxTokens, 10))
	}
	u := c.Base + path
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	attempts := 1
	if o.IdempotencyKey != "" {
		attempts = 2
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		res, err := c.do(ctx, u, o)
		if err == nil {
			return res, nil
		}
		lastErr = err
		var ne *Error
		if errors.As(err, &ne) && ne.Code != "NETWORK" {
			return nil, err
		}
		if ctx.Err() != nil {
			break
		}
		time.Sleep(time.Second)
	}
	return nil, lastErr
}

func (c *Client) do(ctx context.Context, u string, o Options) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, &Error{Code: "INVALID_ARGUMENT", Message: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+c.Bearer)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ptb-cli/"+version.Version)
	if o.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", o.IdempotencyKey)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		var nerr net.Error
		msg := "tidak bisa menghubungi server PTB: " + err.Error()
		if errors.As(err, &nerr) && nerr.Timeout() {
			msg = "server PTB tidak menjawab (timeout)"
		}
		return nil, &Error{Code: "NETWORK", Message: msg}
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, &Error{Code: "NETWORK", Message: err.Error()}
	}
	var env struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	_ = json.Unmarshal(body, &env)
	if res.StatusCode >= 400 || (env.Success != nil && !*env.Success) {
		e := &Error{Status: res.StatusCode}
		var obj struct{ Code, Message string }
		if len(env.Error) > 0 && json.Unmarshal(env.Error, &obj) == nil && obj.Message != "" {
			e.Code, e.Message = obj.Code, obj.Message
		} else {
			var s string
			if json.Unmarshal(env.Error, &s) == nil && s != "" {
				e.Message = s
			} else if env.Message != "" {
				e.Message = env.Message
			} else {
				e.Message = http.StatusText(res.StatusCode)
			}
		}
		if e.Code == "" {
			switch res.StatusCode {
			case http.StatusUnauthorized:
				e.Code, e.Message = "AUTH_REQUIRED", "sesi tidak valid atau kedaluwarsa: jalankan `ptb login` lagi ("+e.Message+")"
			case http.StatusTooManyRequests:
				e.Code = "RATE_LIMITED"
			}
		}
		return nil, e
	}
	data := env.Data
	if len(data) == 0 {
		data = body
	}
	return &Response{Data: data, Replayed: res.Header.Get("Idempotent-Replayed") == "true"}, nil
}

// Billing is the billing block of a paid answer.
type Billing struct {
	ChargedTokens int64  `json:"chargedTokens"`
	BalanceAfter  *int64 `json:"balanceAfter"`
	Free          bool   `json:"free"`
	Rule          string `json:"rule"`
	Refunded      int64  `json:"refunded"`
}

// BillingOf extracts data.billing when present.
func BillingOf(data json.RawMessage) *Billing {
	var v struct {
		Billing *Billing `json:"billing"`
	}
	if json.Unmarshal(data, &v) != nil {
		return nil
	}
	return v.Billing
}

// Quote is GET /quote.
type Quote struct {
	Tool            string `json:"tool"`
	Symbol          string `json:"symbol"`
	Date            string `json:"date"`
	EstimatedTokens int64  `json:"estimatedTokens"`
	Exact           bool   `json:"exact"`
	Free            bool   `json:"free"`
	Trades          int64  `json:"trades"`
	Unpaid          int64  `json:"unpaid"`
	Balance         *int64 `json:"balance"`
	Rule            string `json:"rule"`
}

func (c *Client) Quote(ctx context.Context, tool, symbol, date string) (*Quote, error) {
	q := url.Values{"tool": {tool}, "symbol": {symbol}}
	if date != "" {
		q.Set("date", date)
	}
	res, err := c.Get(ctx, "/quote", q, Options{})
	if err != nil {
		return nil, err
	}
	var out Quote
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return nil, fmt.Errorf("quote tidak valid: %w", err)
	}
	return &out, nil
}
