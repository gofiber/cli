package file

import (
	"os"
)

// Check if file exists
func Exist(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}
	return true
}
