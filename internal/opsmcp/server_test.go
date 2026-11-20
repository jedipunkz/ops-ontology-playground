package opsmcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jedipunkz/ops-ontology-playground/internal/graph"
	"github.com/jedipunkz/ops-ontology-playground/internal/typedb"
)

// fakeTypeDB answers sign-in and passes /v1/query requests to handleQuery.
func fakeTypeDB(t *testing.T, handleQuery func(query string) string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/signin":
			_, _ = w.Write([]byte(`{"token":"test-token"}`))
		case "/v1/query":
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("Authorization = %q", got)
			}
			var request struct {
				Query string `json:"query"`
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &request); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(handleQuery(request.Query)))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func connect(t *testing.T, typedbURL string) *mcp.ClientSession {
	t.Helper()
	client, err := typedb.NewClient(typedbURL, "admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	store, err := graph.NewStore(client, "ops")
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := NewServer(store).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func recordArgs(confidence float64) map[string]any {
	return map[string]any{
		"incident_id":   "incident-5351",
		"deployment_id": "checkout-v42",
		"summary":       `A "quoted" summary \ with a backslash.`,
		"evidence":      "A valid length evidence.",
		"confidence":    confidence,
	}
}

func TestRecordToolRejectsWhenMatchFindsNoSharedApplication(t *testing.T) {
	t.Parallel()

	var queries []string
	server := fakeTypeDB(t, func(query string) string {
		queries = append(queries, query)
		return `{"queryType":"write","answerType":"conceptRows","answers":[]}`
	})

	result, err := connect(t, server.URL).CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "record_root_cause_hypothesis",
		Arguments: recordArgs(0.5),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("tool reported success although the match found nothing")
	}
	if len(queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(queries))
	}
	if !strings.Contains(queries[0], `has summary "A \"quoted\" summary \\ with a backslash."`) {
		t.Fatalf("summary literal was not escaped: %s", queries[0])
	}
	if !strings.Contains(queries[0], "has confidence 0.5") {
		t.Fatalf("confidence literal missing: %s", queries[0])
	}
}

func TestRecordToolWritesWholeNumberConfidenceAsDouble(t *testing.T) {
	t.Parallel()

	var query string
	server := fakeTypeDB(t, func(q string) string {
		query = q
		return `{"queryType":"write","answerType":"conceptRows","answers":[{}]}`
	})

	result, err := connect(t, server.URL).CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "record_root_cause_hypothesis",
		Arguments: recordArgs(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %+v", result.Content)
	}
	if !strings.Contains(query, "has confidence 1.0;") && !strings.Contains(query, "has confidence 1.0,") {
		t.Fatalf("confidence was not written as a double literal: %s", query)
	}
}

func TestToolsRejectNonSlugInputBeforeNetwork(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()
	session := connect(t, server.URL)

	for _, call := range []*mcp.CallToolParams{
		{Name: "search_operational_graph", Arguments: map[string]any{"application": `checkout"; delete $x;`}},
		{Name: "list_hypotheses", Arguments: map[string]any{"incident_id": "Incident 5377"}},
	} {
		result, err := session.CallTool(context.Background(), call)
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatalf("%s accepted a non-slug argument", call.Name)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("sent %d requests to TypeDB for invalid input", got)
	}
}

func TestToolsAdvertiseReadOnlyHints(t *testing.T) {
	t.Parallel()

	result, err := connect(t, "http://127.0.0.1:1").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"describe_ontology":            true,
		"list_hypotheses":              true,
		"search_operational_graph":     true,
		"record_root_cause_hypothesis": false,
	}
	if len(result.Tools) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(result.Tools), len(want))
	}
	for _, tool := range result.Tools {
		readOnly, ok := want[tool.Name]
		if !ok {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != readOnly {
			t.Fatalf("%s readOnlyHint = %+v, want %v", tool.Name, tool.Annotations, readOnly)
		}
	}
}
