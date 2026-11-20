package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jedipunkz/ops-ontology-playground/internal/config"
	"github.com/jedipunkz/ops-ontology-playground/internal/opsmcp"
)

func main() {
	// stdout carries the MCP protocol; log writes to stderr.
	store, err := config.NewStore()
	if err != nil {
		log.Fatal(err)
	}
	if err := opsmcp.NewServer(store).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("run MCP server: %v", err)
	}
}
