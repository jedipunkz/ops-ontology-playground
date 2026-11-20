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
- A Go ADK agent reads incidents and deployments through a constrained SPARQL
  query tool.
- Ollama runs the default language model locally; Gemini is an optional provider.
- The agent can propose a root-cause hypothesis. Go validates the referenced
  incident and deployment, writes the candidate to a temporary named graph,
  validates it against SHACL, then promotes it to the default graph.
- `cmd/seed` loads synthetic operational data. It is safe to run repeatedly.

The default setup needs no API key or Google Cloud project. Ollama runs locally
and the client uses its OpenAI-compatible endpoint. Fuseki is bound to
`127.0.0.1` and has no authentication; do not expose this demo endpoint to an
untrusted network.

## Requirements

- Go 1.26.10 or later
- Docker Compose
- [Ollama](https://ollama.com/download)
- About 2.5 GB of disk space for the default `qwen3:4b` model

## Run

Start Ollama (for example, `ollama serve` in another terminal) and download a
local model:

```sh
ollama pull qwen3:4b
```

Start the local graph store:

```sh
docker compose up --build -d
```

Load the sample graph:

```sh
go run ./cmd/seed
```

Run the ADK agent:

```sh
go run ./cmd/agent console
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

### Optional: use Gemini instead of Ollama

```sh
MODEL_PROVIDER=gemini GOOGLE_API_KEY="your-gemini-api-key" \
  go run ./cmd/agent console
```

The Ollama model name and local endpoint can be overridden with `OLLAMA_MODEL`
and `OLLAMA_BASE_URL`. The endpoint must be a loopback HTTP URL ending in `/v1`.
The ADK Go OpenAI-compatible model connector is experimental.

## Verify

```sh
go test ./...
```

The tests use HTTP test servers and do not call a model, require Ollama, or
require Docker.

## Scope and limitations

- The data is synthetic and represents one small incident/deployment scenario.
- The write tool accepts structured fields, not arbitrary SPARQL from the
  model. It only records hypotheses after checking the referenced entities.
- SHACL validates the proposed hypothesis shape. It does not prove that the
  hypothesis is causally correct; evidence remains visible for human review.
- This is a local learning sample, not a production incident-management
  system or a QuipuDB compatibility implementation.

## References

- [Netflix: Ontology-Driven Observability](https://netflixtechblog.com/ontology-driven-observability-building-an-end-to-end-knowledge-graph-at-netflix-scale-421392cf5adf)
- [Apache Jena Fuseki Docker tools](https://jena.apache.org/documentation/fuseki2/fuseki-docker.html)
- [Apache Jena SHACL](https://jena.apache.org/documentation/shacl)
- [Google ADK for Go](https://github.com/google/adk-go)
- [ADK Go OpenAI-compatible model connector](https://adk.dev/agents/models/openai/)
- [Ollama OpenAI-compatible API](https://docs.ollama.com/api/openai-compatibility)
- [Ollama Qwen 3 models](https://ollama.com/library/qwen3)
