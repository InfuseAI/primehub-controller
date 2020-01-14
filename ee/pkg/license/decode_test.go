package license

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestDecode(t *testing.T) {
	t.Run("Test invalid signed license", func(t *testing.T) {
		signedLicense := "JvdXA6IDAK.906c088f8bc5f32f851d4b701cea37480181c6d3093e0a82447947358b33ec4fdbd2bf66431d52ed2019d89b4d9308212c522a760ac8ce8047ab96bd30f0fc467798263be95db7a7691d8071eb5bef036534cf4647ee953e7180f0d77910674621ac90e99665c295581148dd1b58542aa4f075c04ff5a3f2ef597240dabcf5322bdb3c4cec201cc7a0c9c12682ccb7db59cc1f1a1042316fc32082cae0459cee28c18969424f5ccdf50a85383dad03846a9b55aab01a9a918f8bbf28b53d97bdaf2fd891419e91f04d228401b1bb5dc79eb4aa70586fd49c472b90fe38961c0f376fed3bc56eb668f2f8b3e991894b5ecb0c66e027c50b20fad1647b5083c1bc"
		decoded, err := Decode(signedLicense)

		assert.Nil(t, decoded)
		assert.Contains(t, err.Error(), "license verification failed")
	})

	t.Run("Test decode success", func(t *testing.T) {
		/*
		   licensed_to: "Test Case"
		   started_at: "2001-01-01T00:00:00Z"
		   expired_at: "2001-12-31T00:00:00Z"
		   max_group: 0
		*/
		signedLicense := "bGljZW5zZWRfdG86ICJUZXN0IENhc2UiCnN0YXJ0ZWRfYXQ6ICIyMDAxLTAxLTAxVDAwOjAwOjAwWiIKZXhwaXJlZF9hdDogIjIwMDEtMTItMzFUMDA6MDA6MDBaIgptYXhfZ3JvdXA6IDAK.906c088f8bc5f32f851d4b701cea37480181c6d3093e0a82447947358b33ec4fdbd2bf66431d52ed2019d89b4d9308212c522a760ac8ce8047ab96bd30f0fc467798263be95db7a7691d8071eb5bef036534cf4647ee953e7180f0d77910674621ac90e99665c295581148dd1b58542aa4f075c04ff5a3f2ef597240dabcf5322bdb3c4cec201cc7a0c9c12682ccb7db59cc1f1a1042316fc32082cae0459cee28c18969424f5ccdf50a85383dad03846a9b55aab01a9a918f8bbf28b53d97bdaf2fd891419e91f04d228401b1bb5dc79eb4aa70586fd49c472b90fe38961c0f376fed3bc56eb668f2f8b3e991894b5ecb0c66e027c50b20fad1647b5083c1bc"
		decoded, err := Decode(signedLicense)

		assert.Equal(t, decoded["licensed_to"], "Test Case")
		assert.Equal(t, decoded["started_at"], "2001-01-01T00:00:00Z")
		assert.Equal(t, decoded["expired_at"], "2001-12-31T00:00:00Z")
		assert.Equal(t, decoded["max_group"], "0")
		assert.Nil(t, err)
	})
}
