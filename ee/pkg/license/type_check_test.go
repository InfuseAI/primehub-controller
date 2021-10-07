package license

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestTypeCheck(t *testing.T) {
	t.Run("Test platform type equals the applied one", func(t *testing.T) {
		content := make(map[string]string)
		content["platform_type"] = "enterprise"
		err := TypeCheck(content, "enterprise")

		assert.Nil(t, err)
	})

	t.Run("Test platform type does not equal the applied one", func(t *testing.T) {
		content := make(map[string]string)
		content["platform_type"] = "deploy"
		err := TypeCheck(content, "enterprise")

		assert.Contains(t, err.Error(), "invalid platform type")
	})

	t.Run("Test platform type is empty", func(t *testing.T) {
		content := make(map[string]string)
		content["platform_type"] = "enterprise"
		err := TypeCheck(content, "")

		assert.Nil(t, err)
	})

	t.Run("Test the applied license has no platform type", func(t *testing.T) {
		content := make(map[string]string)
		err := TypeCheck(content, "enterprise")

		assert.Nil(t, err)
	})
}
