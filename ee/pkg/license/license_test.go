package license

import (
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestExpiryStatus(t *testing.T) {
	t.Run("Test unexpired license", func(t *testing.T) {
		data := map[string]string{}
		data["started_at"] = time.Now().AddDate(0, 0, -7).Format(TIME_LAYOUT)
		data["expired_at"] = time.Now().AddDate(0, 0, 7).Format(TIME_LAYOUT)
		result := expiryStatus(data)

		assert.Equal(t, result, STATUS_UNEXPIRED)
	})

	t.Run("Test expired license", func(t *testing.T) {
		data := map[string]string{}
		data["started_at"] = time.Now().AddDate(0, 0, -7).Format(TIME_LAYOUT)
		data["expired_at"] = time.Now().AddDate(0, 0, -3).Format(TIME_LAYOUT)
		result := expiryStatus(data)

		assert.Equal(t, result, STATUS_EXPIRED)
	})

	t.Run("Test unexpired future license", func(t *testing.T) {
		// We may install license that not started yet, but this should be considered unexpired
		// eg. started_at in the future, expired_at in the future.
		data := map[string]string{}
		data["started_at"] = time.Now().AddDate(0, 0, 7).Format(TIME_LAYOUT)
		data["expired_at"] = time.Now().AddDate(0, 0, 14).Format(TIME_LAYOUT)
		result := expiryStatus(data)

		assert.Equal(t, result, STATUS_UNEXPIRED)
	})
}
