package monitoring

import (
	"errors"
	"io/ioutil"
	"strconv"
	"strings"
)

func ReadNumber(filename string) (int64, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return 0, err
	}

	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, err
	}

	return value, nil
}

func ReadTotalInactiveFile(filename string) (int64, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return 0, err
	}

	content := string(data)
	attribute := "total_inactive_file"
	if index := strings.Index(content, attribute); index >= 0 {
		content = content[index:]
	} else {
		return 0, errors.New("cannot find total_inactive_file attribute")
	}

	if index := strings.Index(content, "\n"); index >= 0 {
		content = content[:index]
	}
	if index := strings.Index(content, " "); index >= 0 {
		content = content[index:]
	}

	value, err := strconv.ParseInt(strings.TrimSpace(content), 10, 64)
	if err != nil {
		return 0, err
	}
	return value, nil

}
