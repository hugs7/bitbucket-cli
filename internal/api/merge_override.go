package api

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
)

// MergeCheckBypassService is an optional capability, not part of Service.
// Implementations must restore settings before returning, even on merge errors.
// Callers must explicitly obtain consent for repository-wide temporary changes.
type MergeCheckBypassService interface {
	MergePRBypassingChecks(project, slug string, id int, strategyID string) error
}

// MergeCheckRecoveryService restores an interrupted bypass without retrying the
// merge or deleting its source branch. Recovery is explicit, never automatic.
type MergeCheckRecoveryService interface {
	RecoverMergeChecks(project, slug string) error
}

const (
	overrideApproversKey     = "com.atlassian.bitbucket.server.bitbucket-bundled-hooks:requiredApprovers"
	overrideApproversHookKey = "com.atlassian.bitbucket.server.bitbucket-bundled-hooks:requiredApproversMergeHook"
)

type overrideHook struct {
	Key      string         `json:"key"`
	State    map[string]any `json:"state"`
	Settings map[string]any `json:"settings"`
}

type overrideJournal struct {
	Version    int              `json:"version"`
	BaseURL    string           `json:"baseURL"`
	Project    string           `json:"project"`
	Slug       string           `json:"slug"`
	PR         int              `json:"pr"`
	Settings   map[string]any   `json:"settings"`
	Conditions []map[string]any `json:"conditions"`
	Hooks      []overrideHook   `json:"hooks"`
}

// MergePRBypassingChecks temporarily disables merge checks at repository scope
// and matching repository-local default-reviewer minima. Project-inherited hooks
// are restored by removing the temporary repository override, never by writing
// project settings. Unknown hook scopes and inherited reviewer rules are refused.
// A durable private journal precedes every remote write; cleanup attempts every
// restoration even after a failed/ambiguous HTTP response. Process termination
// requires RecoverMergeChecks. There is no remote transaction: other merges can
// run during this repository-wide window, so coordinate with other admins.
func (s *serverService) MergePRBypassingChecks(project, slug string, id int, strategyID string) (err error) {
	journalPath, lock, err := s.lockMergeOverride(project, slug)
	if err != nil {
		return err
	}
	defer lock.Close()
	if _, statErr := os.Lstat(journalPath); !os.IsNotExist(statErr) {
		return fmt.Errorf("unresolved merge-check recovery journal %s; run %s before another bypass: %v", journalPath, s.overrideRecoveryCommand(project, slug), statErr)
	}
	j, err := s.snapshotMergeOverride(project, slug, id)
	if err != nil {
		return fmt.Errorf("merge-check bypass preflight (no settings changed): %w", err)
	}
	if err := writeOverrideJournal(journalPath, j); err != nil {
		return fmt.Errorf("persist recovery journal (no settings changed): %w", err)
	}
	merged := false
	defer func() {
		restoreErr := s.restoreMergeOverride(j)
		if restoreErr == nil {
			restoreErr = removeOverrideJournal(journalPath)
		}
		if restoreErr != nil {
			state := "merge not confirmed; inspect PR state before retrying"
			if merged {
				state = "PR merged"
			}
			err = errors.Join(err, fmt.Errorf("%s; settings restoration failed or uncertain: %w; recovery journal: %s; run %s before any further bypass", state, restoreErr, journalPath, s.overrideRecoveryCommand(project, slug)))
		}
	}()
	if err := s.disableMergeOverride(j); err != nil {
		return fmt.Errorf("disable merge checks (merge not attempted): %w", err)
	}
	if err := s.MergePR(project, slug, id, strategyID); err != nil {
		return fmt.Errorf("bypass merge failed or outcome uncertain; inspect PR #%d before retrying: %w", id, err)
	}
	merged = true
	return nil
}

// RecoverMergeChecks reapplies saved settings and verifies them, retaining the
// journal on any error. It does not merge, retarget, or delete branches. A local
// advisory lock rejects recovery while another local bypass is still running.
// Recovery deliberately restores the saved baseline; coordinate external admin
// changes first because Bitbucket does not provide conditional settings writes.
func (s *serverService) RecoverMergeChecks(project, slug string) error {
	journalPath, lock, err := s.lockMergeOverride(project, slug)
	if err != nil {
		return err
	}
	defer lock.Close()
	j, err := readOverrideJournal(journalPath)
	if err != nil {
		return err
	}
	if j.Version != 1 || j.BaseURL != s.client.baseURL() || j.Project != project || j.Slug != slug || j.Settings == nil {
		return fmt.Errorf("invalid or mismatched recovery journal %s; no settings changed", journalPath)
	}
	if err := s.restoreMergeOverride(j); err != nil {
		return fmt.Errorf("settings restoration failed or uncertain: %w; retained recovery journal %s; retry %s after addressing the failure", err, journalPath, s.overrideRecoveryCommand(project, slug))
	}
	return removeOverrideJournal(journalPath)
}

