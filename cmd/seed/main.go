// Command seed recreates the ops database with the schema and sample data.
// It deletes every hypothesis recorded so far.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jedipunkz/ops-ontology-playground/internal/config"
	"github.com/jedipunkz/ops-ontology-playground/internal/typedb"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	store, err := config.NewStore()
	if err != nil {
		fatal(err)
	}
	schema, err := os.ReadFile("ontology/schema.tql")
	if err != nil {
		fatal(err)
	}
	data, err := os.ReadFile("ontology/sample-ops.tql")
	if err != nil {
		fatal(err)
	}
	// TypeDB may still be starting right after `make up`.
	for {
		err = store.Reset(ctx, string(schema), string(data))
		if !errors.Is(err, typedb.ErrUnavailable) || ctx.Err() != nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		fatal(err)
	}
	fmt.Println("Recreated the ops database with the schema and sample data.")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
