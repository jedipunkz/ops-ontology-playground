package graph

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	opsNamespace = "https://example.com/ops#"
	maxBodyBytes = 1 << 20
)

var (
	slugPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	conformsPattern = regexp.MustCompile(`(?i)(?:sh:conforms|<http://www\.w3\.org/ns/shacl#conforms>)\s+"?(true|false)"?(?:\^\^[^;\s]+)?`)

	//go:embed hypothesis-shapes.ttl
	shapeDocument []byte
)

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

type Binding struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Datatype string `json:"datatype,omitempty"`
}

type QueryResult struct {
	Boolean *bool `json:"boolean,omitempty"`
	Results struct {
		Bindings []map[string]Binding `json:"bindings"`
	} `json:"results,omitempty"`
}

type Hypothesis struct {
	IncidentID   string
	DeploymentID string
	Summary      string
	Evidence     string
	Confidence   float64
}

func NewClient(rawURL string) (*Client, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse Fuseki URL: %w", err)
	}
	if parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Fuseki URL must be a plain local HTTP URL")
	}
	if !isLoopbackHost(parsed.Hostname()) {
		return nil, errors.New("Fuseki URL must use localhost or a loopback IP")
	}
	if parsed.Port() == "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), "80")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if parsed.Path == "" {
		parsed.Path = "/ops"
	}
	return &Client{
		baseURL: parsed,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) Query(ctx context.Context, sparql string) (QueryResult, error) {
	var result QueryResult
	body, err := c.request(ctx, http.MethodPost, "/query", "application/sparql-query", "application/sparql-results+json", []byte(sparql))
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("decode SPARQL result: %w", err)
	}
	return result, nil
}

func (c *Client) Ask(ctx context.Context, sparql string) (bool, error) {
	result, err := c.Query(ctx, sparql)
	if err != nil {
		return false, err
	}
	if result.Boolean == nil {
		return false, errors.New("SPARQL endpoint returned no boolean result")
	}
	return *result.Boolean, nil
}

func (c *Client) Update(ctx context.Context, sparql string) error {
	_, err := c.request(ctx, http.MethodPost, "/update", "application/sparql-update", "", []byte(sparql))
	return err
}

func (c *Client) LoadTurtle(ctx context.Context, turtle []byte) error {
	_, err := c.request(ctx, http.MethodPost, "/data?default", "text/turtle", "", turtle)
	return err
}

func (c *Client) SearchApplication(ctx context.Context, application string) (QueryResult, error) {
	if !slugPattern.MatchString(application) {
		return QueryResult{}, errors.New("application must be a lowercase slug")
	}
	appIRI := iri(opsNamespace + application)
	query := fmt.Sprintf(`
PREFIX ops: <%s>
SELECT ?incident ?severity ?incidentSummary ?incidentStartedAt ?deployment ?version ?deployedAt
WHERE {
  %s a ops:Application .
  OPTIONAL {
    ?incident a ops:Incident ;
              ops:affects %s ;
              ops:severity ?severity ;
              ops:summary ?incidentSummary ;
              ops:startedAt ?incidentStartedAt .
  }
  OPTIONAL {
    ?deployment a ops:Deployment ;
                ops:target %s ;
                ops:version ?version ;
                ops:deployedAt ?deployedAt .
  }
}
ORDER BY DESC(?deployedAt)
LIMIT 100
`, opsNamespace, appIRI, appIRI, appIRI)
	return c.Query(ctx, query)
}

// DescribeOntology returns the classes and properties of the ops namespace so
// an agent can learn the schema from the graph instead of from its prompt.
func (c *Client) DescribeOntology(ctx context.Context) (QueryResult, error) {
	return c.Query(ctx, fmt.Sprintf(`
PREFIX ops:  <%s>
PREFIX owl:  <http://www.w3.org/2002/07/owl#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?term ?kind ?label ?domain ?range
WHERE {
  VALUES ?kind { owl:Class owl:ObjectProperty owl:DatatypeProperty }
  ?term a ?kind .
  FILTER(STRSTARTS(STR(?term), STR(ops:)))
  OPTIONAL { ?term rdfs:label ?label }
  OPTIONAL { ?term rdfs:domain ?domain }
  OPTIONAL { ?term rdfs:range ?range }
}
ORDER BY ?kind ?term
LIMIT 200
`, opsNamespace))
}

