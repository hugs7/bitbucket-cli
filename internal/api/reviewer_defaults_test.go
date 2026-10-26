package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

// reviewerTestService uses a context path to verify sibling Server plugin URLs.
func reviewerTestService(kind, host string) Service {
	cfg := config.Host{Type: kind, Username: "author", APIBase: host + "/bitbucket/rest/api/1.0"}
	if kind == "cloud" {
		cfg.APIBase = host + "/2.0"
		cfg.Username = "login@example.test"
		return &cloudService{client: New("example.test", cfg), host: "example.test"}
	}
	return &serverService{client: New("example.test", cfg), host: "example.test"}
}

func TestCreatePRReviewersAreAtomic(t *testing.T) {
	for _, kind := range []string{"server", "cloud"} {
		for _, mode := range []string{"defaults", "explicit", "empty", "author only", "no defaults"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				var body map[string]any
				posts, defaults, authorLookups := 0, 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/pull-requests") || strings.HasSuffix(r.URL.Path, "/pullrequests")):
						posts++
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						fmt.Fprint(w, `{"id":42,"title":"Title","state":"OPEN"}`)
					case r.Method != http.MethodGet:
						t.Errorf("unexpected mutation: %s %s", r.Method, r.URL)
						http.Error(w, "unexpected mutation", http.StatusBadRequest)
					case r.URL.Path == "/bitbucket/rest/api/1.0/projects/PRJ/repos/repo":
						fmt.Fprint(w, `{"id":17}`)
					case r.URL.Path == "/bitbucket/rest/default-reviewers/1.0/projects/PRJ/repos/repo/reviewers":
						defaults++
						want := map[string]string{"sourceRepoId": "17", "targetRepoId": "17", "sourceRefId": "refs/heads/feature/a+b", "targetRefId": "refs/heads/release/next"}
						for key, value := range want {
							if got := r.URL.Query().Get(key); got != value {
								t.Errorf("%s = %q, want %q", key, got, value)
							}
						}
						if mode == "no defaults" {
							fmt.Fprint(w, `[]`)
						} else {
							fmt.Fprint(w, `[{"name":"author"},{"name":"reviewer"},{"name":"REVIEWER"}]`)
						}
					case r.URL.Path == "/2.0/repositories/PRJ/repo/effective-default-reviewers":
						defaults++
						if mode == "no defaults" {
							fmt.Fprint(w, `{"values":[]}`)
						} else {
							fmt.Fprint(w, `{"values":[{"user":{"uuid":"{author}"},"reviewer_type":"project"},{"user":{"uuid":"{reviewer}"},"reviewer_type":"repository"},{"user":{"uuid":"{reviewer}"}}]}`)
						}
					case r.URL.Path == "/2.0/user":
						authorLookups++
						fmt.Fprint(w, `{"uuid":"{author}","nickname":"author"}`)
					case r.URL.Path == "/2.0/users/reviewer":
						fmt.Fprint(w, `{"uuid":"{reviewer}"}`)
					default:
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						http.NotFound(w, r)
					}
				}))
				t.Cleanup(server.Close)
				in := CreatePRInput{Title: "Title", Description: "Description", SourceRef: "feature/a+b", TargetRef: "release/next"}
				author, reviewer := "author", "reviewer"
				if kind == "cloud" {
					author, reviewer = "{author}", "{reviewer}"
				}
				switch mode {
				case "explicit":
					in.Reviewers = []string{author, reviewer, "reviewer"}
				case "empty":
					in.Reviewers = []string{}
				case "author only":
					in.Reviewers = []string{author}
				}
				pr, err := reviewerTestService(kind, server.URL).CreatePR("PRJ", "repo", in)
				if err != nil {
					t.Fatal(err)
				}
				if posts != 1 || pr.ID != 42 || pr.Title != "Title" || pr.State != "OPEN" {
					t.Fatalf("posts = %d, PR = %+v", posts, pr)
				}
				wantDefaults := 0
				if in.Reviewers == nil {
					wantDefaults = 1
				}
				if defaults != wantDefaults {
					t.Errorf("default lookups = %d, want %d", defaults, wantDefaults)
				}
				if kind == "cloud" && (mode == "empty" || mode == "no defaults") && authorLookups != 0 {
					t.Errorf("unnecessary author lookup for empty reviewers")
				}
				wantReviewers := []any{}
				if mode == "defaults" || mode == "explicit" {
					if kind == "cloud" {
						wantReviewers = append(wantReviewers, map[string]any{"uuid": reviewer})
					} else {
						wantReviewers = append(wantReviewers, map[string]any{"user": map[string]any{"name": reviewer}})
					}
				}
				wantBody := map[string]any{"title": in.Title, "description": in.Description, "reviewers": wantReviewers}
				if kind == "cloud" {
					wantBody["source"] = map[string]any{"branch": map[string]any{"name": in.SourceRef}}
					wantBody["destination"] = map[string]any{"branch": map[string]any{"name": in.TargetRef}}
				} else {
					wantBody["state"], wantBody["open"], wantBody["closed"] = "OPEN", true, false
					repo := map[string]any{"slug": "repo", "project": map[string]any{"key": "PRJ"}}
					wantBody["fromRef"] = map[string]any{"id": "refs/heads/" + in.SourceRef, "repository": repo}
					wantBody["toRef"] = map[string]any{"id": "refs/heads/" + in.TargetRef, "repository": repo}
				}
				if !reflect.DeepEqual(body, wantBody) {
					t.Errorf("body = %#v, want %#v", body, wantBody)
				}
			})
		}
	}
}

