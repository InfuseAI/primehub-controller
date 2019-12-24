/*
Full functions, please reference:
https://github.com/minrk/escapism
*/

package escapism

import (
	"encoding/hex"
	"log"
	"unicode"
)

func escapeChar(c string, escape_char string) string {
	bytechars := []byte(c)
	var safechars string
	for _, bytechar := range bytechars {
		safechars += escape_char
		var temp = []byte{bytechar}
		safechars += hex.EncodeToString(temp)
	}
	return safechars
}

func EscapeToDNSLabel(input string) string {
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

func UnescapeDNSLabel(input string) string {
	var bytestring []byte

	total_len := len(input)
	for index := 0; index < total_len; index++ {
		char := input[index]
		if string(char) == "-" {
			temp, err := hex.DecodeString(input[index+1 : index+3])
			if err != nil {
				log.Print(err)
				return ""
			}
			bytestring = append(bytestring, temp...)
			index = index + 2
		} else {
			bytestring = append(bytestring, char)
		}
	}
	return string(bytestring)
}

func EscapeToPrimehubLabel(input string) string {
	return "escaped-" + EscapeToDNSLabel(input)
}

func UnescapePrimehubLabel(input string) string {
	return UnescapeDNSLabel(input[8:])
}
