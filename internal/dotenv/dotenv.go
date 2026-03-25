// Package dotenv loads a .env file into the process environment.
// Variables already set in the environment are never overwritten, so real
// env vars always take precedence over the file.
package dotenv

import (
	"bufio"
	"os"
	"strings"
)

// Load reads path and sets any unset environment variables it finds.
// If the file does not exist the call is silently ignored.
// Lines starting with # and blank lines are skipped.
func Load(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Strip optional surrounding quotes ("value" or 'value').
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		// Never overwrite a value that was already in the environment.
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
