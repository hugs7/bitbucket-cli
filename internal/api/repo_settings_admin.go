package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// RepoSettingActions lists implemented operations, not a claim about the user's
// permissions or installed plugins. Token management is deliberately absent.
func RepoSettingActions(resource string) []string {
	switch resource {
	case "details", "default-branch", "pull-requests", "merge-strategies", "hook-settings":
		return []string{"get", "update"}
	case "default-reviewers", "permissions-users", "permissions-groups", "access-keys":
		return []string{"get", "create", "update", "delete"}
	case "branch-restrictions":
		return []string{"get", "create", "delete"} // no documented in-place update
	case "auto-decline":
		return []string{"get", "update", "delete"}
	case "hooks":
		return []string{"get", "enable", "disable"}
	}
	return nil
}

// RepoSettingNeedsID identifies operations addressing a collection member.
// Collection GET accepts an optional ID, except hook-settings, which requires it.
func RepoSettingNeedsID(resource, action string) bool {
	if resource == "hook-settings" {
		return true
	}
	if action != "get" && (resource == "permissions-users" || resource == "permissions-groups") {
		return true
	}
	if action == "get" || action == "create" {
		return false
	}
	switch resource {
	case "default-reviewers", "permissions-users", "permissions-groups", "branch-restrictions", "access-keys", "hooks":
		return true
	}
	return false
}

func adminSetting(resource string) bool {
	switch resource {
	case "details", "default-branch", "permissions-users", "permissions-groups", "branch-restrictions", "access-keys", "hooks", "hook-settings":
		return true
	}
	return false
}

func adminSettingPath(s *serverService, project, slug, resource string) string {
	base := "projects/" + url.PathEscape(project) + "/repos/" + url.PathEscape(slug)
	switch resource {
	case "details":
		return base
	case "default-branch":
		return base + "/default-branch"
	case "permissions-users":
		return base + "/permissions/users"
	case "permissions-groups":
		return base + "/permissions/groups"
	case "branch-restrictions":
		return s.client.HostRoot() + "/rest/branch-permissions/2.0/" + base + "/restrictions"
	case "access-keys":
		return s.client.HostRoot() + "/rest/keys/1.0/" + base + "/ssh"
	default:
		return base + "/settings/hooks"
	}
}

func validateAdminSettingID(resource, id string, required bool) error {
	if required && strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s requires --id (a condition/key ID, username, group name or hook key)", resource)
	}
	if id == "" {
		return nil
	}
	if resource == "details" || resource == "default-branch" {
		return fmt.Errorf("%s does not accept --id", resource)
	}
	if resource == "branch-restrictions" || resource == "access-keys" {
		if err := validateConditionID(id); err != nil {
			return fmt.Errorf("%s requires a non-negative numeric --id", resource)
		}
	}
	if (resource == "hooks" || resource == "hook-settings") && (id == "." || id == ".." || strings.ContainsAny(id, "/\\")) {
		return fmt.Errorf("invalid hook key")
	}
	return nil
}

func readAdminSetting(s *serverService, project, slug, resource, id string) (json.RawMessage, error) {
	if err := validateAdminSettingID(resource, id, resource == "hook-settings"); err != nil {
		return nil, err
	}
	path := adminSettingPath(s, project, slug, resource)
	if id == "" && resource != "details" && resource != "default-branch" {
		values, err := readSettingsCollection(s.client, path)
		if err != nil {
			return nil, err
		}
		return json.Marshal(values)
	}
	if resource == "permissions-users" || resource == "permissions-groups" {
		values, err := readSettingsCollection(s.client, path)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			var entry struct {
				User struct {
					Name string `json:"name"`
				} `json:"user"`
				Group struct {
					Name string `json:"name"`
				} `json:"group"`
				Permission string `json:"permission"`
			}
			if err := json.Unmarshal(value, &entry); err != nil {
				return nil, err
			}
			if (resource == "permissions-users" && entry.User.Name == id) || (resource == "permissions-groups" && entry.Group.Name == id) {
				return json.Marshal(map[string]string{"permission": entry.Permission})
			}
		}
		return nil, fmt.Errorf("no listed repository permission for %q; use create to grant it (inherited access may still apply)", id)
	}
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	if resource == "hook-settings" {
		path += "/settings"
	}
	var raw json.RawMessage
	if err := s.client.getJSON(path, &raw); err != nil {
		return nil, err
	}
	if resource == "details" {
		fields, err := settingObject(raw)
		if err != nil {
			return nil, err
		}
		return json.Marshal(selectSettingFields(fields, "name", "description", "forkable", "public"))
	}
	if resource == "default-branch" {
		fields, err := settingObject(raw)
		if err != nil {
			return nil, err
		}
		return json.Marshal(selectSettingFields(fields, "id"))
	}
	return raw, nil
}

