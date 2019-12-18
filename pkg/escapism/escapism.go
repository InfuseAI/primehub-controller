/*
Full functions, please reference:
https://github.com/minrk/escapism
*/

package escapism

import (
	"encoding/hex"
	"unicode"
)

func escapeChar(c string, escape_char string) string {
	bytechars := []byte(c)
	var safechars string
	safechars += escape_char
	safechars += hex.EncodeToString(bytechars)
	return safechars
}

func Escape(input string) string {
	safestring := ""
	for _, char := range input {
		if !unicode.IsLower(char) && !unicode.IsDigit(char) {
			safestring += escapeChar(string(char), "-")
		} else {
			safestring += string(char)
		}
	}
	return safestring
}
