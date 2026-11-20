package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jedipunkz/ops-ontology-playground/internal/graph"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	baseURL := os.Getenv("FUSEKI_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:3030/ops"
	}
	client, err := graph.NewClient(baseURL)
	if err != nil {
		fatal(err)
	}
	for _, path := range []string{"ontology/observability.ttl", "ontology/sample-ops.ttl"} {
		data, err := os.ReadFile(path)
		if err != nil {
			fatal(fmt.Errorf("read %s: %w", path, err))
		}
		if err := client.LoadTurtle(ctx, data); err != nil {
			fatal(fmt.Errorf("load %s: %w", path, err))
		}
	}
	fmt.Println("Loaded operational ontology and sample data.")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
