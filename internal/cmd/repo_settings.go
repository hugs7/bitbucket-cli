package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/spf13/cobra"
)

func newRepoSettingsResourceCmd(action string) *cobra.Command {
	var repoFlag, hostFlag, id, file string
	var yes, includeSensitive bool
	var minimum int
	c := &cobra.Command{
		Use:   action + " <resource>",
		Short: strings.ToUpper(action[:1]) + action[1:] + " documented Server/Data Center settings (JSON)",
		Args:  cobra.ExactArgs(1),
		Long: `Manage documented Bitbucket Server/Data Center repository settings.

Resources: details, default-branch, permissions-users, permissions-groups,
branch-restrictions, access-keys, hooks, hook-settings, pull-requests,
merge-strategies, default-reviewers, auto-decline.
get prints JSON; update overlays a JSON object on freshly fetched settings,
preserving omitted fields. Nested objects are merged, arrays are replaced;
an explicit empty object replaces the object (merge-strategies {} restores
inheritance). Bitbucket validates fields and permissions. Cloud is not supported.

details: get/update name, description, public, forkable. Renaming may change the
repository slug; reopen with its new slug afterwards. Moves/deletion unsupported.
default-branch: get/update {"id":"refs/heads/main"}.
permissions-users / permissions-groups: get lists all entries, --id selects a
username/group name; create/update --id NAME --file permission.json grants or
changes {"permission":"REPO_READ|REPO_WRITE|REPO_ADMIN"}. delete revokes the
repository grant only; inherited project/global access may remain.
branch-restrictions: get/create/delete --id ID. Creation needs type, matcher,
and optional users/groups/accessKeys exceptions. No atomic update API: changing
a restriction requires separate explicit create/delete operations.
access-keys: public SSH keys only. get/create/update/delete; update --id ID changes
only {"permission":"REPO_READ|REPO_WRITE"}. Create accepts
{"key":{"text":"ssh-ed25519 ...","label":"CI"},"permission":"REPO_READ"}.
hooks: get metadata; enable/disable --id PLUGIN:HOOK. No settings body is sent.
hook-settings: get --id KEY --include-sensitive explicitly reads plugin config;
update --id KEY --file JSON preserves omitted fields. Plugin schemas vary.
Hook configuration may contain secrets; it is excluded from overview dumps,
and write confirmation does not echo its payload. Protect input/output files.
HTTP tokens and private-key creation are unsupported; use Bitbucket's secure UI.

default-reviewers: get lists all conditions, or use --id for one repository
condition; create requires sourceMatcher, targetMatcher, reviewers (user IDs),
and requiredApprovals. update/delete require --id. Inherited project conditions
are not editable here. Reviewer groups depend on the installed server version.
auto-decline: update enabled/inactivityWeeks; delete restores inherited settings.
pull-requests: --minimum-approvals is the merge check, not a reviewer assignment.
Plugin-specific settings and named required-build policies are not guaranteed.

Examples:
  bb repo settings get pull-requests -R PROJ/repo
  bb repo settings update pull-requests --minimum-approvals 2 -R PROJ/repo
  bb repo settings update pull-requests --file checks.json --yes -R PROJ/repo
  bb repo settings update merge-strategies --file strategies.json -R PROJ/repo
  bb repo settings update default-reviewers --id 4 --file condition.json -R PROJ/repo

Writes require typing yes or --yes. With --file -, use --yes (stdin is JSON).
There is no atomic concurrency check; coordinate with other administrators.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resource := args[0]
			if !slices.Contains(api.RepoSettingActions(resource), action) {
				return fmt.Errorf("%s does not support %s; see --help for supported resources and operations", resource, action)
			}
			if api.RepoSettingNeedsID(resource, action) && strings.TrimSpace(id) == "" {
				return fmt.Errorf("%s %s requires --id", action, resource)
			}
			if action == "get" && resource == "hook-settings" && !includeSensitive {
				return fmt.Errorf("hook configuration may contain secrets; explicitly pass --include-sensitive and protect the output")
			}
			var patch json.RawMessage
			if action == "update" || action == "create" {
				if cmd.Flags().Changed("minimum-approvals") {
					if action != "update" || resource != "pull-requests" || minimum < 0 || file != "" {
						return fmt.Errorf("--minimum-approvals requires update pull-requests, a non-negative count, and no --file")
					}
				} else {
					if file == "" {
						return fmt.Errorf("--file is required (or --minimum-approvals for pull-requests)")
					}
					if file == "-" && !yes {
						return fmt.Errorf("--file - requires --yes; stdin cannot also be used for confirmation")
					}
					var err error
					if file == "-" {
						patch, err = io.ReadAll(cmd.InOrStdin())
					} else {
						patch, err = os.ReadFile(file)
					}
					if err != nil {
						return err
					}
				}
				if !cmd.Flags().Changed("minimum-approvals") {
					var object map[string]json.RawMessage
					if err := json.Unmarshal(patch, &object); err != nil || object == nil {
						return fmt.Errorf("settings input must be a JSON object")
					}
				}
			}
			svc, project, slug, err := resolveContext(repoFlag, hostFlag)
			if err != nil {
				return err
			}
			if action == "get" {
				raw, err := api.ReadRepoSetting(svc, project, slug, resource, id)
				if err != nil {
					return err
				}
				var pretty strings.Builder
				// RawMessage preserves numeric IDs without a float conversion.
				encoder := json.NewEncoder(&pretty)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(raw); err != nil {
					return err
				}
				_, err = fmt.Fprint(cmd.OutOrStdout(), pretty.String())
				return err
			}
			if !api.CanEditRepoSettings(svc) {
				return fmt.Errorf("these settings writes require Bitbucket Server/Data Center")
			}
			if cmd.Flags().Changed("minimum-approvals") {
				current, err := api.ReadRepoSetting(svc, project, slug, "pull-requests", "")
				if err != nil {
					return err
				}
				patch, err = api.RepoMinimumApprovalsPatch(current, minimum)
				if err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "%s %s %s on %s/%s (%s)\n", action, resource, id, project, slug, svc.Host())
			if len(patch) > 0 {
				if resource == "hook-settings" || resource == "access-keys" {
					fmt.Fprintln(cmd.ErrOrStderr(), "Payload hidden: review your input file before confirming.")
				} else {
					fmt.Fprintln(cmd.ErrOrStderr(), string(patch))
				}
			}
			if !yes {
				fmt.Fprint(cmd.ErrOrStderr(), "Apply this change? Type yes: ")
				answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(answer) != "yes" {
					return fmt.Errorf("settings change cancelled; nothing written")
				}
			}
			if err := api.ChangeRepoSetting(svc, project, slug, resource, id, action, patch); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Repository settings updated.")
			return nil
		},
	}
	c.Flags().StringVarP(&repoFlag, "repo", "R", "", "PROJ/repo or host/PROJ/repo")
	c.Flags().StringVar(&hostFlag, "host", "", "configured host")
	c.Flags().StringVar(&id, "id", "", "condition/key ID, username, group name or plugin hook key")
	if action == "get" {
		c.Flags().BoolVar(&includeSensitive, "include-sensitive", false, "allow explicit reading of hook configuration, which may contain secrets")
	}
	if action != "get" {
		c.Flags().BoolVarP(&yes, "yes", "y", false, "confirm the settings write")
	}
	if action == "update" || action == "create" {
		c.Flags().StringVarP(&file, "file", "f", "", "JSON object file, or - for stdin (requires --yes)")
		c.Flags().IntVar(&minimum, "minimum-approvals", 0, "minimum approving reviewers (0 disables the check)")
	}
	return c
}
