package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestErrorsMapToExitCodes(t *testing.T) {
	cases := map[string]int{
		"INSUFFICIENT_TOKENS": ExitTokens, "BUDGET_EXCEEDED": ExitTokens, "MEMBERSHIP_INACTIVE": ExitEntitlement,
		"NO_ACCESS": ExitEntitlement, "MARKET_CLOSED": ExitMarket, "RATE_LIMITED": ExitRateLimit,
		"INVALID_ARGUMENT": ExitUsage, "TOOL_UNAVAILABLE": ExitNetwork,
	}
	for code, want := range cases {
		if got := ExitCode(&Error{Code: code}); got != want {
			t.Fatalf("%s → %d, want %d", code, got, want)
		}
	}
	if ExitCode(&Error{Status: 401}) != ExitAuth || ExitCode(errors.New("x")) != ExitGeneric {
		t.Fatal("fallbacks")
	}
}

func TestGetSendsBudgetAndKeyAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Idempotency-Key") != "k1" || r.URL.Query().Get("max_tokens") != "5" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Idempotent-Replayed", "true")
		_, _ = w.Write([]byte(`{"success":true,"data":{"symbol":"BBCA","billing":{"chargedTokens":3,"balanceAfter":97}}}`))
	}))
	defer srv.Close()
	res, err := New(srv.URL, "tok", 0).Get(context.Background(), "/live/orderflow/BBCA", nil, Options{MaxTokens: 5, IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	b := BillingOf(res.Data)
	if !res.Replayed || b == nil || b.ChargedTokens != 3 || *b.BalanceAfter != 97 {
		t.Fatalf("got %+v %+v", res, b)
	}
}

func TestServerErrorBecomesTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"BUDGET_EXCEEDED","message":"terlalu mahal"}}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL, "tok", 0).Get(context.Background(), "/x", nil, Options{})
	var e *Error
	if !errors.As(err, &e) || e.Code != "BUDGET_EXCEEDED" || ExitCode(err) != ExitTokens {
		t.Fatalf("got %v", err)
	}
}

func TestNetworkFailureRetriedOnceWithSameKey(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close() // drop the connection: a network failure
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "tok", 0).Get(context.Background(), "/x", nil, Options{IdempotencyKey: "same"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
