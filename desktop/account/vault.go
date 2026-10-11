package account

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// A separate service prevents account sessions from entering provider environment
// variables, subprocesses, configuration exports or workspace credential files.
type vaultStore struct{}

func (vaultStore) Load() (string, error) {
	v, err := keyring.Get("semantix-account", Origin)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errNotFound
	}
	return v, err
}
func (vaultStore) Save(value string) error { return keyring.Set("semantix-account", Origin, value) }
func (vaultStore) Delete() error {
	err := keyring.Delete("semantix-account", Origin)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
