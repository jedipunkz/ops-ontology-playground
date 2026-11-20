# Ops Ontology Playground

A local playground for the ontology-driven observability pattern described by
[Netflix TechBlog](https://netflixtechblog.com/ontology-driven-observability-building-an-end-to-end-knowledge-graph-at-netflix-scale-421392cf5adf).

This project does not run Netflix's QuipuDB, which is not available outside
Netflix. Instead, it stores a small operational ontology in
[TypeDB](https://typedb.com/) and lets an AI agent (Claude Code) read and
write it through an MCP server.

## What runs

- TypeDB CE 3.14.2 stores the knowledge graph. `ontology/schema.tql` defines
  the ontology: applications, deployments, incidents, and root-cause
  hypotheses, with constraints such as `@key`, `@card`, `@regex`, `@values`
  and `@range`. TypeDB rejects writes that violate them.
- `cmd/ops-mcp` is an MCP server (stdio) for Claude Code or any MCP client.
  The agent never sends TypeQL; each tool takes typed fields and the server
  builds fixed queries.

| Tool | Kind | Description |
|---|---|---|
| `describe_ontology` | read | Returns the TypeDB schema, so the agent learns the ontology from the database |
| `search_operational_graph` | read | Returns incidents and deployments for an application |
| `list_hypotheses` | read | Returns hypotheses earlier sessions recorded for an incident |
| `record_root_cause_hypothesis` | write | Inserts a hypothesis in one transaction; the incident and deployment must share an application |

TypeDB's HTTP API is published only on `127.0.0.1:8000`. The container runs
as an unprivileged user, and TypeDB's default usage and error reporting is
turned off in `compose.yaml`. The MCP server only accepts a loopback TypeDB
URL.

## Requirements

- Go 1.26.10 or later
- Docker Compose
- [Claude Code](https://code.claude.com/docs/en/mcp) and `jq` for the demo

## Run

Set the TypeDB credentials. TypeDB CE starts with the vendor default
`admin` / `password`; use it only for this local sample data.

```sh
export TYPEDB_USERNAME=admin TYPEDB_PASSWORD=password
```

Start TypeDB and load the ontology and sample data. `make seed` recreates
the `ops` database, which deletes every recorded hypothesis.

```sh
make up
make seed
```

Run the demo. It calls `claude -p` three times (this uses your Claude Code
quota) and checks the database after each run:

1. Only the read tools are allowed. The agent's write is denied, and no
   hypothesis exists.
2. All tools are allowed. The agent records one hypothesis.
3. All tools are allowed. The agent reads the hypothesis from run 2 and does
   not record a duplicate.

```sh
make demo
```

The demo passes `--permission-mode dontAsk`, so any tool call that is not
allowlisted is denied instead of prompting.

To use the server interactively, start `claude` in this directory and enable
the `ops-ontology` server from `.mcp.json`.

Stop TypeDB without deleting its data:

```sh
make down
```

## Verify

```sh
go test ./...
```

The tests use HTTP test servers and in-memory MCP transports. They do not
require Docker or a model.

## Scope and limitations

- The data is synthetic and represents one small incident/deployment scenario.
- The schema validates the shape of a hypothesis. It does not prove that the
  hypothesis is causally correct; evidence remains visible for human review.
- This is a local learning sample, not a production incident-management
  system or a QuipuDB compatibility implementation.

## References

- [Netflix: Ontology-Driven Observability](https://netflixtechblog.com/ontology-driven-observability-building-an-end-to-end-knowledge-graph-at-netflix-scale-421392cf5adf)
- [TypeDB HTTP API](https://typedb.com/docs/reference/typedb-http-api/)
- [TypeQL annotations](https://typedb.com/docs/typeql-reference/annotations/)
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [Claude Code MCP](https://code.claude.com/docs/en/mcp)
- [Claude Code headless mode](https://code.claude.com/docs/en/headless)
