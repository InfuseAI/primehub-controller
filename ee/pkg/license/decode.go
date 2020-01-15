package license

import (
	"encoding/base64"
	"strings"

	"gopkg.in/yaml.v2"
)

func Decode(signedLicense string) (content map[string]string, err error) {
	content = map[string]string{}
	splits := strings.Split(signedLicense, ".")
	decoded, err := base64.StdEncoding.DecodeString(splits[0])
	yaml.Unmarshal(decoded, content)

	return
}