func (s *serverService) overrideRepoPath(project, slug string) string {
	return s.client.HostRoot() + "/rest/api/latest/projects/" + url.PathEscape(project) + "/repos/" + url.PathEscape(slug)
}

func (s *serverService) overrideReviewerPath(project, slug string) string {
	return s.client.HostRoot() + "/rest/default-reviewers/1.0/projects/" + url.PathEscape(project) + "/repos/" + url.PathEscape(slug)
}

func (s *serverService) snapshotMergeOverride(project, slug string, id int) (*overrideJournal, error) {
	base := s.overrideRepoPath(project, slug)
	// This endpoint requires REPO_ADMIN, including effective group/project/global
	// admin grants. Do not infer admin status from a configured username.
	var permissions any
	if err := s.client.getJSON(base+"/permissions/users?limit=1", &permissions); err != nil {
		return nil, fmt.Errorf("repository admin permission required: %w", err)
	}
	pr, err := s.GetPR(project, slug, id)
	if err != nil {
		return nil, err
	}
	if pr.State != "OPEN" || pr.SourceRef == "" || pr.TargetRef == "" {
		return nil, fmt.Errorf("PR must be open with known source and target branches")
	}
	j := &overrideJournal{Version: 1, BaseURL: s.client.baseURL(), Project: project, Slug: slug, PR: id}
	if err := s.client.getJSON(base+"/settings/pull-requests", &j.Settings); err != nil {
		return nil, err
	}
	count, ok := overrideCount(j.Settings["requiredApprovers"])
	plugin, pluginOK := j.Settings[overrideApproversKey].(map[string]any)
	pluginCount, countOK := overrideCount(plugin["count"])
	enabled, enabledOK := plugin["enable"].(bool)
	if !ok || !pluginOK || !countOK || !enabledOK || (count > 0 && (!enabled || pluginCount != count)) || (count == 0 && enabled) {
		return nil, fmt.Errorf("unsupported/non-restorable required-approver settings (expected Westpac requiredApprovers and enable/count fields)")
	}
	hooks, err := s.overrideCollection(base + "/settings/hooks")
	if err != nil {
		return nil, err
	}
	approversRestorable := count == 0
	for _, state := range hooks {
		details, _ := state["details"].(map[string]any)
		if details["type"] != "PRE_PULL_REQUEST_MERGE" {
			continue
		}
		enabled, ok := state["enabled"].(bool)
		if !ok {
			return nil, fmt.Errorf("merge hook has unknown enabled state")
		}
		if !enabled {
			continue
		}
		key, _ := details["key"].(string)
		configured, configuredOK := state["configured"].(bool)
		scope := overrideScope(state)
		if key == "" || (scope != "REPOSITORY" && scope != "PROJECT") || !configuredOK {
			return nil, fmt.Errorf("refusing unknown-scope or non-restorable merge hook %q", key)
		}
		hook := overrideHook{Key: key, State: state}
		if err := s.client.getJSON(base+"/settings/hooks/"+url.PathEscape(key)+"/settings", &hook.Settings); err != nil {
			return nil, fmt.Errorf("snapshot hook %s settings: %w", key, err)
		}
		if hook.Settings == nil || (!configured && len(hook.Settings) != 0) {
			return nil, fmt.Errorf("non-restorable configuration for merge hook %s", key)
		}
		if key == overrideApproversHookKey || key == overrideApproversKey || key == "com.atlassian.bitbucket.server.bitbucket-bundled-hooks:required-approvers-merge-check" {
			approversRestorable = true
		}
		j.Hooks = append(j.Hooks, hook)
	}
	if !approversRestorable {
		return nil, fmt.Errorf("cannot identify a restorable required-approver merge check; refusing bypass")
	}
	conditions, err := s.overrideCollection(s.overrideReviewerPath(project, slug) + "/conditions")
	if err != nil {
		return nil, err
	}
	for _, condition := range conditions {
		count, ok := overrideCount(condition["requiredApprovals"])
		if !ok {
			return nil, fmt.Errorf("unknown default-reviewer minimum")
		}
		if count == 0 {
			continue
		}
		match, err := overrideConditionMatches(condition, pr.SourceRef, pr.TargetRef)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}
		id, validID := overrideCount(condition["id"])
		_, reviewersOK := condition["reviewers"].([]any)
		if !overrideLocal(condition) || !validID || id == 0 || !reviewersOK {
			return nil, fmt.Errorf("refusing inherited or non-restorable default-reviewer condition")
		}
		j.Conditions = append(j.Conditions, condition)
	}
	return j, nil
}