func TestServerDefaultReviewersAreBranchSpecific(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation: %s", r.Method)
		}
		switch r.URL.Path {
		case "/bitbucket/rest/api/1.0/projects/PRJ/repos/repo":
			fmt.Fprint(w, `{"id":23}`)
		case "/bitbucket/rest/default-reviewers/1.0/projects/PRJ/repos/repo/reviewers":
			if r.URL.Query().Get("sourceRefId") != "refs/heads/feature/one" {
				t.Errorf("wrong source: %s", r.URL)
			}
			if r.URL.Query().Get("targetRefId") == "refs/heads/main" {
				fmt.Fprint(w, `[{"name":"inherited","displayName":"Project Reviewer","emailAddress":"project@example.test"},{"name":"author"}]`)
			} else {
				fmt.Fprint(w, `[{"name":"release-owner","displayName":"Release Owner"}]`)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := reviewerTestService("server", server.URL)
	for _, tc := range []struct {
		target string
		want   []User
	}{
		{"main", []User{{Username: "inherited", DisplayName: "Project Reviewer", Email: "project@example.test"}}},
		{"refs/heads/release/next", []User{{Username: "release-owner", DisplayName: "Release Owner"}}},
	} {
		got, err := DefaultReviewers(svc, "PRJ", "repo", "refs/heads/feature/one", tc.target)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("target %s: users = %+v, err = %v, want %+v", tc.target, got, err, tc.want)
		}
	}
}

func TestCloudDefaultReviewersPagination(t *testing.T) {
	pages := 0
	var host string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation: %s", r.Method)
		}
		switch r.URL.Path {
		case "/2.0/repositories/PRJ/repo/effective-default-reviewers":
			pages++
			switch r.URL.Query().Get("page") {
			case "":
				fmt.Fprintf(w, `{"values":[{"user":{"uuid":"{one}","display_name":"Inherited"},"reviewer_type":"project"}],"next":%q}`, host+r.URL.Path+"?page=2")
			case "2":
				fmt.Fprint(w, `{"values":[{"user":{"uuid":"{one}"}},{"user":{"uuid":"{author}"}}],"next":"?page=3"}`)
			case "3":
				fmt.Fprint(w, `{"values":[{"user":{"uuid":"{two}","display_name":"Repository"},"reviewer_type":"repository"}]}`)
			default:
				t.Errorf("unexpected page: %s", r.URL)
			}
		case "/2.0/user":
			fmt.Fprint(w, `{"uuid":"{author}"}`)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	host = server.URL
	users, err := DefaultReviewers(reviewerTestService("cloud", host), "PRJ", "repo", "feature", "main")
	want := []User{{Username: "{one}", DisplayName: "Inherited"}, {Username: "{two}", DisplayName: "Repository"}}
	if err != nil || !reflect.DeepEqual(users, want) || pages != 3 {
		t.Fatalf("users = %+v, pages = %d, err = %v; want %+v", users, pages, err, want)
	}
}

