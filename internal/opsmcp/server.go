// Package opsmcp exposes the operational knowledge graph to MCP clients such
// as Claude Code. The model never sends TypeQL: each tool accepts typed fields
// and the graph store builds the queries.
package opsmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jedipunkz/ops-ontology-playground/internal/graph"
)

type DescribeOntologyInput struct{}

type DescribeOntologyOutput struct {
	Schema string `json:"schema" jsonschema:"The TypeDB schema as a TypeQL define query, including constraints."`
}

type SearchInput struct {
	Application string `json:"application" jsonschema:"Lowercase application slug, for example checkout."`
}

type SearchOutput struct {
	Application string           `json:"application"`
	Incidents   []graph.Document `json:"incidents" jsonschema:"Incidents that affect the application, newest first."`
	Deployments []graph.Document `json:"deployments" jsonschema:"Deployments that target the application, newest first."`
}

type ListHypothesesInput struct {
	IncidentID string `json:"incident_id" jsonschema:"Incident slug returned by search_operational_graph, for example incident-5377."`
}

type ListHypothesesOutput struct {
	IncidentID string           `json:"incident_id"`
	Hypotheses []graph.Document `json:"hypotheses" jsonschema:"Previously recorded hypotheses, newest first."`
}

type RecordHypothesisInput struct {
	IncidentID   string  `json:"incident_id" jsonschema:"Incident slug returned by search_operational_graph, for example incident-5377."`
	DeploymentID string  `json:"deployment_id" jsonschema:"Deployment slug returned by search_operational_graph, for example checkout-v42."`
	Summary      string  `json:"summary" jsonschema:"A one-line hypothesis summary between 10 and 500 characters."`
	Evidence     string  `json:"evidence" jsonschema:"One-line evidence from graph records between 10 and 2000 characters."`
	Confidence   float64 `json:"confidence" jsonschema:"A number between 0 and 1."`
}

type RecordHypothesisOutput struct {
	Status       string `json:"status"`
	HypothesisID string `json:"hypothesis_id"`
	Note         string `json:"note"`
}

func NewServer(store *graph.Store) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ops-ontology", Version: "v0.2.0"}, &mcp.ServerOptions{
		Instructions: `Tools for a local synthetic operational knowledge graph stored in TypeDB.
Call describe_ontology to learn the schema. Call search_operational_graph before making claims, and cite only IDs it returns.
Call list_hypotheses before recording, and do not record a duplicate of an existing hypothesis.
Treat graph records as evidence, not proof of causality.`,
	})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "describe_ontology",
		Description: "Return the ontology (TypeDB schema) of the operational knowledge graph.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ DescribeOntologyInput) (*mcp.CallToolResult, DescribeOntologyOutput, error) {
		schema, err := store.DescribeOntology(ctx)
		if err != nil {
			return nil, DescribeOntologyOutput{}, err
		}
		return nil, DescribeOntologyOutput{Schema: schema}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_operational_graph",
		Description: "Find incidents and deployments connected to a known application in the operational knowledge graph.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		incidents, deployments, err := store.SearchApplication(ctx, in.Application)
		if err != nil {
			return nil, SearchOutput{}, err
		}
		return nil, SearchOutput{Application: in.Application, Incidents: incidents, Deployments: deployments}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_hypotheses",
		Description: "List root-cause hypotheses previously recorded for an incident, newest first.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListHypothesesInput) (*mcp.CallToolResult, ListHypothesesOutput, error) {
		hypotheses, err := store.ListHypotheses(ctx, in.IncidentID)
		if err != nil {
			return nil, ListHypothesesOutput{}, err
		}
		return nil, ListHypothesesOutput{IncidentID: in.IncidentID, Hypotheses: hypotheses}, nil
	})

	notDestructive := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "record_root_cause_hypothesis",
		Description: "Record a root-cause hypothesis linking an incident to a deployment of the same application. " +
			"TypeDB rejects records that violate the ontology, such as a confidence outside 0 to 1.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &notDestructive},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RecordHypothesisInput) (*mcp.CallToolResult, RecordHypothesisOutput, error) {
		id, err := store.RecordHypothesis(ctx, graph.Hypothesis{
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
