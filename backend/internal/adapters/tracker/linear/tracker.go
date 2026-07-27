// Package linear implements the read-only tracker port against Linear's
// GraphQL API.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	defaultEndpoint  = "https://api.linear.app/graphql"
	defaultUserAgent = "ao-agent-orchestrator/tracker-linear"
	listPageSize     = 50
	maxListPages     = 100
)

var (
	// ErrNoCredential indicates that no unambiguous AO-scoped Linear
	// credential was available.
	ErrNoCredential = errors.New("linear tracker: no credential")
	// ErrNotFound indicates that Linear could not resolve an issue.
	ErrNotFound = errors.New("linear tracker: issue not found")
	// ErrRateLimited indicates that Linear rejected the request for rate limits.
	ErrRateLimited = errors.New("linear tracker: rate limited")
	// ErrAuthFailed indicates that Linear rejected or could not resolve the identity.
	ErrAuthFailed = errors.New("linear tracker: authentication failed")
	// ErrWrongProvider indicates that a non-Linear tracker ID reached this adapter.
	ErrWrongProvider = errors.New("linear tracker: id is not a linear tracker id")
	// ErrBadID indicates that an issue identifier is empty or contains whitespace.
	ErrBadID = errors.New("linear tracker: malformed native id")
	// ErrBadScope indicates that a project UUID is empty or contains whitespace.
	ErrBadScope = errors.New("linear tracker: malformed project scope")
)

// AuthorizationSource returns the complete Authorization header value. Linear
// personal API keys are sent raw; OAuth access tokens use the Bearer scheme.
type AuthorizationSource interface {
	Authorization(context.Context) (string, error)
}

// StaticAuthorizationSource is primarily useful for tests.
type StaticAuthorizationSource string

// Authorization returns the source verbatim after rejecting an empty value.
func (s StaticAuthorizationSource) Authorization(context.Context) (string, error) {
	value := strings.TrimSpace(string(s))
	if value == "" {
		return "", ErrNoCredential
	}
	return value, nil
}

// EnvironmentAuthorizationSource reads an explicitly AO-scoped Linear
// credential. Exactly one source may be configured so an accidental broad
// personal key cannot silently override an OAuth app identity.
type EnvironmentAuthorizationSource struct {
	Getenv func(string) string
}

// Authorization selects exactly one AO-scoped personal API key or OAuth token.
func (s EnvironmentAuthorizationSource) Authorization(context.Context) (string, error) {
	getenv := s.Getenv
	if getenv == nil {
		return "", ErrNoCredential
	}
	apiKey := strings.TrimSpace(getenv("AO_LINEAR_API_KEY"))
	oauth := strings.TrimSpace(getenv("AO_LINEAR_OAUTH_TOKEN"))
	if apiKey != "" && oauth != "" {
		return "", fmt.Errorf("linear tracker: configure only one of AO_LINEAR_API_KEY or AO_LINEAR_OAUTH_TOKEN")
	}
	if apiKey != "" {
		return apiKey, nil
	}
	if oauth != "" {
		return "Bearer " + oauth, nil
	}
	return "", ErrNoCredential
}

// Options defines the authentication and transport boundaries for a Tracker.
// Endpoint and HTTPClient are injectable so contract tests never need a real
// Linear workspace.
type Options struct {
	Authorization AuthorizationSource
	HTTPClient    *http.Client
	Endpoint      string
	UserAgent     string
}

// Tracker implements ports.Tracker for a project-scoped Linear intake lane.
type Tracker struct {
	http          *http.Client
	authorization AuthorizationSource
	endpoint      string
	userAgent     string
	preflightOK   atomic.Bool
	preflightMu   sync.Mutex
}

var _ ports.Tracker = (*Tracker)(nil)

