package api

import (
	"encoding/json"
	"fmt"
)

// WestpacRequiredApproversSetting is a live legacy response fragment, not the
// hook key and not a writable settings contract. Its count is irrelevant while
// disabled. Write the scalar requiredApprovers field instead.
const WestpacRequiredApproversSetting = "com.atlassian.bitbucket.server.bitbucket-bundled-hooks:requiredApprovers"

// RepoMinimumApprovals reads the effective count. The documented scalar wins
// over plugin metadata, including Westpac's disabled fragment with count:1.
func RepoMinimumApprovals(raw json.RawMessage) (int, error) {
	fields, err := settingObject(raw)
	if err != nil {
		return 0, err
	}
	if scalar, ok := fields["requiredApprovers"]; ok {
		var count int
		if string(scalar) == "null" || json.Unmarshal(scalar, &count) != nil || count < 0 {
			return 0, fmt.Errorf("invalid requiredApprovers scalar")
		}
		return count, nil
	}
	if _, legacy := fields[WestpacRequiredApproversSetting]; legacy {
		return 0, fmt.Errorf("legacy plugin metadata without requiredApprovers scalar; no settings will be written")
	}
	var fragment struct {
		Enabled *bool `json:"enabled"`
		Count   *int  `json:"count"`
	}
	if json.Unmarshal(fields[RequiredApproversSetting], &fragment) != nil || fragment.Enabled == nil || fragment.Count == nil || *fragment.Count < 0 {
		return 0, fmt.Errorf("unrecognized minimum-approvals schema; no settings will be written")
	}
	if !*fragment.Enabled {
		return 0, nil
	}
	return *fragment.Count, nil
}

// RepoMinimumApprovalsPatch adapts a desired count to the fetched schema. When
// requiredApprovers exists (including Westpac), send ONLY that scalar. Never
// submit Westpac's plugin object or count:0. Modern-only servers use their
// documented fragment; disabling retains a valid positive configured count.
func RepoMinimumApprovalsPatch(current json.RawMessage, count int) (json.RawMessage, error) {
	if count < 0 {
		return nil, fmt.Errorf("minimum approvals must be non-negative")
	}
	if _, err := RepoMinimumApprovals(current); err != nil {
		return nil, err
	}
	fields, _ := settingObject(current)
	if _, ok := fields["requiredApprovers"]; ok {
		return json.Marshal(map[string]int{"requiredApprovers": count})
	}
	configured := count
	if configured == 0 {
		var fragment struct {
			Count int `json:"count"`
		}
		_ = json.Unmarshal(fields[RequiredApproversSetting], &fragment)
		configured = max(1, fragment.Count)
	}
	return json.RawMessage(fmt.Sprintf(`{"%s":{"enabled":%t,"count":%d}}`, RequiredApproversSetting, count > 0, configured)), nil
}
