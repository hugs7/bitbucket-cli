package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

// The dashboard endpoint pages its results; every authored PR up to
// the caller's limit must come back, not just the first page.
func TestListMyAuthoredPRsFollowsPages(t *testing.T) {
	const total, pageSize = 34, 25
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("role") != "AUTHOR" || q.Get("state") != "OPEN" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		start, _ := strconv.Atoi(q.Get("start"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		end := min(start+min(limit, pageSize), total)
		values := make([]string, 0, end-start)
		for id := start + 1; id <= end; id++ {
			values = append(values, fmt.Sprintf(
				`{"id":%d,"toRef":{"repository":{"slug":"repo","project":{"key":"PRJ"}}}}`, id))
		}
		fmt.Fprintf(w, `{"isLastPage":%t,"values":[%s]}`, end == total, strings.Join(values, ","))
	}))
	t.Cleanup(server.Close)

	client := New("example.test", config.Host{Type: "server", APIBase: server.URL + "/rest/api/1.0"})
	svc := &serverService{client: client, host: "example.test"}

	for _, tc := range []struct{ limit, want int }{{200, total}, {30, 30}, {10, 10}} {
		prs, err := svc.ListMyAuthoredPRs(tc.limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(prs) != tc.want {
			t.Fatalf("limit %d: got %d PRs, want %d", tc.limit, len(prs), tc.want)
		}
		if last := prs[len(prs)-1]; last.PR.ID != tc.want || last.Project != "PRJ" || last.Slug != "repo" {
			t.Fatalf("limit %d: last PR = %+v", tc.limit, last)
		}
	}
}
