package linear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func ExampleEnvironmentAuthorizationSource_Authorization() {
	source := EnvironmentAuthorizationSource{
		Getenv: func(key string) string {
			if key == "AO_LINEAR_OAUTH_TOKEN" {
				return "oauth-token"
			}
			return ""
		},
	}
	authorization, _ := source.Authorization(context.Background())
	fmt.Println(authorization)
	// Output: Bearer oauth-token
}

func ExampleNew() {
	_, err := New(Options{Authorization: StaticAuthorizationSource("")})
	fmt.Println(errors.Is(err, ErrNoCredential))
	// Output: true
}

type fakeLinear struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	requests []map[string]any
	handler  func(http.ResponseWriter, map[string]any)
}

func newFakeLinear(t *testing.T, handler func(http.ResponseWriter, map[string]any)) *fakeLinear {
	t.Helper()
	f := &fakeLinear{t: t, handler: handler}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "lin-test" {
			t.Errorf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		f.mu.Lock()
		f.requests = append(f.requests, request)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		handler(w, request)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func newTrackerForTest(t *testing.T, f *fakeLinear) *Tracker {
	t.Helper()
	tracker, err := New(Options{
		Authorization: StaticAuthorizationSource("lin-test"),
		HTTPClient:    f.server.Client(),
		Endpoint:      f.server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tracker
}

func TestEnvironmentAuthorizationSource(t *testing.T) {
	values := map[string]string{}
	source := EnvironmentAuthorizationSource{Getenv: func(key string) string { return values[key] }}
	if _, err := source.Authorization(context.Background()); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("missing credential = %v", err)
	}
	values["AO_LINEAR_API_KEY"] = "key"
	if got, _ := source.Authorization(context.Background()); got != "key" {
		t.Fatalf("api key authorization = %q", got)
	}
	values["AO_LINEAR_API_KEY"] = ""
	values["AO_LINEAR_OAUTH_TOKEN"] = "oauth"
	if got, _ := source.Authorization(context.Background()); got != "Bearer oauth" {
		t.Fatalf("oauth authorization = %q", got)
	}
	values["AO_LINEAR_API_KEY"] = "key"
	if _, err := source.Authorization(context.Background()); err == nil {
		t.Fatal("both credential sources were accepted")
	}
}

func TestGetMapsLinearIssue(t *testing.T) {
	f := newFakeLinear(t, func(w http.ResponseWriter, request map[string]any) {
		variables := request["variables"].(map[string]any)
		if variables["id"] != "FUQ-7" {
			t.Errorf("id = %#v", variables["id"])
		}
		_, _ = io.WriteString(w, `{"data":{"issue":{
			"id":"issue-uuid","identifier":"FUQ-7","title":"Run canary",
			"description":"Verify the loop","url":"https://linear.app/x/FUQ-7",
			"state":{"name":"In Review","type":"started"},
			"labels":{"nodes":[{"name":"agent"}]},
			"assignee":{"id":"user-1","name":"FuQing Zhang","email":"user@example.test"}
		}}}`)
	})
	issue, err := newTrackerForTest(t, f).Get(context.Background(), domain.TrackerID{
		Provider: domain.TrackerProviderLinear,
		Native:   "FUQ-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if issue.ID.Native != "issue-uuid" || issue.Identifier != "FUQ-7" ||
		issue.State != domain.IssueInReview || len(issue.Assignees) != 3 {
		t.Fatalf("issue = %#v", issue)
	}
}

func TestListPaginatesProjectAndFilters(t *testing.T) {
	page := 0
	f := newFakeLinear(t, func(w http.ResponseWriter, request map[string]any) {
		page++
		variables := request["variables"].(map[string]any)
		if variables["project"] != "project-uuid" {
			t.Errorf("project = %#v", variables["project"])
		}
		if page == 1 {
			_, _ = io.WriteString(w, `{"data":{"project":{"issues":{
				"nodes":[
					{"id":"1","identifier":"FUQ-1","title":"one","state":{"name":"Todo","type":"unstarted"},"labels":{"nodes":[{"name":"agent"}]},"assignee":{"id":"u1","name":"Alice","email":"alice@example.test"}},
					{"id":"2","identifier":"FUQ-2","title":"two","state":{"name":"Done","type":"completed"},"labels":{"nodes":[{"name":"agent"}]}}
				],
				"pageInfo":{"hasNextPage":true,"endCursor":"next"}
			}}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"project":{"issues":{
			"nodes":[{"id":"3","identifier":"FUQ-3","title":"three","state":{"name":"Started","type":"started"},"labels":{"nodes":[{"name":"agent"}]},"assignee":{"id":"u2","name":"Bob","email":"bob@example.test"}}],
			"pageInfo":{"hasNextPage":false,"endCursor":null}
		}}}}`)
	})
	issues, err := newTrackerForTest(t, f).List(context.Background(), domain.TrackerRepo{
		Provider: domain.TrackerProviderLinear,
		Native:   "project-uuid",
	}, domain.ListFilter{State: domain.ListOpen, Labels: []string{"agent"}, Assignee: "*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || issues[0].Identifier != "FUQ-1" || issues[1].Identifier != "FUQ-3" {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestPreflightCachesSuccess(t *testing.T) {
	calls := 0
	f := newFakeLinear(t, func(w http.ResponseWriter, request map[string]any) {
		calls++
		if !strings.Contains(request["query"].(string), "viewer") {
			t.Errorf("query = %q", request["query"])
		}
		_, _ = io.WriteString(w, `{"data":{"viewer":{"id":"me"}}}`)
	})
	tracker := newTrackerForTest(t, f)
	if err := tracker.Preflight(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Preflight(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestGraphQLErrorClassification(t *testing.T) {
	f := newFakeLinear(t, func(w http.ResponseWriter, _ map[string]any) {
		_, _ = io.WriteString(w, `{"errors":[{"message":"slow down","extensions":{"code":"RATELIMITED"}}]}`)
	})
	_, err := newTrackerForTest(t, f).Get(context.Background(), domain.TrackerID{
		Provider: domain.TrackerProviderLinear,
		Native:   "FUQ-7",
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectsOversizedResponse(t *testing.T) {
	f := newFakeLinear(t, func(w http.ResponseWriter, _ map[string]any) {
		_, _ = io.WriteString(w, strings.Repeat("x", (4<<20)+1))
	})
	_, err := newTrackerForTest(t, f).Get(context.Background(), domain.TrackerID{
		Provider: domain.TrackerProviderLinear,
		Native:   "FUQ-7",
	})
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectsWrongProviderAndScope(t *testing.T) {
	f := newFakeLinear(t, func(w http.ResponseWriter, _ map[string]any) {
		_, _ = io.WriteString(w, `{"data":{}}`)
	})
	tracker := newTrackerForTest(t, f)
	if _, err := tracker.Get(context.Background(), domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "x"}); !errors.Is(err, ErrWrongProvider) {
		t.Fatalf("wrong provider = %v", err)
	}
	if _, err := tracker.List(context.Background(), domain.TrackerRepo{Provider: domain.TrackerProviderLinear}, domain.ListFilter{}); !errors.Is(err, ErrBadScope) {
		t.Fatalf("bad scope = %v", err)
	}
}
