package api

import (
	"os"

	"github.com/Scottmg1/Sentry-USB/server/config"
)

// Backend service base URLs.
// Override via environment variables or sentryusb.conf.
// Defaults to production values.
var (
	APIBaseURL          = configOrDefault("SENTRY_API_URL", "https://api.sentry-six.com")
	NotificationBaseURL = configOrDefault("SENTRY_NOTIFICATION_URL", "https://notifications.sentry-six.com")
)

// configOrDefault checks the environment variable first, then sentryusb.conf, then defaults.
// This allows the systemd service to pick up SENTRY_NOTIFICATION_URL (and other backend URLs)
// from the user's config file without needing EnvironmentFile in the unit.
func configOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if active, _, err := config.ParseFile(config.FindConfigPath()); err == nil {
		if v, ok := active[key]; ok && v != "" {
			return v
		}
	}
	return fallback
}
