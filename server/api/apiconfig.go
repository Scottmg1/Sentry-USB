package api

import "os"

// Backend service base URLs.
// Override via environment variables for staging/local development.
// Defaults to production values.
var (
	APIBaseURL          = envOrDefault("SENTRY_API_URL", "https://api.sentry-six.com")
	NotificationBaseURL = envOrDefault("SENTRY_NOTIFICATION_URL", "https://notifications.sentry-six.com")
)

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
