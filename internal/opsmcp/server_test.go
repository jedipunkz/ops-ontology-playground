package opsmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jedipunkz/ops-ontology-playground/internal/graph"
)

func connect(t *testing.T, fusekiURL string) *mcp.ClientSession {
	t.Helper()
	client, err := graph.NewClient(fusekiURL)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := NewServer(client).Connect(ctx, serverTransport, nil)
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

func TestSearchToolFlattensBindings(t *testing.T) {
	t.Parallel()

	fuseki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":{"bindings":[{"incident":{"type":"uri","value":"https://example.com/ops#incident-5377"}}]}}`))
	}))
	defer fuseki.Close()

	result, err := connect(t, fuseki.URL+"/ops").CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_operational_graph",
		Arguments: map[string]any{"application": "checkout"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %+v", result.Content)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out SearchOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 1 || out.Rows[0]["incident"] != "https://example.com/ops#incident-5377" {
		t.Fatalf("rows = %+v", out.Rows)
	}
}

func TestRecordToolReportsRejectionAsToolError(t *testing.T) {
	t.Parallel()

	fuseki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ops/query" {
			t.Errorf("unexpected request after failed reference check: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"boolean":false}`))
	}))
	defer fuseki.Close()

	result, err := connect(t, fuseki.URL+"/ops").CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_root_cause_hypothesis",
		Arguments: map[string]any{
			"incident_id":   "incident-5351",
			"deployment_id": "checkout-v42",
			"summary":       "A valid length summary.",
			"evidence":      "A valid length evidence.",
			"confidence":    0.5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("tool accepted references to different applications")
	}
}
