package serverside

import (
	"context"

	"github.com/centrifugal/centrifuge"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string, data []byte) (*centrifuge.Credentials, []byte, bool, error)
}