// New constructs a read-only Linear tracker and validates that its credential
// source can produce one unambiguous Authorization header.
func New(opts Options) (*Tracker, error) {
	if opts.Authorization == nil {
		return nil, ErrNoCredential
	}
	if _, err := opts.Authorization.Authorization(context.Background()); err != nil {
		return nil, err
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	userAgent := strings.TrimSpace(opts.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	return &Tracker{
		http:          client,
		authorization: opts.Authorization,
		endpoint:      endpoint,
		userAgent:     userAgent,
	}, nil
}

const issueFields = `
id
identifier
title
description
url
state { name type }
labels { nodes { name } }
assignee { id name email }
`

const issueQuery = `query AOIssue($id: String!) {
  issue(id: $id) {` + issueFields + `}
}`

const projectIssuesQuery = `query AOProjectIssues($project: String!, $first: Int!, $after: String) {
  project(id: $project) {
    issues(first: $first, after: $after, orderBy: updatedAt) {
      nodes {` + issueFields + `}
      pageInfo { hasNextPage endCursor }
    }
  }
}`

const viewerQuery = `query AOViewer { viewer { id } }`

type rawIssue struct {
	ID          string  `json:"id"`
	Identifier  string  `json:"identifier"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	URL         string  `json:"url"`
	State       struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"state"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Assignee *struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"assignee"`
}

type pageInfo struct {
	HasNextPage bool    `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type graphQLError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code string `json:"code"`
	} `json:"extensions"`
}

type graphQLResponse[T any] struct {
	Data   T              `json:"data"`
	Errors []graphQLError `json:"errors"`
}

// Get resolves a Linear identifier or UUID and normalizes the result. The
// returned issue always uses Linear's stable UUID as its canonical tracker ID.
func (t *Tracker) Get(ctx context.Context, id domain.TrackerID) (domain.Issue, error) {
	if id.Provider != domain.TrackerProviderLinear {
		return domain.Issue{}, fmt.Errorf("%w: provider=%q", ErrWrongProvider, id.Provider)
	}
	native := strings.TrimSpace(id.Native)
	if native == "" || strings.ContainsAny(native, " \t\r\n") {
		return domain.Issue{}, ErrBadID
	}
	var data struct {
		Issue *rawIssue `json:"issue"`
	}
	if err := t.query(ctx, issueQuery, map[string]any{"id": native}, &data); err != nil {
		return domain.Issue{}, err
	}
	if data.Issue == nil {
		return domain.Issue{}, ErrNotFound
	}
	return issueFromLinear(*data.Issue), nil
}

// List returns issues from one Linear project UUID. Provider-neutral filters
// are applied after each bounded project page because required-label semantics
// require every requested label, while Linear's relationship filter matches
// any label by default.
func (t *Tracker) List(ctx context.Context, scope domain.TrackerRepo, filter domain.ListFilter) ([]domain.Issue, error) {
	if scope.Provider != domain.TrackerProviderLinear {
		return nil, fmt.Errorf("%w: provider=%q", ErrWrongProvider, scope.Provider)
	}
	project := strings.TrimSpace(scope.Native)
	if project == "" || strings.ContainsAny(project, " \t\r\n") {
		return nil, ErrBadScope
	}
	var out []domain.Issue
	var after *string
	for page := 0; page < maxListPages; page++ {
		var data struct {
			Project *struct {
				Issues struct {
					Nodes    []rawIssue `json:"nodes"`
					PageInfo pageInfo   `json:"pageInfo"`
				} `json:"issues"`
			} `json:"project"`
		}
		if err := t.query(ctx, projectIssuesQuery, map[string]any{
			"project": project,
			"first":   listPageSize,
			"after":   after,
		}, &data); err != nil {
			return nil, err
		}
		if data.Project == nil {
			return nil, ErrBadScope
		}
		for _, raw := range data.Project.Issues.Nodes {
			issue := issueFromLinear(raw)
			if matchesFilter(issue, filter) {
				out = append(out, issue)
				if filter.Limit > 0 && len(out) >= filter.Limit {
					return out, nil
				}
			}
		}
		info := data.Project.Issues.PageInfo
		if !info.HasNextPage {
			return out, nil
		}
		if info.EndCursor == nil || strings.TrimSpace(*info.EndCursor) == "" {
			return nil, fmt.Errorf("linear tracker: pagination advertised a next page without a cursor")
		}
		after = info.EndCursor
	}
	return nil, fmt.Errorf("linear tracker: pagination exceeded %d pages", maxListPages)
}

// Preflight validates the active Linear identity once per Tracker instance.
func (t *Tracker) Preflight(ctx context.Context) error {
	if t.preflightOK.Load() {
		return nil
	}
	t.preflightMu.Lock()
	defer t.preflightMu.Unlock()
	if t.preflightOK.Load() {
		return nil
	}
	var data struct {
		Viewer *struct {
			ID string `json:"id"`
		} `json:"viewer"`
	}
	if err := t.query(ctx, viewerQuery, nil, &data); err != nil {
		return err
	}
	if data.Viewer == nil || strings.TrimSpace(data.Viewer.ID) == "" {
		return ErrAuthFailed
	}
	t.preflightOK.Store(true)
	return nil
}

func (t *Tracker) query(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return fmt.Errorf("linear tracker: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("linear tracker: build request: %w", err)
	}
	auth, err := t.authorization.Authorization(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", t.userAgent)
	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("linear tracker: request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	const maxResponseBytes = 4 << 20
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("linear tracker: read response: %w", err)
	}
	if len(payload) > maxResponseBytes {
		return fmt.Errorf("linear tracker: response exceeds %d bytes", maxResponseBytes)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrAuthFailed
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("linear tracker: HTTP %d", resp.StatusCode)
	}
	var envelope graphQLResponse[json.RawMessage]
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("linear tracker: decode response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return classifyGraphQLError(envelope.Errors[0])
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("linear tracker: response omitted data")
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("linear tracker: decode data: %w", err)
	}
	return nil
}

func classifyGraphQLError(err graphQLError) error {
	switch strings.ToUpper(strings.TrimSpace(err.Extensions.Code)) {
	case "RATELIMITED", "RATE_LIMITED":
		return ErrRateLimited
	case "AUTHENTICATION_ERROR", "FORBIDDEN":
		return ErrAuthFailed
	case "ENTITY_NOT_FOUND", "NOT_FOUND":
		return ErrNotFound
	default:
		if err.Message == "" {
			return errors.New("linear tracker: GraphQL request failed")
		}
		return fmt.Errorf("linear tracker: GraphQL request failed: %s", err.Message)
	}
}

func issueFromLinear(raw rawIssue) domain.Issue {
	labels := make([]string, 0, len(raw.Labels.Nodes))
	for _, label := range raw.Labels.Nodes {
		if name := strings.TrimSpace(label.Name); name != "" {
			labels = append(labels, name)
		}
	}
	var assignees []string
	if raw.Assignee != nil {
		for _, value := range []string{raw.Assignee.ID, raw.Assignee.Email, raw.Assignee.Name} {
			if value = strings.TrimSpace(value); value != "" {
				assignees = append(assignees, value)
			}
		}
	}
	body := ""
	if raw.Description != nil {
		body = *raw.Description
	}
	native := strings.TrimSpace(raw.ID)
	if native == "" {
		native = strings.TrimSpace(raw.Identifier)
	}
	return domain.Issue{
		ID:         domain.TrackerID{Provider: domain.TrackerProviderLinear, Native: native},
		Identifier: strings.TrimSpace(raw.Identifier),
		Title:      raw.Title,
		Body:       body,
		State:      mapState(raw.State.Type, raw.State.Name),
		URL:        raw.URL,
		Labels:     nilIfEmpty(labels),
		Assignees:  nilIfEmpty(assignees),
	}
}

func mapState(stateType, name string) domain.NormalizedIssueState {
	switch strings.ToLower(strings.TrimSpace(stateType)) {
	case "completed":
		return domain.IssueDone
	case "canceled", "cancelled":
		return domain.IssueCancelled
	case "started":
		if strings.Contains(strings.ToLower(name), "review") {
			return domain.IssueInReview
		}
		return domain.IssueInProgress
	default:
		return domain.IssueOpen
	}
}

func matchesFilter(issue domain.Issue, filter domain.ListFilter) bool {
	switch filter.State {
	case domain.ListOpen:
		if issue.State == domain.IssueDone || issue.State == domain.IssueCancelled {
			return false
		}
	case domain.ListClosed:
		if issue.State != domain.IssueDone && issue.State != domain.IssueCancelled {
			return false
		}
	}
	for _, label := range filter.Labels {
		if !containsFold(issue.Labels, label) {
			return false
		}
	}
	assignee := strings.TrimSpace(filter.Assignee)
	switch {
	case assignee == "":
	case assignee == "*":
		if len(issue.Assignees) == 0 {
			return false
		}
	case strings.EqualFold(assignee, "none"):
		if len(issue.Assignees) != 0 {
			return false
		}
	case !containsFold(issue.Assignees, assignee):
		return false
	}
	return true
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}

func nilIfEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}