// ListHypotheses returns hypotheses previously recorded for an incident.
func (c *Client) ListHypotheses(ctx context.Context, incidentID string) (QueryResult, error) {
	if !slugPattern.MatchString(incidentID) {
		return QueryResult{}, errors.New("incident ID must be a lowercase slug")
	}
	return c.Query(ctx, fmt.Sprintf(`
PREFIX ops: <%s>
SELECT ?hypothesis ?deployment ?summary ?evidence ?confidence ?recordedAt
WHERE {
  ?hypothesis a ops:Hypothesis ;
              ops:aboutIncident %s ;
              ops:relatedDeployment ?deployment ;
              ops:summary ?summary ;
              ops:evidence ?evidence ;
              ops:confidence ?confidence ;
              ops:recordedAt ?recordedAt .
}
ORDER BY DESC(?recordedAt)
LIMIT 50
`, opsNamespace, iri(opsNamespace+incidentID)))
}

func (c *Client) RecordHypothesis(ctx context.Context, hypothesis Hypothesis) (id string, retErr error) {
	if !slugPattern.MatchString(hypothesis.IncidentID) {
		return "", errors.New("incident ID must be a lowercase slug")
	}
	if !slugPattern.MatchString(hypothesis.DeploymentID) {
		return "", errors.New("deployment ID must be a lowercase slug")
	}
	if math.IsNaN(hypothesis.Confidence) || math.IsInf(hypothesis.Confidence, 0) ||
		hypothesis.Confidence < 0 || hypothesis.Confidence > 1 {
		return "", errors.New("confidence must be between 0 and 1")
	}
	if err := validateText("summary", hypothesis.Summary, 10, 500); err != nil {
		return "", err
	}
	if err := validateText("evidence", hypothesis.Evidence, 10, 2000); err != nil {
		return "", err
	}

	incidentIRI := iri(opsNamespace + hypothesis.IncidentID)
	deploymentIRI := iri(opsNamespace + hypothesis.DeploymentID)
	referencesExist, err := c.Ask(ctx, fmt.Sprintf(`
PREFIX ops: <%s>
ASK {
  %s a ops:Incident ; ops:affects ?application .
  %s a ops:Deployment ; ops:target ?application .
}
`, opsNamespace, incidentIRI, deploymentIRI))
	if err != nil {
		return "", fmt.Errorf("check hypothesis references: %w", err)
	}
	if !referencesExist {
		return "", errors.New("incident and deployment must exist and refer to the same application")
	}

	id, err = newUUID()
	if err != nil {
		return "", fmt.Errorf("create hypothesis ID: %w", err)
	}
	hypothesisIRI := iri(opsNamespace + "hypothesis-" + id)
	stagingIRI := "urn:ops-ontology:staging:" + id
	stagingRef := iri(stagingIRI)
	confidence := strconv.FormatFloat(hypothesis.Confidence, 'f', 6, 64)
	summary, err := sparqlLiteral(hypothesis.Summary)
	if err != nil {
		return "", err
	}
	evidence, err := sparqlLiteral(hypothesis.Evidence)
	if err != nil {
		return "", err
	}
	recordedAt, err := sparqlLiteral(time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	candidate := fmt.Sprintf(`
PREFIX ops: <%s>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
INSERT DATA {
  GRAPH %s {
    %s a ops:Incident .
    %s a ops:Deployment .
    %s a ops:Hypothesis ;
       ops:aboutIncident %s ;
       ops:relatedDeployment %s ;
       ops:summary %s ;
       ops:evidence %s ;
       ops:confidence "%s"^^xsd:decimal ;
       ops:recordedAt %s^^xsd:dateTime .
  }
}
`, opsNamespace, stagingRef, incidentIRI, deploymentIRI, hypothesisIRI,
		incidentIRI, deploymentIRI, summary, evidence, confidence, recordedAt)
	if err := c.Update(ctx, candidate); err != nil {
		return "", fmt.Errorf("stage hypothesis for SHACL validation: %w", err)
	}
	staged := true
	defer func() {
		if staged {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := c.Update(cleanupCtx, "DROP SILENT GRAPH "+stagingRef); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("clean up staged hypothesis: %w", err))
			}
		}
	}()

	if err := c.validateGraph(ctx, stagingIRI); err != nil {
		return "", err
	}

	promote := fmt.Sprintf(`
INSERT { ?subject ?predicate ?object }
WHERE { GRAPH %s { ?subject ?predicate ?object } } ;
DROP SILENT GRAPH %s
`, stagingRef, stagingRef)
	if err := c.Update(ctx, promote); err != nil {
		return "", fmt.Errorf("promote validated hypothesis: %w", err)
	}
	staged = false
	return "hypothesis-" + id, nil
}

