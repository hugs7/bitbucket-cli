package reposettings

func settingPanelHelp(title string) string {
	switch title {
	case "Default reviewers":
		return "e edit JSON · a add · d delete (by condition ID)"
	case "Repository permissions", "Group permissions":
		return "a grant · e change · d revoke (username/group name)\nRepository grants only; inherited project/global access may remain."
	case "Branch permissions":
		return "a add · d delete (restriction ID)\nNo in-place update API; use separate explicit create/delete operations."
	case "Access keys":
		return "a register PUBLIC SSH key · e change permission · d revoke\nREPO_READ/REPO_WRITE only. No private keys or token generation."
	case "Hooks":
		return "e edit plugin JSON · a enable · d disable (plugin:hook key)\nMetadata only here. Explicit config editing may reveal secrets."
	case "Auto-decline":
		return "e edit JSON · d restore inheritance"
	case "Default branch":
		return "e edit JSON: id must be refs/heads/<branch>"
	default:
		return "e edit JSON"
	}
}
