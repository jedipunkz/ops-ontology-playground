// Package opsmcp exposes the operational knowledge graph to MCP clients such
// as Claude Code. The model never sends SPARQL: each tool accepts typed fields
// and the graph client builds and validates the queries.
package opsmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jedipunkz/ops-ontology-playground/internal/graph"
)

type SearchInput struct {
	Application string `json:"application" jsonschema:"Lowercase application slug, for example checkout."`
}

type SearchOutput struct {
	Application string              `json:"application"`
	Rows        []map[string]string `json:"rows" jsonschema:"Incident and deployment rows. IRIs use the https://example.com/ops# namespace."`
}

type DescribeOntologyInput struct{}

type DescribeOntologyOutput struct {
	Terms []map[string]string `json:"terms" jsonschema:"Classes and properties with optional label, domain and range."`
}

type ListHypothesesInput struct {
	IncidentID string `json:"incident_id" jsonschema:"Incident slug returned by search_operational_graph, for example incident-5377."`
}

type ListHypothesesOutput struct {
	IncidentID string              `json:"incident_id"`
	Hypotheses []map[string]string `json:"hypotheses" jsonschema:"Previously recorded hypotheses, newest first."`
}

type RecordHypothesisInput struct {
	IncidentID   string  `json:"incident_id" jsonschema:"Incident slug returned by search_operational_graph, for example incident-5377."`
	DeploymentID string  `json:"deployment_id" jsonschema:"Deployment slug returned by search_operational_graph, for example checkout-v42."`
	Summary      string  `json:"summary" jsonschema:"A concise hypothesis summary between 10 and 500 characters."`
	Evidence     string  `json:"evidence" jsonschema:"Evidence from graph records between 10 and 2000 characters."`
	Confidence   float64 `json:"confidence" jsonschema:"A number between 0 and 1."`
}

type RecordHypothesisOutput struct {
	Status       string `json:"status"`
	HypothesisID string `json:"hypothesis_id"`
	Note         string `json:"note"`
}

func NewServer(client *graph.Client) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ops-ontology", Version: "v0.1.0"}, &mcp.ServerOptions{
		Instructions: `Tools for a local synthetic operational knowledge graph.
Call describe_ontology to learn the schema. Call search_operational_graph before making claims, and cite only IDs it returns.
Call list_hypotheses before recording, and do not record a duplicate of an existing hypothesis.
Treat graph records as evidence, not proof of causality.`,
	})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "describe_ontology",
		Description: "List the classes and properties of the operational ontology stored in the graph.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ DescribeOntologyInput) (*mcp.CallToolResult, DescribeOntologyOutput, error) {
		result, err := client.DescribeOntology(ctx)
		if err != nil {
			return nil, DescribeOntologyOutput{}, err
		}
		return nil, DescribeOntologyOutput{Terms: flatten(result)}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_hypotheses",
		Description: "List root-cause hypotheses previously recorded for an incident, newest first.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListHypothesesInput) (*mcp.CallToolResult, ListHypothesesOutput, error) {
		result, err := client.ListHypotheses(ctx, in.IncidentID)
		if err != nil {
			return nil, ListHypothesesOutput{}, err
		}
		return nil, ListHypothesesOutput{IncidentID: in.IncidentID, Hypotheses: flatten(result)}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_operational_graph",
		Description: "Find incidents and deployments connected to a known application in the operational knowledge graph.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		result, err := client.SearchApplication(ctx, in.Application)
		if err != nil {
			return nil, SearchOutput{}, err
		}
		return nil, SearchOutput{Application: in.Application, Rows: flatten(result)}, nil
	})

	notDestructive := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "record_root_cause_hypothesis",
		Description: "Record a typed root-cause hypothesis. The server checks that the incident and deployment " +
			"exist and target the same application, then validates the record with SHACL before writing it.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &notDestructive},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RecordHypothesisInput) (*mcp.CallToolResult, RecordHypothesisOutput, error) {
		id, err := client.RecordHypothesis(ctx, graph.Hypothesis{
			IncidentID:   in.IncidentID,
			DeploymentID: in.DeploymentID,
			Summary:      in.Summary,
			Evidence:     in.Evidence,
			Confidence:   in.Confidence,
		})
		if err != nil {
			return nil, RecordHypothesisOutput{}, err
		}
		return nil, RecordHypothesisOutput{
			Status:       "recorded",
			HypothesisID: id,
			Note:         "This is a hypothesis, not a confirmed cause.",
		}, nil
	})

	return server
}

// flatten drops SPARQL binding metadata and keeps only values, which is what
// the model needs to cite IDs and timestamps.
func flatten(result graph.QueryResult) []map[string]string {
	rows := make([]map[string]string, 0, len(result.Results.Bindings))
	for _, binding := range result.Results.Bindings {
		row := make(map[string]string, len(binding))
		for name, value := range binding {
			row[name] = value.Value
		}
		rows = append(rows, row)
	}
	return rows
}
