// Package config builds the graph store from environment variables.
package config

import (
	"os"

	"github.com/jedipunkz/ops-ontology-playground/internal/graph"
	"github.com/jedipunkz/ops-ontology-playground/internal/typedb"
)

const (
	defaultURL      = "http://127.0.0.1:8000"
	defaultDatabase = "ops"
)

// NewStore reads TYPEDB_URL, TYPEDB_DATABASE, TYPEDB_USERNAME and
// TYPEDB_PASSWORD. Credentials have no default.
func NewStore() (*graph.Store, error) {
	client, err := typedb.NewClient(
		getenv("TYPEDB_URL", defaultURL),
		os.Getenv("TYPEDB_USERNAME"),
		os.Getenv("TYPEDB_PASSWORD"),
	)
	if err != nil {
		return nil, err
	}
	return graph.NewStore(client, getenv("TYPEDB_DATABASE", defaultDatabase))
}

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
