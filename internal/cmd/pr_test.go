package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

func TestCreateReviewerFlags(t *testing.T) {
	for _, tc := range []struct {
		name        string
		flags, want []string
	}{
		{"defaults", nil, []string{"default-reviewer"}},
		{"none", []string{"--no-reviewers"}, []string{}},
		{"explicit", []string{"--reviewer", "alice,bob"}, []string{"alice", "bob"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "POST":
					var body struct {
						Reviewers []struct {
							User struct {
								Name string `json:"name"`
							} `json:"user"`
						} `json:"reviewers"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					got = []string{}
					for _, reviewer := range body.Reviewers {
						got = append(got, reviewer.User.Name)
					}
					fmt.Fprint(w, `{"id":1}`)
				case r.URL.Path == "/rest/default-reviewers/1.0/projects/P/repos/repo/reviewers":
					fmt.Fprint(w, `[{"name":"default-reviewer"}]`)
				default:
					fmt.Fprint(w, `{"id":42}`)
				}
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(path, []byte("hosts:\n  test:\n    type: server\n    username: author\n    api_base: "+server.URL+"/rest/api/1.0\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := config.Load(path); err != nil {
				t.Fatal(err)
			}
			cmd := newPRCreateCmd()
			cmd.SetArgs(append([]string{"-R", "test/P/repo", "-y", "--source", "feature/work", "--target", "main", "--title", "Test"}, tc.flags...))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("reviewers = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPRCreateHasNonInteractiveFlag(t *testing.T) {
	flag := newPRCreateCmd().Flags().ShorthandLookup("y")
	if flag == nil || flag.Name != "yes" {
		t.Fatal("pr create is missing the -y/--yes flag")
	}
}

func TestPRMergeRetargetsDependentsByDefault(t *testing.T) {
	flag := newPRMergeCmd().Flags().Lookup("retarget-dependents")
	if flag == nil {
		t.Fatal("retarget-dependents flag is missing")
	}
	if flag.DefValue != "true" {
		t.Fatalf("retarget-dependents default = %q, want true", flag.DefValue)
	}
}
