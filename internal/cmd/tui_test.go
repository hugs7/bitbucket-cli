package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	pty "github.com/aymanbagabas/go-pty"
	"github.com/hinshun/vt10x"
)

// Exercise the actual command entry points and Bubble Tea programs in a PTY:
// Back must navigate to the same parents, while Quit must exit the process.
func TestTUINavigation(t *testing.T) {
	if raw := os.Getenv("BB_TEST_TUI_ARGS"); raw != "" {
		var args []string
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			t.Fatal(err)
		}
		root := NewRootCmd(BuildInfo{Version: "dev"})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("PTY navigation integration runs on Unix; native Windows needs ConPTY CI")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("navigation sent a write: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/repo"):
			fmt.Fprint(w, `{"id":7,"slug":"repo","name":"Navigation fixture","project":{"key":"PRJ"}}`)
		case strings.HasSuffix(r.URL.Path, "/default-branch"):
			fmt.Fprint(w, `{"id":"refs/heads/main","displayId":"main"}`)
		case strings.HasSuffix(r.URL.Path, "/pull-requests") && !strings.Contains(r.URL.Path, "/settings/"):
			fmt.Fprint(w, `{"values":[{"id":41,"title":"First PR","state":"OPEN","author":{"user":{"name":"other"}},"fromRef":{"displayId":"feature/first"},"toRef":{"displayId":"main"}},{"id":42,"title":"Linked PR","state":"OPEN","author":{"user":{"name":"other"}},"fromRef":{"displayId":"feature/linked"},"toRef":{"displayId":"main"}}],"isLastPage":true}`)
		case strings.HasSuffix(r.URL.Path, "/settings/pull-requests"):
			fmt.Fprint(w, `{"requiredApprovers":0}`)
		case strings.HasSuffix(r.URL.Path, "/conditions"):
			fmt.Fprint(w, `[]`)
		default:
			fmt.Fprint(w, `{"values":[],"isLastPage":true}`)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	config := fmt.Sprintf("default_host: bitbucket.test\nhosts:\n  bitbucket.test:\n    type: server\n    username: reader\n    api_base: %s/rest/api/1.0\n", server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", dir}, {"-C", dir, "remote", "add", "origin", "ssh://git@bitbucket.test/PRJ/repo.git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v: %s", err, out)
		}
	}
	type step struct{ key, want string }
	const (
		esc      = "\x1b"
		prs      = "2 shown"
		repo     = "REPO · PRJ/repo"
		settings = "REPO SETTINGS · PRJ/repo"
		home     = "BB · HOME"
	)
	roundTrip := []step{{"p", prs}, {esc, repo}, {"S", settings}, {esc, repo}, {esc, home}}
	for _, tc := range []struct {
		name    string
		args    []string
		initial string
		steps   []step
		quit    string
	}{
		{"direct PRs", []string{"prs", "-R", "bitbucket.test/PRJ/repo"}, prs, append([]step{{"?\r", "PR #41"}, {esc, prs}, {esc, repo}}, roundTrip...), "q"},
		{"PR URL", []string{"pr", "https://bitbucket.test/projects/PRJ/repos/repo/pull-requests/42"}, prs, append([]step{{"?\r", "PR #42"}, {esc, prs}, {esc, repo}}, roundTrip...), "q"},
		{"direct settings", []string{"repo", "settings", "-R", "bitbucket.test/PRJ/repo"}, settings, append([]step{{esc, repo}}, roundTrip...), "q"},
		{"direct repo", []string{"repo", "-R", "bitbucket.test/PRJ/repo"}, repo, roundTrip, "q"},
		{"path", []string{dir}, repo, roundTrip, "q"},
		{"home", nil, home, nil, "q"},
		{"PR quit", []string{"prs", "-R", "bitbucket.test/PRJ/repo"}, prs, nil, "q"},
		{"PR interrupt", []string{"prs", "-R", "bitbucket.test/PRJ/repo"}, prs, nil, "\x03"},
		{"nested PR quit", []string{"repo", "-R", "bitbucket.test/PRJ/repo"}, repo, []step{{"p", prs}}, "q"},
		{"nested settings quit", []string{"repo", "-R", "bitbucket.test/PRJ/repo"}, repo, []step{{"S", settings}}, "q"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			terminal, err := pty.New()
			if err != nil {
				t.Fatal(err)
			}
			defer terminal.Close()
			if err := terminal.Resize(140, 40); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			args, _ := json.Marshal(append([]string{"--config", configPath}, tc.args...))
			process := terminal.CommandContext(ctx, os.Args[0], "-test.run=^TestTUINavigation$")
			process.Env = append(os.Environ(), "BB_TEST_TUI_ARGS="+string(args), "TERM=xterm-256color", "NO_COLOR=1")
			process.Dir = dir
			if err := process.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var processErr error
			go func() {
				processErr = process.Wait()
				close(done)
			}()
			screen := vt10x.New(vt10x.WithSize(140, 40), vt10x.WithWriter(terminal))
			readDone := make(chan struct{})
			go func() {
				defer close(readDone)
				reader := bufio.NewReader(terminal)
				for screen.Parse(reader) == nil {
				}
			}()
			defer func() {
				cancel()
				terminal.Close()
				<-done
				<-readDone
			}()
			waitFor := func(want string) {
				t.Helper()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if strings.Contains(screen.String(), want) {
						return
					}
					select {
					case <-done:
						t.Fatalf("exited before %q: %v\n%s", want, processErr, screen.String())
					default:
					}
					time.Sleep(20 * time.Millisecond)
				}
				t.Fatalf("did not reach %q:\n%s", want, screen.String())
			}
			waitFor(tc.initial)
			for _, step := range tc.steps {
				if _, err := terminal.Write([]byte(step.key)); err != nil {
					t.Fatal(err)
				}
				waitFor(step.want)
			}
			if _, err := terminal.Write([]byte(tc.quit)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
				if processErr != nil {
					t.Fatalf("quit failed: %v\n%s", processErr, screen.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("quit opened another screen instead of exiting:\n%s", screen.String())
			}
		})
	}
}
