package main

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jedipunkz/ops-ontology-playground/internal/graph"
	"github.com/jedipunkz/ops-ontology-playground/internal/opsmcp"
)

func main() {
	// stdout carries the MCP protocol; log writes to stderr.
	baseURL := os.Getenv("FUSEKI_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:3030/ops"
	}
	client, err := graph.NewClient(baseURL)
	if err != nil {
		log.Fatal(err)
	}
	if err := opsmcp.NewServer(client).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("run MCP server: %v", err)
	}
}
