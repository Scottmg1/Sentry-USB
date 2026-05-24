# Privacy

This page documents every outbound data flow from a Sentry USB device,
the legal basis it relies on under GDPR, how long the data is retained,
and how to disable it. If anything you observe on the wire doesn't
match what's listed here, it's a bug — please open an issue.

## Summary

By default, Sentry USB sends **no device identifier** to our servers.
The "opt-in for analytics" toggle in the setup wizard and in
`Settings → Privacy` is the only switch that controls whether a
device-derived identifier ever leaves your Pi.

## Per-flow disclosure

### 1. Daily update check

- **Endpoint:** `POST https://api.sentry-six.com/sentryusb/telemetry`
- **Sent:** `current_version`, `arch`, `model`, `update_available` flag,
  `new_version` (when relevant)
- **Identifier:** None by default. **If you have opted in** to the
  analytics toggle, a one-way salted SHA-256 of your board's serial
  number is included as `fingerprint`.
- **Purpose:** Detect vulnerable builds, ship compatible binaries,
  and (for opted-in devices) count unique installs without double-
  counting reinstalls.
- **Legal basis:** Legitimate interest under Art. 6(1)(f) for the
  default (no fingerprint) version — Recital 49 explicitly recognizes
  security as a legitimate-interest purpose. For the opted-in
  fingerprinted variant, consent under Art. 6(1)(a).
- **Retention:** Opted-in rows kept until you toggle off or the row
  is purged manually. Non-fingerprinted calls are not stored — only
  rate-limit counters survive briefly in RAM.
- **How to disable:** `Settings → Privacy → Help us count new installs
  → No thanks`. The toggle takes effect immediately.

### 2. Anonymous install beacon

- **Endpoint:** `POST https://api.sentry-six.com/sentryusb/install-beacon`
- **Sent:** **Nothing.** Empty body, no headers beyond standard HTTP.
- **Identifier:** None. The server only increments a daily counter.
- **Purpose:** Tell us gross install volume independent of the opt-in
  cohort — i.e. so we can see if a release attracted new installs at
  all without knowing anything about anyone.
- **Legal basis:** Not personal data, so GDPR doesn't apply. (Your IP
  is briefly seen by the rate-limiter but isn't stored or logged
  beyond the in-memory rolling window.)
- **Retention:** Daily counts are kept indefinitely as aggregate
  numbers. No per-user data exists to retain.
- **How to disable:** Fires exactly once per install (gated by a
  `/mutable/.beaconed` marker). To suppress entirely, create that file
  before first boot: `sudo touch /mutable/.beaconed`. Network-block
  `api.sentry-six.com` if you want to be sure.

### 3. Wraps / lock chime submissions

- **Endpoint:** `POST https://api.sentry-six.com/wraps/upload`,
  `POST https://api.sentry-six.com/lockchime/upload`
- **Sent:** The file you uploaded, name, model (for wraps), and your
  IP address (briefly, for rate-limiting and abuse moderation).
- **Identifier:** **No device fingerprint.** Older versions sent an
  `X-Fingerprint` header — that was removed. Abuse handling now goes
  through the Discord moderation queue plus per-IP rate limits.
- **Purpose:** Sharing your contribution with the community.
- **Legal basis:** Contractual necessity under Art. 6(1)(b) — you
  triggered the upload, so processing the upload is intrinsic to
  the service you requested.
- **Retention:** The uploaded file is retained as long as it's listed
  in the community library. Your IP is retained in the row for
  abuse-investigation purposes only; not exposed to the public
  library view.
- **How to disable:** Don't submit. Browsing/downloading the library
  is anonymous (no headers needed) and rate-limited by IP only.

### 4. Wraps / lock chime downloads

- **Endpoint:** `GET https://api.sentry-six.com/wraps/download/<code>`,
  `GET https://api.sentry-six.com/lockchime/download/<code>`
- **Sent:** Standard HTTP request. No custom identifying headers.
- **Identifier:** None.
- **Purpose:** Fetch the requested asset.
- **Legal basis:** Contractual necessity — you asked for the file.
- **Retention:** A unique-download counter at the IP level was
  previously kept; with `X-Fingerprint` removed it's no longer
  meaningful and is effectively dormant.
- **How to disable:** Don't download.

### 5. iOS push notification pairing (opt-in)

- **Endpoint:** `POST https://api.sentry-six.com/register-code`
- **Sent:** A `device_id` (random UUID generated on this Pi),
  `device_secret`, your chosen pairing code, and your Pi's hostname.
- **Identifier:** The `device_id` — but it's a random value created
  locally on first run, **not** derived from your hardware. Resetting
  it generates a new one.
- **Purpose:** Routing push notifications from your Pi to your phone.
- **Legal basis:** Consent — you actively enabled this feature.
- **Retention:** Kept until you unpair the device.
- **How to disable:** Don't pair, or unpair in the iOS app + delete
  the credentials on the Pi.

## Things Sentry USB does **not** do

- Send a hardware fingerprint without explicit opt-in.
- Phone home on every boot.
- Send "diagnostics" or "crash reports" in the background. If a crash
  reporter is ever added, it will be its own opt-in.
- Bundle multiple consents under one button. Each opt-in is a separate
  affirmative action.
- Use pre-ticked checkboxes — explicit click required.

## Source code references

If you want to verify any of the above against the source:

- Update-check telemetry: `server/api/update.go` → `doSendTelemetry()`.
  Look for the `analyticsOptIn()` read and confirm the `fingerprint`
  key is only inserted when that pref is `"true"`.
- Install beacon: same file → `SendInstallBeacon()`. The POST is
  bodyless.
- Wraps/chimes header forwarding: `server/api/community_wraps.go` and
  `server/api/lock_chime.go`. Search for `X-Fingerprint` — there
  should be zero call sites that set it, only the "deliberately
  omitted" comments.
- Notification pairing: `server/api/notifications.go` →
  `registerCodeWithBackend()`. Confirm the request body has no
  `fingerprint` field.

## Reporting a privacy bug

Open an issue at
[github.com/Scottmg1/Sentry-USB/issues](https://github.com/Scottmg1/Sentry-USB/issues)
or email `privacy@sentry-six.com`. If the bug is "the client sent X
even though the docs said it wouldn't" please include a `tcpdump` or
the relevant journalctl line so we can fix it.
