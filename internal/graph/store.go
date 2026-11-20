// Package graph reads and writes the operational knowledge graph in TypeDB.
// The model never sends TypeQL: callers pass slugs and text, and this package
// builds fixed queries. The TypeDB schema enforces the ontology on writes.
package graph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jedipunkz/ops-ontology-playground/internal/typedb"
)

var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// ErrReferenceMismatch means the incident and deployment do not exist or do
// not target the same application.
var ErrReferenceMismatch = errors.New("incident and deployment must exist and refer to the same application")

type Store struct {
	client   *typedb.Client
	database string
}

type Hypothesis struct {
	IncidentID   string
	DeploymentID string
	Summary      string
	Evidence     string
	Confidence   float64
}

type Document = map[string]any

func NewStore(client *typedb.Client, database string) (*Store, error) {
	if !slugPattern.MatchString(database) {
		return nil, errors.New("database name must be a lowercase slug")
	}
	return &Store{client: client, database: database}, nil
}

// Reset recreates the database with the schema and sample data. It deletes
// every hypothesis recorded so far.
func (s *Store) Reset(ctx context.Context, schema, data string) error {
	exists, err := s.client.DatabaseExists(ctx, s.database)
	if err != nil {
		return err
	}
	if exists {
		if err := s.client.DeleteDatabase(ctx, s.database); err != nil {
			return fmt.Errorf("delete database: %w", err)
		}
	}
	if err := s.client.CreateDatabase(ctx, s.database); err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	if _, err := s.client.Query(ctx, s.database, typedb.Schema, schema, true); err != nil {
		return fmt.Errorf("define schema: %w", err)
	}
	if _, err := s.client.Query(ctx, s.database, typedb.Write, data, true); err != nil {
		return fmt.Errorf("insert sample data: %w", err)
	}
	return nil
}

// DescribeOntology returns the schema as a TypeQL define query, so an agent
// learns the types and constraints from the database itself.
func (s *Store) DescribeOntology(ctx context.Context) (string, error) {
	return s.client.Schema(ctx, s.database)
}

func (s *Store) SearchApplication(ctx context.Context, application string) (incidents, deployments []Document, err error) {
	if !slugPattern.MatchString(application) {
		return nil, nil, errors.New("application must be a lowercase slug")
	}
	app := quote(application)
	incidents, err = s.fetch(ctx, fmt.Sprintf(`
match
  $a isa application, has slug %s;
  impact (incident: $i, application: $a);
  $i has started-at $t;
sort $t desc;
limit 100;
fetch {
  "incident": $i.slug,
  "severity": $i.severity,
  "summary": $i.summary,
  "started_at": $t
};`, app))
	if err != nil {
		return nil, nil, err
	}
	deployments, err = s.fetch(ctx, fmt.Sprintf(`
match
  $a isa application, has slug %s;
  deployment-target (deployment: $d, application: $a);
  $d has deployed-at $t;
sort $t desc;
limit 100;
fetch {
  "deployment": $d.slug,
  "version": $d.version,
  "deployed_at": $t
};`, app))
	return incidents, deployments, err
}

func (s *Store) ListHypotheses(ctx context.Context, incidentID string) ([]Document, error) {
	if !slugPattern.MatchString(incidentID) {
		return nil, errors.New("incident ID must be a lowercase slug")
	}
	return s.fetch(ctx, fmt.Sprintf(`
match
  $i isa incident, has slug %s;
  $h isa root-cause-hypothesis, links (incident: $i, deployment: $d);
  $h has recorded-at $t;
sort $t desc;
limit 50;
fetch {
  "hypothesis": $h.slug,
  "deployment": $d.slug,
  "summary": $h.summary,
  "evidence": $h.evidence,
  "confidence": $h.confidence,
  "recorded_at": $t
};`, quote(incidentID)))
}

// RecordHypothesis inserts a hypothesis in one write transaction. The match
// clause requires the incident and deployment to share an application; when
// it matches nothing, TypeDB inserts nothing and the call is rejected. The
// schema rejects values outside the ontology (for example confidence > 1).
func (s *Store) RecordHypothesis(ctx context.Context, hypothesis Hypothesis) (string, error) {
	if !slugPattern.MatchString(hypothesis.IncidentID) {
		return "", errors.New("incident ID must be a lowercase slug")
	}
	if !slugPattern.MatchString(hypothesis.DeploymentID) {
		return "", errors.New("deployment ID must be a lowercase slug")
	}
	if math.IsNaN(hypothesis.Confidence) || math.IsInf(hypothesis.Confidence, 0) {
		return "", errors.New("confidence must be a finite number")
	}
	if err := validateText("summary", hypothesis.Summary, 10, 500); err != nil {
		return "", err
	}
	if err := validateText("evidence", hypothesis.Evidence, 10, 2000); err != nil {
		return "", err
	}

	id, err := newID()
	if err != nil {
		return "", fmt.Errorf("create hypothesis ID: %w", err)
	}
	slug := "hypothesis-" + id
	response, err := s.client.Query(ctx, s.database, typedb.Write, fmt.Sprintf(`
match
  $i isa incident, has slug %s;
  $d isa deployment, has slug %s;
  impact (incident: $i, application: $a);
  deployment-target (deployment: $d, application: $a);
insert
  $h isa root-cause-hypothesis, links (incident: $i, deployment: $d),
    has slug %s,
    has summary %s,
    has evidence %s,
    has confidence %s,
    has recorded-at %s;`,
		quote(hypothesis.IncidentID),
		quote(hypothesis.DeploymentID),
		quote(slug),
		quote(strings.TrimSpace(hypothesis.Summary)),
		quote(strings.TrimSpace(hypothesis.Evidence)),
		doubleLiteral(hypothesis.Confidence),
		time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	), true)
	if err != nil {
		return "", err
	}
	if len(response.Answers) != 1 {
		return "", ErrReferenceMismatch
	}
	return slug, nil
}

func (s *Store) fetch(ctx context.Context, query string) ([]Document, error) {
	response, err := s.client.Query(ctx, s.database, typedb.Read, query, false)
	if err != nil {
		return nil, err
	}
	documents := make([]Document, 0, len(response.Answers))
	for _, answer := range response.Answers {
		var document Document
		if err := json.Unmarshal(answer, &document); err != nil {
			return nil, fmt.Errorf("decode TypeDB document: %w", err)
		}
		documents = append(documents, document)
	}
	return documents, nil
}

// doubleLiteral always includes a decimal point so TypeQL reads a double,
// not an integer.
func doubleLiteral(value float64) string {
	literal := strconv.FormatFloat(value, 'f', -1, 64)
	if !strings.Contains(literal, ".") {
		literal += ".0"
	}
	return literal
}

// quote renders a TypeQL string literal. validateText already rejects
// control characters, so only the quote and backslash need escaping.
func quote(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(value) + `"`
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
		if unicode.IsControl(r) {
			return fmt.Errorf("%s must be a single line without control characters", name)
		}
	}
	return nil
}

func newID() (string, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
