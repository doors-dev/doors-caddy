package common

import (
	"fmt"
	"net/http"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
)

func ErrorsJoin(mainError error, specificError error) error {
	return fmt.Errorf("%w: %w", mainError, specificError)
}