// changeAdminSetting only uses documented core, branch-permissions and SSH APIs:
// https://docs.atlassian.com/bitbucket-server/rest/7.21.0/bitbucket-rest.html
// https://docs.atlassian.com/bitbucket-server/rest/7.21.0/bitbucket-ref-restriction-rest.html
// https://docs.atlassian.com/bitbucket-server/rest/7.21.0/bitbucket-ssh-rest.html
func changeAdminSetting(s *serverService, project, slug, resource, id, action string, patch json.RawMessage) error {
	if !slices.Contains(RepoSettingActions(resource), action) || action == "get" {
		return fmt.Errorf("%s does not support %s; supported operations: %s", resource, action, strings.Join(RepoSettingActions(resource), ", "))
	}
	requiresID := RepoSettingNeedsID(resource, action) || resource == "permissions-users" || resource == "permissions-groups"
	if err := validateAdminSettingID(resource, id, requiresID); err != nil {
		return err
	}
	if action == "create" && id != "" && resource != "permissions-users" && resource != "permissions-groups" {
		return fmt.Errorf("create %s does not accept --id", resource)
	}
	path := adminSettingPath(s, project, slug, resource)
	if resource == "hooks" {
		path += "/" + url.PathEscape(id) + "/enabled"
		if action == "disable" {
			return s.client.deleteJSON(path)
		}
		// No configuration body: enabling must not erase existing settings.
		return s.client.putJSON(path, nil, nil)
	}
	if action == "delete" {
		if strings.HasPrefix(resource, "permissions-") {
			return s.client.deleteJSON(path + "?" + url.Values{"name": {id}}.Encode())
		}
		return s.client.deleteJSON(path + "/" + url.PathEscape(id))
	}
	fields, err := settingObject(patch)
	if err != nil {
		return err
	}
	switch resource {
	case "details", "hook-settings":
		if resource == "details" {
			if err := onlySettingFields(fields, "name", "description", "public", "forkable"); err != nil {
				return err
			}
		} else {
			path += "/" + url.PathEscape(id) + "/settings"
		}
		current, err := readAdminSetting(s, project, slug, resource, id)
		if err != nil {
			return err // never replace a configuration we could not read
		}
		base, err := settingObject(current)
		if err != nil {
			return err
		}
		return s.client.putJSON(path, overlaySetting(base, fields), nil)
	case "default-branch":
		if err := onlySettingFields(fields, "id"); err != nil {
			return err
		}
		branch, err := settingString(fields, "id")
		if err != nil || !strings.HasPrefix(branch, "refs/heads/") || branch == "refs/heads/" {
			return fmt.Errorf("default-branch requires an id like refs/heads/main")
		}
		return s.client.putJSON(path, fields, nil)
	case "permissions-users", "permissions-groups", "access-keys":
		if resource == "access-keys" && action == "create" {
			if err := onlySettingFields(fields, "key", "permission"); err != nil {
				return err
			}
			key, err := settingObject(fields["key"])
			if err != nil {
				return fmt.Errorf("access-keys requires key: {text: public SSH key, label: label}")
			}
			if err := onlySettingFields(key, "text", "label"); err != nil {
				return err
			}
			text, err := settingString(key, "text")
			if err != nil || strings.Contains(text, "PRIVATE KEY") || !(strings.HasPrefix(text, "ssh-") || strings.HasPrefix(text, "ecdsa-") || strings.HasPrefix(text, "sk-")) {
				return fmt.Errorf("supply a public OpenSSH key only; private keys and token generation are unsupported")
			}
		} else if err := onlySettingFields(fields, "permission"); err != nil {
			return err
		}
		permission, err := settingString(fields, "permission")
		allowed := []string{"REPO_READ", "REPO_WRITE", "REPO_ADMIN"}
		if resource == "access-keys" {
			allowed = allowed[:2]
		}
		if err != nil || !slices.Contains(allowed, permission) {
			return fmt.Errorf("permission must be one of %s", strings.Join(allowed, ", "))
		}
		if resource == "access-keys" {
			if action == "create" {
				return s.client.postJSON(path, fields, nil)
			}
			return s.client.putJSON(path+"/"+id+"/permission/"+permission, nil, nil)
		}
		return s.client.putJSON(path+"?"+url.Values{"name": {id}, "permission": {permission}}.Encode(), nil, nil)
	case "branch-restrictions":
		if err := onlySettingFields(fields, "type", "matcher", "users", "groups", "accessKeys"); err != nil {
			return err
		}
		kind, err := settingString(fields, "type")
		if err != nil || !slices.Contains([]string{"read-only", "no-deletes", "fast-forward-only", "pull-request-only"}, kind) {
			return fmt.Errorf("unsupported branch restriction type")
		}
		if _, err := settingObject(fields["matcher"]); err != nil {
			return fmt.Errorf("branch restriction requires a matcher object")
		}
		return s.client.postJSON(path, fields, nil)
	}
	return fmt.Errorf("unsupported settings operation")
}

func selectSettingFields(fields map[string]json.RawMessage, names ...string) map[string]json.RawMessage {
	result := map[string]json.RawMessage{}
	for _, name := range names {
		if value, ok := fields[name]; ok {
			result[name] = value
		}
	}
	return result
}

func onlySettingFields(fields map[string]json.RawMessage, names ...string) error {
	for name := range fields {
		if !slices.Contains(names, name) {
			return fmt.Errorf("unsupported field %q; writable fields: %s", name, strings.Join(names, ", "))
		}
	}
	return nil
}

func settingString(fields map[string]json.RawMessage, name string) (string, error) {
	var value string
	if err := json.Unmarshal(fields[name], &value); err != nil || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", name)
	}
	return value, nil
}
