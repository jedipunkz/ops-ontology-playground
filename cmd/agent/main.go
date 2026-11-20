package main

import (
	"context"
	"log"
	"os"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"github.com/jedipunkz/ops-ontology-playground/internal/graph"
	"github.com/jedipunkz/ops-ontology-playground/internal/modelconfig"
)

type SearchArgs struct {
	Application string `json:"application" jsonschema:"Lowercase application slug, for example checkout."`
}

type RecordHypothesisArgs struct {
	IncidentID   string  `json:"incident_id" jsonschema:"Incident slug returned by the graph search, for example incident-5377."`
	DeploymentID string  `json:"deployment_id" jsonschema:"Deployment slug returned by the graph search, for example checkout-v42."`
	Summary      string  `json:"summary" jsonschema:"A concise hypothesis summary between 10 and 500 characters."`
	Evidence     string  `json:"evidence" jsonschema:"Evidence from graph records between 10 and 2000 characters."`
	Confidence   float64 `json:"confidence" jsonschema:"A number between 0 and 1."`
}

func main() {
	baseURL := os.Getenv("FUSEKI_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:3030/ops"
	}
	graphClient, err := graph.NewClient(baseURL)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	model, err := modelconfig.New(ctx, os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	searchTool, err := functiontool.New(
		functiontool.Config{
			Name:        "search_operational_graph",
			Description: "Find incidents and deployments connected to a known application in the operational knowledge graph.",
		},
		func(ctx agent.Context, args SearchArgs) (map[string]any, error) {
			result, err := graphClient.SearchApplication(ctx, args.Application)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"application": args.Application,
				"bindings":    result.Results.Bindings,
			}, nil
		},
	)
	if err != nil {
		log.Fatalf("create graph search tool: %v", err)
	}

	recordTool, err := functiontool.New(
		functiontool.Config{
			Name:        "record_root_cause_hypothesis",
			Description: "Record a typed root-cause hypothesis only after checking the incident and deployment references and passing SHACL validation.",
		},
		func(ctx agent.Context, args RecordHypothesisArgs) (map[string]any, error) {
			id, err := graphClient.RecordHypothesis(ctx, graph.Hypothesis{
				IncidentID:   args.IncidentID,
				DeploymentID: args.DeploymentID,
				Summary:      args.Summary,
				Evidence:     args.Evidence,
				Confidence:   args.Confidence,
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"status":        "recorded",
				"hypothesis_id": id,
				"note":          "This is a hypothesis, not a confirmed cause.",
			}, nil
		},
	)
	if err != nil {
		log.Fatalf("create hypothesis tool: %v", err)
	}

	root, err := llmagent.New(llmagent.Config{
		Name:        "ops_ontology_agent",
		Model:       model,
		Description: "Investigates synthetic operational incidents using a typed knowledge graph and records evidence-backed hypotheses.",
		Instruction: `You are an SRE investigation assistant for a local synthetic dataset.
Use search_operational_graph before making claims. Only cite incident and deployment IDs returned by that tool.
You may record a hypothesis with record_root_cause_hypothesis only when the user asks you to record it.
Treat the graph as evidence, not proof of causality. State uncertainty and include the evidence behind every hypothesis.
Never invent graph facts or claim that recording a hypothesis confirms the root cause.`,
		Tools: []tool.Tool{searchTool, recordTool},
	})
	if err != nil {
		log.Fatalf("create ADK agent: %v", err)
	}

	config := &launcher.Config{AgentLoader: agent.NewSingleLoader(root)}
	if err := full.NewLauncher().Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("run agent: %v", err)
	}
}
