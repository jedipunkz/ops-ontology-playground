package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClientRejectsNonLoopbackHost(t *testing.T) {
	t.Parallel()

	_, err := NewClient("http://example.com/ops")
	if err == nil {
		t.Fatal("NewClient accepted a non-loopback host")
	}
}

func TestNewClientDefaultsDatasetPath(t *testing.T) {
	t.Parallel()

	client, err := NewClient("http://localhost:3030")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := client.baseURL.Path, "/ops"; got != want {
		t.Fatalf("base path = %q, want %q", got, want)
	}
}

func TestSearchApplicationUsesParameterizedSlugAndParsesBindings(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ops/query" {
			t.Errorf("request path = %q, want /ops/query", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/sparql-query" {
			t.Errorf("Content-Type = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if !strings.Contains(string(body), "<https://example.com/ops#checkout>") {
			t.Errorf("query does not contain the expected application IRI: %s", body)
		}
		if !strings.Contains(string(body), "ops:startedAt ?incidentStartedAt") {
			t.Errorf("query does not return incident start time needed to compare deployment timing: %s", body)
		}
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"head": map[string]any{"vars": []string{"incident"}},
			"results": map[string]any{
				"bindings": []map[string]any{
					{
						"incident": map[string]string{
							"type":  "uri",
							"value": "https://example.com/ops#incident-5377",
						},
						"incidentStartedAt": map[string]string{
							"type":  "literal",
							"value": "2026-11-18T10:23:00Z",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL + "/ops")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.SearchApplication(context.Background(), "checkout")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(result.Results.Bindings); got != 1 {
		t.Fatalf("binding count = %d, want 1", got)
	}
	if got := result.Results.Bindings[0]["incident"].Value; got != "https://example.com/ops#incident-5377" {
		t.Fatalf("incident IRI = %q", got)
	}
	if got := result.Results.Bindings[0]["incidentStartedAt"].Value; got != "2026-11-18T10:23:00Z" {
		t.Fatalf("incident start time = %q", got)
	}
}

func TestSearchApplicationRejectsSPARQLInput(t *testing.T) {
	t.Parallel()

	client, err := NewClient("http://localhost:3030/ops")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SearchApplication(context.Background(), "checkout> } DELETE WHERE { ?s ?p ?o } #")
	if err == nil {
		t.Fatal("SearchApplication accepted invalid slug input")
	}
}

func TestLoadTurtleTargetsDefaultGraphParameter(t *testing.T) {
	t.Parallel()

	const turtle = "@prefix ops: <https://example.com/ops#> ."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ops/data" {
			t.Errorf("request path = %q, want /ops/data", r.URL.Path)
		}
		if _, ok := r.URL.Query()["default"]; !ok {
			t.Errorf("query = %q, want default graph parameter", r.URL.RawQuery)
		}
		if got := r.Header.Get("Content-Type"); got != "text/turtle" {
			t.Errorf("Content-Type = %q, want text/turtle", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if got := string(body); got != turtle {
			t.Errorf("body = %q, want %q", got, turtle)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(server.URL + "/ops")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.LoadTurtle(context.Background(), []byte(turtle)); err != nil {
		t.Fatal(err)
	}
}

func TestQueryDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	destinationHit := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinationHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer origin.Close()

	client, err := NewClient(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Query(context.Background(), "ASK {}")
	if err == nil {
		t.Fatal("Query followed a redirect instead of returning an error")
	}
	if destinationHit {
		t.Fatal("Query sent a request to the redirect destination")
	}
}

func TestRecordHypothesisValidatesReferencesAndSHACLBeforePromoting(t *testing.T) {
	t.Parallel()

	var updates []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ops/query":
			if got := r.Header.Get("Content-Type"); got != "application/sparql-query" {
				t.Errorf("query Content-Type = %q", got)
			}
			w.Header().Set("Content-Type", "application/sparql-results+json")
			_, _ = w.Write([]byte(`{"head":{},"boolean":true}`))
		case r.URL.Path == "/ops/update":
			data, err := ioReadAll(r)
			if err != nil {
				t.Error(err)
			}
			updates = append(updates, string(data))
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/ops/shacl":
			if got := r.URL.Query().Get("graph"); !strings.HasPrefix(got, "urn:ops-ontology:staging:") {
				t.Errorf("SHACL graph = %q", got)
			}
			if got := r.Header.Get("Content-Type"); got != "text/turtle" {
				t.Errorf("SHACL Content-Type = %q", got)
			}
			_, _ = w.Write([]byte(`@prefix sh: <http://www.w3.org/ns/shacl#> . [] sh:conforms true .`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL + "/ops")
	if err != nil {
		t.Fatal(err)
	}
	id, err := client.RecordHypothesis(context.Background(), Hypothesis{
		IncidentID:   "incident-5377",
		DeploymentID: "checkout-v42",
		Summary:      `The checkout deploy caused "slow" responses.`,
		Evidence:     "Latency increased eight minutes after v42 was deployed.",
		Confidence:   0.75,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "hypothesis-") {
		t.Fatalf("hypothesis ID = %q", id)
	}
	if got := len(updates); got != 2 {
		t.Fatalf("update count = %d, want staging and promotion", got)
	}
	if !strings.Contains(updates[0], `The checkout deploy caused \"slow\" responses.`) {
		t.Fatalf("staging update did not escape literal quotes: %s", updates[0])
	}
	if !strings.Contains(updates[0], "GRAPH <urn:ops-ontology:staging:") {
		t.Fatalf("staging update did not use a named staging graph: %s", updates[0])
	}
	if !strings.Contains(updates[1], "DROP SILENT GRAPH <urn:ops-ontology:staging:") {
		t.Fatalf("promotion did not remove its staging graph: %s", updates[1])
	}
}

func TestRecordHypothesisDoesNotPromoteWhenSHACLRejects(t *testing.T) {
	t.Parallel()

	var updates int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ops/query":
			_, _ = w.Write([]byte(`{"boolean":true}`))
		case "/ops/update":
			updates++
			w.WriteHeader(http.StatusNoContent)
		case "/ops/shacl":
			_, _ = w.Write([]byte(`@prefix sh: <http://www.w3.org/ns/shacl#> . [] sh:conforms false .`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL + "/ops")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RecordHypothesis(context.Background(), Hypothesis{
		IncidentID:   "incident-5377",
		DeploymentID: "checkout-v42",
		Summary:      "A valid length summary.",
		Evidence:     "A valid length evidence.",
		Confidence:   0.5,
	})
	if err == nil || !strings.Contains(err.Error(), "SHACL") {
		t.Fatalf("RecordHypothesis error = %v, want SHACL rejection", err)
	}
	if updates != 2 {
		t.Fatalf("update count = %d, want staging plus cleanup and no promotion", updates)
	}
}

func TestRecordHypothesisRejectsInvalidConfidenceBeforeNetwork(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected network request: %s %s", r.Method, r.URL)
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RecordHypothesis(context.Background(), Hypothesis{
		IncidentID:   "incident-5377",
		DeploymentID: "checkout-v42",
		Summary:      "A valid length summary.",
		Evidence:     "A valid length evidence.",
		Confidence:   1.1,
	})
	if err == nil {
		t.Fatal("RecordHypothesis accepted confidence above 1")
	}
}

func TestSparqlLiteralRejectsUnsupportedControlCharacters(t *testing.T) {
	t.Parallel()

	_, err := sparqlLiteral("unsafe\x00value")
	if err == nil {
		t.Fatal("sparqlLiteral should reject control characters")
	}
}

func ioReadAll(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}

func TestListHypothesesRejectsSPARQLInput(t *testing.T) {
	t.Parallel()

	client, err := NewClient("http://localhost:3030/ops")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListHypotheses(context.Background(), "incident-1> } DELETE WHERE { ?s ?p ?o } #")
	if err == nil {
		t.Fatal("ListHypotheses accepted invalid slug input")
	}
}

func TestListHypothesesQueriesIncidentIRI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if !strings.Contains(string(body), "ops:aboutIncident <https://example.com/ops#incident-5377>") {
			t.Errorf("query does not filter by incident IRI: %s", body)
		}
		_, _ = w.Write([]byte(`{"results":{"bindings":[]}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL + "/ops")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListHypotheses(context.Background(), "incident-5377"); err != nil {
		t.Fatal(err)
	}
}
