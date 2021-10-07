package license

import (
	"errors"
	"fmt"
)

func TypeCheck(content map[string]string, platform_type string) (err error) {
	value, found := content["platform_type"]
	if !found || platform_type == "" {
		return nil
	}
	if platform_type != value {
		format := "invalid platform type: expected \"%s\" but got \"%s\""
		return errors.New(fmt.Sprintf(format, platform_type, value))
	}
	return nil
}
