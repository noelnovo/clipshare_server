package cert

import (
	"fmt"

	"clipshare/src/internal/certs"
)

func runTrust(dir, foreignPath string) error {
	if err := certs.Trust(dir, foreignPath); err != nil {
		return err
	}
	fmt.Printf("trusted CA(s) from %s (restart the daemon to pick up the change)\n", foreignPath)
	return nil
}