package common

import (
	"fmt"
)

func ErrorsJoin(mainError error, specificError error) error {
	return fmt.Errorf("%w: %w", mainError, specificError)
}