func (c *Client) validateGraph(ctx context.Context, graphIRI string) error {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/shacl"
	query := endpoint.Query()
	query.Set("graph", graphIRI)
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(shapeDocument))
	if err != nil {
		return fmt.Errorf("create SHACL validation request: %w", err)
	}
	request.Header.Set("Content-Type", "text/turtle")
	request.Header.Set("Accept", "text/turtle")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("validate hypothesis with SHACL: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("read SHACL validation report: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("SHACL endpoint returned HTTP %d", response.StatusCode)
	}

	match := conformsPattern.FindSubmatch(body)
	if len(match) != 2 {
		return errors.New("SHACL endpoint returned an unrecognized validation report")
	}
	switch strings.ToLower(string(match[1])) {
	case "true":
		return nil
	case "false":
		return errors.New("hypothesis rejected by SHACL validation")
	default:
		return errors.New("SHACL endpoint returned an unrecognized validation report")
	}
}

func (c *Client) request(ctx context.Context, method, path, contentType, accept string, body []byte) ([]byte, error) {
	relative, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse Fuseki endpoint path: %w", err)
	}
	if relative.IsAbs() || relative.Host != "" || relative.Fragment != "" {
		return nil, errors.New("Fuseki endpoint path must be relative")
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + relative.Path
	endpoint.RawQuery = relative.RawQuery
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Fuseki request: %w", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call Fuseki endpoint: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read Fuseki response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Fuseki endpoint returned HTTP %d", response.StatusCode)
	}
	return responseBody, nil
}

func iri(value string) string {
	return "<" + value + ">"
}

func validateText(name, value string, minRunes, maxRunes int) error {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", name)
	}
	length := utf8.RuneCountInString(value)
	if length < minRunes || length > maxRunes {
		return fmt.Errorf("%s must contain between %d and %d characters", name, minRunes, maxRunes)
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return fmt.Errorf("%s contains an unsupported control character", name)
		}
	}
	return nil
}

func sparqlLiteral(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("literal must be valid UTF-8")
	}
	var escaped strings.Builder
	escaped.Grow(len(value) + 2)
	escaped.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			escaped.WriteString(`\\`)
		case '"':
			escaped.WriteString(`\"`)
		case '\n':
			escaped.WriteString(`\n`)
		case '\r':
			escaped.WriteString(`\r`)
		case '\t':
			escaped.WriteString(`\t`)
		default:
			if unicode.IsControl(r) {
				return "", errors.New("literal contains an unsupported control character")
			}
			escaped.WriteRune(r)
		}
	}
	escaped.WriteByte('"')
	return escaped.String(), nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:], nil
}
