//go:build !unix

package api

import (
	"fmt"
	"os"
)

func lockOverrideFile(_ *os.File) error {
	return fmt.Errorf("safe merge-check bypass/recovery has no advisory file-lock implementation on this platform")
}
