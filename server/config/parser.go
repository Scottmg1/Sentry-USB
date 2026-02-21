package config

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// SetupConfig holds the parsed configuration variables as key-value pairs.
type SetupConfig map[string]string

// DefaultConfigPath is the standard location for the setup variables file.
const DefaultConfigPath = "/root/sentryusb.conf"

// BootConfigPath is the location on the boot partition.
const BootConfigPath = "/boot/firmware/sentryusb.conf"

var exportRegex = regexp.MustCompile(`^\s*export\s+([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
var commentedExportRegex = regexp.MustCompile(`^\s*#\s*export\s+([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// ParseFile reads a sentryusb.conf file and returns both
// active (exported) and commented-out variables.
func ParseFile(path string) (active SetupConfig, commented SetupConfig, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer f.Close()

	active = make(SetupConfig)
	commented = make(SetupConfig)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if m := exportRegex.FindStringSubmatch(line); m != nil {
			active[m[1]] = unquote(m[2])
		} else if m := commentedExportRegex.FindStringSubmatch(line); m != nil {
			commented[m[1]] = unquote(m[2])
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("error reading config file: %w", err)
	}

	return active, commented, nil
}

// FindConfigPath returns the first existing config file path.
func FindConfigPath() string {
	for _, p := range []string{DefaultConfigPath, BootConfigPath, "/boot/sentryusb.conf"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return DefaultConfigPath
}

// WriteFile writes the configuration back to the file, preserving comments
// and structure as much as possible. Variables in newConfig will be written
// as active exports. Variables not in newConfig that were previously active
// will be commented out.
func WriteFile(path string, newConfig SetupConfig) error {
	// Read the original file
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}

	var lines []string
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	f.Close()

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading config file: %w", err)
	}

	// Process each line
	var output []string
	for _, line := range lines {
		if m := exportRegex.FindStringSubmatch(line); m != nil {
			key := m[1]
			seen[key] = true
			if val, ok := newConfig[key]; ok {
				output = append(output, fmt.Sprintf("export %s=%s", key, quote(val)))
			} else {
				// Comment out variables not in newConfig
				output = append(output, "#"+line)
			}
		} else if m := commentedExportRegex.FindStringSubmatch(line); m != nil {
			key := m[1]
			seen[key] = true
			if val, ok := newConfig[key]; ok {
				output = append(output, fmt.Sprintf("export %s=%s", key, quote(val)))
			} else {
				output = append(output, line)
			}
		} else {
			output = append(output, line)
		}
	}

	// Append any new variables not in the original file
	for key, val := range newConfig {
		if !seen[key] {
			output = append(output, fmt.Sprintf("export %s=%s", key, quote(val)))
		}
	}

	// Write back
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	defer out.Close()

	w := bufio.NewWriter(out)
	for _, line := range output {
		fmt.Fprintln(w, line)
	}
	return w.Flush()
}

// unquote removes surrounding single or double quotes from a value.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	// Handle $'...' syntax
	if strings.HasPrefix(s, "$'") && strings.HasSuffix(s, "'") {
		return s[2 : len(s)-1]
	}
	// Strip inline comments for unquoted values (e.g. "3480 # this number is in seconds")
	for i := 1; i < len(s); i++ {
		if s[i] == '#' && (s[i-1] == ' ' || s[i-1] == '\t') {
			s = strings.TrimSpace(s[:i-1])
			break
		}
	}
	return s
}

// quote wraps a value in single quotes for safe bash export.
func quote(s string) string {
	if s == "" {
		return "''"
	}
	// If value contains no special characters, leave it bare
	if !strings.ContainsAny(s, " \t'\"\\$!#&|;(){}[]<>?*~`") {
		return s
	}
	// Use single quotes; escape any embedded single quotes
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}
