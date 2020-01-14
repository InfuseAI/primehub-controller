package license

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestVerify(t *testing.T) {
	t.Run("Test malformed key", func(t *testing.T) {
		signedLicense := "fgrsewgwergtwergt"
		ok, err := Verify(signedLicense)

		assert.Equal(t, ok, false)
		assert.Contains(t, err.Error(), "malformed")
	})

	t.Run("Test decode signature failed", func(t *testing.T) {
		signedLicense := "fgrsewgwergtwergt.grgresgrseghrh"
		ok, err := Verify(signedLicense)

		assert.Equal(t, ok, false)
		assert.Contains(t, err.Error(), "decode license signature failed")
	})

	t.Run("Test verify signature success", func(t *testing.T) {
		signedLicense := "bGljZW5zZWRfdG86ICJEZXZlbG9wZXIiCnN0YXJ0ZWRfYXQ6ICIyMDE5LTEyLTIwVDIxOjA3OjQ1WiIKZXhwaXJlZF9hdDogIjIwMTktMTMtMjBUMjE6MDc6NDVaIgptYXhfZ3JvdXA6IDAK.394c67a71171c2d54e1ea4ee7b449312bc9d997936670fdf5c843e903e57d752f3062d50cd3d2b4291f514ffd2f738a5f2d6423007284c1caca75897e56a5433f34983e73b92202bbea2962e1dc07ef810211dbe929adab71c64d4bf9576e65aaf5172c885bc9d21fc92dbbfc487a42a4f44cf791b0446070b988fa22d4f0b906342406b2d2d1ec56425942e3837627cae22c25e5c30af897372b811dd68702c78c438442da931fa956ec09d117a6a555196fd9d968c3228fb5f6903ca75170fbfefb68e1f6e17f9560a5523c9ce422a1ee2e7d39dcc2f0b03ab45efe4a311755a1d27873ac456515c2218762142713749b6434234e1f0d6ee9d4362f2b6"
		ok, err := Verify(signedLicense)

		assert.Equal(t, false, ok)
		assert.Contains(t, err.Error(), "verification failed")
	})

	t.Run("Test verify signature success", func(t *testing.T) {
		signedLicense := "bGljZW5zZWRfdG86ICJEZXZlbG9wZXIiCnN0YXJ0ZWRfYXQ6ICIyMDE5LTEyLTIwVDIxOjA3OjQ1WiIKZXhwaXJlZF9hdDogIjIwMTktMTMtMjBUMjE6MDc6NDVaIgptYXhfZ3JvdXA6IDAK.394c67a71171c2d54e1ea4ee7b449312bc9d997936670fdf5c843e903e57d752f3062d50cd3d2b4291f514ffd2f738a5f2d6423007284c1caca75897e56a5433f34983e73b92202bbea2962e1dc07ef810211dbe929adab71c64d4bf9576e65aaf5172c885bc9d21fc92dbbfc487a42a4f44cf791b0446070b988fa22d4f0b906342406b2d2d1ec56425942e3837627cae22c25e5c30af897372b811dd68702c78c438442da931fa956ec09d117a6a555196fd9d968c3228fb5f6903ca75170fbfefb68e1f6e17f9560a5523c9ce422a1ee2e7d39dcc2f0b03ab45efe4a311755a1d27873ac456515c2218762142713749b6434234e1f0d6ee9d4362f2b6b629"
		ok, err := Verify(signedLicense)

		assert.Equal(t, true, ok)
		assert.Nil(t, err)
	})
}