func TestReviewerErrorsPreventPRCreation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      string
		failure   string
		status    int
		response  string
		wantError string
	}{
		{"server repository lookup", "server", "repo", 403, `denied`, "resolve repository ID"},
		{"server missing ID", "server", "repo", 200, `{}`, "no valid ID"},
		{"server defaults forbidden", "server", "defaults", 403, `denied`, "HTTP 403"},
		{"server defaults unavailable", "server", "defaults", 404, `missing`, "HTTP 404"},
		{"server malformed defaults", "server", "defaults", 200, `{}`, "cannot unmarshal"},
		{"server null defaults", "server", "defaults", 200, `null`, "not a user list"},
		{"server missing user identity", "server", "defaults", 200, `[{"displayName":"Reviewer"}]`, "no user identity"},
		{"cloud defaults forbidden", "cloud", "defaults", 403, `denied`, "HTTP 403"},
		{"cloud malformed defaults", "cloud", "defaults", 200, `{`, "EOF"},
		{"cloud missing values", "cloud", "defaults", 200, `{}`, "no values list"},
		{"cloud missing user identity", "cloud", "defaults", 200, `{"values":[{"user":{"display_name":"Reviewer"}}]}`, "no user identity"},
		{"cloud next page fails", "cloud", "page2", 500, `unavailable`, "HTTP 500"},
		{"cloud author lookup fails", "cloud", "author", 403, `denied`, "resolve PR author"},
		{"cloud missing author identity", "cloud", "author", 200, `{}`, "cannot determine PR author"},
		{"cloud pagination loop", "cloud", "defaults", 200, `{"values":[],"next":"?page=2"}`, "repeated a page"},
		{"cloud external next page", "cloud", "defaults", 200, `{"values":[],"next":"https://other.test/2.0/reviewers"}`, "outside the API host"},
		{"cloud invalid next page", "cloud", "defaults", 200, `{"values":[],"next":"http://["}`, "invalid default reviewers next page"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					posts++
					http.Error(w, "must not create PR", http.StatusBadRequest)
					return
				}
				stage, response := "defaults", `[{"name":"reviewer"}]`
				switch {
				case r.URL.Path == "/bitbucket/rest/api/1.0/projects/PRJ/repos/repo":
					stage, response = "repo", `{"id":23}`
				case r.URL.Path == "/2.0/user":
					stage, response = "author", `{"uuid":"{author}"}`
				case r.URL.Query().Get("page") == "2" && tc.failure == "page2":
					stage = "page2"
				case tc.kind == "cloud":
					response = `{"values":[{"user":{"uuid":"{reviewer}"}}]}`
					if tc.failure == "page2" {
						response = `{"values":[{"user":{"uuid":"{reviewer}"}}],"next":"?page=2"}`
					}
				}
				if stage == tc.failure {
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.response)
					return
				}
				fmt.Fprint(w, response)
			}))
			t.Cleanup(server.Close)
			pr, err := reviewerTestService(tc.kind, server.URL).CreatePR("PRJ", "repo", CreatePRInput{Title: "Title", SourceRef: "feature", TargetRef: "main"})
			if err == nil || !strings.Contains(err.Error(), "load default reviewers") || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("err = %v, want default reviewer error containing %q", err, tc.wantError)
			}
			if posts != 0 || pr != nil {
				t.Fatalf("posts = %d, PR = %+v; want no creation", posts, pr)
			}
		})
	}
}

func TestDefaultReviewersOptionalCapability(t *testing.T) {
	// Embedding Service supplies its methods but not the optional capability.
	var unsupported struct{ Service }
	if _, err := DefaultReviewers(unsupported, "PRJ", "repo", "feature", "main"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("err = %v, want unsupported capability", err)
	}
}

func TestExplicitReviewerErrorsPreventPRCreation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      string
		reviewers []string
		wantError string
	}{
		{"server blank reviewer", "server", []string{" "}, "no user identity"},
		{"cloud blank reviewer", "cloud", []string{" "}, "no user identity"},
		{"cloud unknown nickname", "cloud", []string{"unknown"}, "resolve PR reviewer"},
		{"cloud author unavailable", "cloud", []string{"{reviewer}"}, "resolve PR author"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || (r.URL.Path != "/2.0/users/unknown" && r.URL.Path != "/2.0/user") {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				http.Error(w, "unavailable", http.StatusForbidden)
			}))
			t.Cleanup(server.Close)
			in := CreatePRInput{Title: "Title", SourceRef: "feature", TargetRef: "main", Reviewers: tc.reviewers}
			pr, err := reviewerTestService(tc.kind, server.URL).CreatePR("PRJ", "repo", in)
			if pr != nil || err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("PR = %+v, err = %v, want %q", pr, err, tc.wantError)
			}
		})
	}
}

func TestServerReviewersRequireAuthorIdentity(t *testing.T) {
	svc := reviewerTestService("server", "http://unused.test").(*serverService)
	svc.client.cfg.Username = ""
	_, err := svc.CreatePR("PRJ", "repo", CreatePRInput{Reviewers: []string{"reviewer"}})
	if err == nil || !strings.Contains(err.Error(), "cannot determine PR author") {
		t.Fatalf("err = %v, want missing author error before HTTP requests", err)
	}
}
