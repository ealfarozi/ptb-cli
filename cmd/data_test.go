package cmd

import (
	"net/url"
	"strings"
	"testing"
)

// Every data command must map to an MCP tool and build the same path as the
// PTB MCP server (mcp/src/client.ts).
func TestSpecPaths(t *testing.T) {
	want := map[string]string{
		"ptb_get_orderflow":         "/live/orderflow/BBCA",
		"ptb_get_heatmap":           "/live/heatmap/BBCA",
		"get_broker_summary":        "/broker-inventory/summary",
		"get_broker_stalker":        "/broker-stalker/data",
		"ptb_get_stock_profile":     "/stocks/BBCA",
		"ptb_get_corporate_actions": "/corporate-action/symbol/BBCA",
		"ptb_get_index_flow":        "/indices/BBCA/flow",
		"ptb_get_scripless":         "/scripless/ticker/BBCA",
	}
	seen := map[string]bool{}
	for _, s := range specs {
		if s.Tool == "" || seen[s.Tool] {
			t.Fatalf("spec %q: missing or duplicate tool %q", s.Use, s.Tool)
		}
		seen[s.Tool] = true
		if strings.Contains(s.Tool, "broker_flow") {
			t.Fatal("Broker Flow was removed from MCP and must not be in the CLI")
		}
		if p, ok := want[s.Tool]; ok {
			args := []string{}
			if s.NArgs+s.OptArgs > 0 {
				args = []string{"bbca"}
			}
			if got := s.Path(args, url.Values{}); got != p {
				t.Fatalf("%s: path %s, want %s", s.Tool, got, p)
			}
		}
		if s.Paid != "" && s.NArgs != 1 {
			t.Fatalf("paid tool %s must take a symbol", s.Tool)
		}
	}
	if len(seen) != 30 {
		t.Fatalf("%d data commands, want 30 (every member MCP tool except the 3 account ones)", len(seen))
	}
}

func TestStockFullAndBrokerQuery(t *testing.T) {
	for _, s := range specs {
		switch s.Tool {
		case "ptb_get_stock_profile":
			q := url.Values{"full": {"true"}}
			if p := s.Path([]string{"adro"}, q); p != "/stocks/ADRO/full" || q.Get("full") != "" {
				t.Fatalf("full: %s %v", p, q)
			}
		case "get_broker_stalker":
			q := url.Values{}
			s.Path([]string{"yp"}, q)
			if q.Get("brokers") != "YP" {
				t.Fatalf("stalker query %v", q)
			}
		}
	}
}
