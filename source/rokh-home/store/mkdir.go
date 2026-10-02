package store

import (
	"errors"
	"os"
)

// mkdir makes the ledger's folder; a home already there is refused.
func mkdir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrExists
		}
		return err
	}
	return nil
}