func (s *serverService) disableMergeOverride(j *overrideJournal) error {
	base := s.overrideRepoPath(j.Project, j.Slug)
	for _, c := range j.Conditions {
		if err := s.client.putJSON(s.overrideConditionPath(j, c), overrideConditionBody(c, 0), nil); err != nil {
			return err
		}
	}
	if j.Settings["requiredApprovers"] != float64(0) {
		if err := s.client.postJSON(base+"/settings/pull-requests", map[string]any{"requiredApprovers": 0}, nil); err != nil {
			return err
		}
	}
	for _, hook := range j.Hooks {
		if err := s.client.deleteJSON(base + "/settings/hooks/" + url.PathEscape(hook.Key) + "/enabled"); err != nil {
			return err
		}
	}
	return s.verifyMergeOverride(j, false)
}

func (s *serverService) restoreMergeOverride(j *overrideJournal) error {
	base := s.overrideRepoPath(j.Project, j.Slug)
	var failures []error
	// The scalar endpoint may create a repository hook override. Restore it
	// before resetting hook inheritance so it cannot recreate a local override
	// after a project-inherited required-approvers hook has been restored.
	if j.Settings["requiredApprovers"] != float64(0) {
		if err := s.client.postJSON(base+"/settings/pull-requests", map[string]any{"requiredApprovers": j.Settings["requiredApprovers"]}, nil); err != nil {
			failures = append(failures, fmt.Errorf("restore required approvers: %w", err))
		}
	}
	// Attempt every restoration, not just the first, including writes whose
	// responses failed: a server may have applied a timed-out request.
	for i := len(j.Hooks) - 1; i >= 0; i-- {
		hook := j.Hooks[i]
		path := base + "/settings/hooks/" + url.PathEscape(hook.Key)
		if overrideScope(hook.State) == "PROJECT" {
			// DELETE the repository configuration, not /enabled: the latter
			// would retain a disabled local override instead of inheritance.
			if err := s.client.deleteJSON(path); err != nil {
				failures = append(failures, fmt.Errorf("restore hook %s project inheritance: %w", hook.Key, err))
			}
			continue
		}
		var body any
		if hook.State["configured"] == true {
			body = hook.Settings
		}
		if err := s.client.putJSON(path+"/enabled", body, nil); err != nil {
			failures = append(failures, fmt.Errorf("restore hook %s: %w", hook.Key, err))
		}
	}
	for i := len(j.Conditions) - 1; i >= 0; i-- {
		c := j.Conditions[i]
		if err := s.client.putJSON(s.overrideConditionPath(j, c), overrideConditionBody(c, c["requiredApprovals"]), nil); err != nil {
			failures = append(failures, fmt.Errorf("restore default-reviewer condition: %w", err))
		}
	}
	if err := s.verifyMergeOverride(j, true); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (s *serverService) verifyMergeOverride(j *overrideJournal, restored bool) error {
	base := s.overrideRepoPath(j.Project, j.Slug)
	var failures []error
	var settings map[string]any
	if err := s.client.getJSON(base+"/settings/pull-requests", &settings); err != nil {
		failures = append(failures, err)
	} else if restored {
		if !reflect.DeepEqual(settings, j.Settings) {
			failures = append(failures, fmt.Errorf("pull-request settings do not match original snapshot"))
		}
	} else {
		plugin, _ := settings[overrideApproversKey].(map[string]any)
		if settings["requiredApprovers"] != float64(0) || plugin["enable"] != false {
			failures = append(failures, fmt.Errorf("required approvers were not disabled"))
		}
	}
	for _, hook := range j.Hooks {
		path := base + "/settings/hooks/" + url.PathEscape(hook.Key)
		var state, settings map[string]any
		if err := s.client.getJSON(path, &state); err != nil {
			failures = append(failures, err)
		} else if (restored && !reflect.DeepEqual(state, hook.State)) || (!restored && (state["enabled"] != false || !overrideLocal(state))) {
			failures = append(failures, fmt.Errorf("hook %s state verification failed", hook.Key))
		}
		if restored {
			if err := s.client.getJSON(path+"/settings", &settings); err != nil {
				failures = append(failures, err)
			} else if !reflect.DeepEqual(settings, hook.Settings) {
				failures = append(failures, fmt.Errorf("hook %s settings do not match original snapshot", hook.Key))
			}
		}
	}
	conditions, err := s.overrideCollection(s.overrideReviewerPath(j.Project, j.Slug) + "/conditions")
	if err != nil {
		failures = append(failures, err)
	} else {
		for _, original := range j.Conditions {
			found := false
			for _, current := range conditions {
				if original["id"] != current["id"] {
					continue
				}
				expected := original
				if !restored {
					expected = overrideCopy(original)
					expected["requiredApprovals"] = float64(0)
				}
				found = reflect.DeepEqual(current, expected)
			}
			if !found {
				failures = append(failures, fmt.Errorf("default-reviewer condition %v verification failed", original["id"]))
			}
		}
	}
	return errors.Join(failures...)
}
