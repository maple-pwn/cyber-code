// Package credential provides private, transactional persistence for secrets.
package credential

import (
	"context"
	"errors"
)

// ErrNotFound indicates that no credential has been stored yet.
var ErrNotFound = errors.New("credential not found")

// Store persists opaque credential data. Implementations must never log it.
type Store interface {
	Load(context.Context) ([]byte, error)
	Save(context.Context, []byte) error
}
