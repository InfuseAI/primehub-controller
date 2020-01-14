package license

import (
	"encoding/base64"
	"strings"

	"gopkg.in/yaml.v2"
)

func Decode(signedLicense string) (content map[string]string, err error) {
	if _, err := Verify(signedLicense); err != nil {
		return nil, err
	}

	content = make(map[string]string)
	splits := strings.Split(signedLicense, ".")
	decoded, _ := base64.StdEncoding.DecodeString(splits[0])
	yaml.Unmarshal(decoded, content)

	return
}
