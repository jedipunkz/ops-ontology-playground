# Ops Ontology Playground

A local playground for the ontology-driven observability pattern described by
[Netflix TechBlog](https://netflixtechblog.com/ontology-driven-observability-building-an-end-to-end-knowledge-graph-at-netflix-scale-421392cf5adf).

This project does not run Netflix's QuipuDB. QuipuDB is described as a
homegrown Netflix database and its implementation is not available here.
Instead, this playground uses Apache Jena Fuseki as a local RDF/SPARQL store,
an operational ontology in RDF/OWL, and SHACL validation before an agent writes
a finding.

## What runs

- Apache Jena Fuseki 6.2.0 stores the local knowledge graph.
- `cmd/ops-mcp` is an MCP server (stdio) that exposes the graph to an MCP
  client such as Claude Code. The client's model is the agent; no local model
  or GPU is needed.
- `search_operational_graph` reads incidents and deployments for an
  application through a fixed SPARQL query.
- `record_root_cause_hypothesis` accepts structured fields, not SPARQL. Go
  checks that the incident and deployment exist and target the same
  application, writes the candidate to a temporary named graph, validates it
  against SHACL, then promotes it to the default graph.
- `cmd/seed` loads synthetic operational data. It is safe to run repeatedly.

Fuseki is bound to `127.0.0.1` and has no authentication; do not expose this
demo endpoint to an untrusted network. The MCP server only accepts a loopback
Fuseki URL.

## Requirements

- Go 1.26.10 or later
- Docker Compose
- An MCP client. The steps below use [Claude Code](https://code.claude.com/docs/en/mcp).

## Run

Start the local graph store:

```sh
docker compose up --build -d
```

Load the sample graph:

```sh
go run ./cmd/seed
```

Start Claude Code in this directory. It reads `.mcp.json` and asks whether to
enable the project's `ops-ontology` server:

```sh
claude
```

Try asking:

> Why did checkout have a SEV-2 incident, and which deployment is a plausible
> cause? Record a hypothesis with the evidence from the graph.

Inspect the graph and issue SPARQL queries at <http://localhost:3030/>.
The dataset endpoint is `ops`; for example, its query endpoint is
<http://localhost:3030/ops/query>.

Stop Fuseki without deleting its persisted graph:

```sh
docker compose down
```

Set `FUSEKI_URL` in `.mcp.json` to point the server at another loopback
Fuseki dataset.

## Verify

```sh
go test ./...
```

The tests use HTTP test servers and in-memory MCP transports. They do not
require Docker or a model.

## Scope and limitations

- The data is synthetic and represents one small incident/deployment scenario.
- SHACL validates the proposed hypothesis shape. It does not prove that the
  hypothesis is causally correct; evidence remains visible for human review.
- This is a local learning sample, not a production incident-management
  system or a QuipuDB compatibility implementation.

## References

- [Netflix: Ontology-Driven Observability](https://netflixtechblog.com/ontology-driven-observability-building-an-end-to-end-knowledge-graph-at-netflix-scale-421392cf5adf)
- [Apache Jena Fuseki Docker tools](https://jena.apache.org/documentation/fuseki2/fuseki-docker.html)
- [Apache Jena SHACL](https://jena.apache.org/documentation/shacl)
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [Claude Code MCP](https://code.claude.com/docs/en/mcp)
