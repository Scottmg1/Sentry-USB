# Configure Notifications for Archive

Get push notifications when Sentry USB finishes archiving clips, encounters errors, or performs other important actions. You can enable any combination of the 11 supported providers.

## Setup Wizard (Recommended)

The easiest way to configure notifications is through the web UI:

1. Open **http://sentryusb.local** in your browser
2. Go to **Settings** → **Open Wizard**
3. Navigate to the **Notifications** step
4. Enable one or more providers and fill in the required fields
5. Optionally set a **Notification Title** (defaults to "Sentry USB")
6. Continue through the remaining wizard steps and click **Apply & Run Setup**

The wizard supports all providers listed below. The rest of this document explains how to obtain the API keys and tokens needed for each provider.

---

## Provider Reference

### Pushover

Free for up to 7,500 messages/month. The [iOS](https://pushover.net/clients/ios)/[Android](https://pushover.net/clients/android) apps have a one-time cost after a free trial.

1. Create a free account at [pushover.net](https://pushover.net) and install the mobile app
2. On the Pushover dashboard, copy your **User Key**
3. [Create a new Application](https://pushover.net/apps/build) and copy the **Application Key**

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| User Key | `PUSHOVER_USER_KEY` | Your Pushover user key |
| App Key | `PUSHOVER_APP_KEY` | Your application's API key |

### Gotify

Self-hosted notification service. Android client available on [Google Play](https://play.google.com/store/apps/details?id=com.github.gotify), [F-Droid](https://f-droid.org/de/packages/com.github.gotify/), or [APK](https://github.com/gotify/android/releases/latest).

1. Install a Gotify server ([instructions](https://gotify.net/docs/install))
2. [Create a new Application](https://gotify.net/docs/pushmsg) and copy the app token

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| Domain | `GOTIFY_DOMAIN` | Your Gotify server URL (e.g., `https://gotify.example.com`) |
| App Token | `GOTIFY_APP_TOKEN` | Application token from Gotify |
| Priority | `GOTIFY_PRIORITY` | Message priority (default: `5`) |

### Discord

Requires a Discord server with permission to create webhooks.

1. Open Discord → Server Settings (or channel settings) → **Integrations** → **Webhooks** → **New Webhook**
2. Name the webhook, choose a channel, and click **Copy Webhook URL**

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| Webhook URL | `DISCORD_WEBHOOK_URL` | The full webhook URL from Discord |

### Telegram

Free. Requires the [Telegram app](https://telegram.org/apps) on your device.

1. Download and sign up for [Telegram](https://telegram.org/apps)
2. Message [@userinfobot](https://telegram.me/userinfobot) to get your **Chat ID**
3. Create a bot via [@BotFather](https://core.telegram.org/bots#botfather) to get your **Bot Token**
4. Make sure the bot token includes the `bot` prefix (e.g., `bot123456789:abc...`)

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| Chat ID | `TELEGRAM_CHAT_ID` | Your numeric Telegram user ID |
| Bot Token | `TELEGRAM_BOT_TOKEN` | Bot token with `bot` prefix |

### IFTTT

Requires an IFTTT account and the [IFTTT app](https://ifttt.com). Webhooks are part of the [pro tier](https://ifttt.com/plans).

1. Connect the [Webhooks service](https://ifttt.com/maker_webhooks)
2. Create an applet: Webhooks trigger → Notifications action
3. Set a unique **Event Name** and note it down
4. Go to [Webhooks Documentation](https://ifttt.com/maker_webhooks) and note the **Key**

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| Event Name | `IFTTT_EVENT_NAME` | The event name from your applet |
| Key | `IFTTT_KEY` | Your Webhooks service key |

### Slack

Send notifications to a Slack channel via incoming webhook.

1. Go to [api.slack.com](https://api.slack.com) → **Create a custom app**
2. Under **Incoming Webhooks**, toggle on and click **Add Webhook to Workspace**
3. Copy the webhook URL

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| Webhook URL | `SLACK_WEBHOOK_URL` | The full Slack webhook URL |

### Signal

Requires a [signal-cli REST API](https://github.com/bbernhard/signal-cli-rest-api) instance running on your network.

1. Set up signal-cli REST API on a server
2. Register or link a phone number with signal-cli

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| Signal CLI URL | `SIGNAL_URL` | URL of the signal-cli REST API (e.g., `http://localhost:8080`) |
| From Number | `SIGNAL_FROM_NUM` | Sender phone number (e.g., `+1234567890`) |
| To Number | `SIGNAL_TO_NUM` | Recipient phone number |

### Matrix

Federated messaging via [Matrix.org](https://matrix.org) or a self-hosted homeserver.

1. Create a bot account on [matrix.org](https://matrix.org) or your homeserver
2. Create or join a room, then find the **Internal Room ID** in room settings

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| Server URL | `MATRIX_SERVER_URL` | Homeserver URL (e.g., `https://matrix.org`) |
| Username | `MATRIX_USERNAME` | Bot username |
| Password | `MATRIX_PASSWORD` | Bot password |
| Room ID | `MATRIX_ROOM` | Target room ID (e.g., `!roomid:matrix.org`) |

### AWS SNS

Amazon Simple Notification Service. Free tier available.

1. Create an [AWS account](https://aws.amazon.com/)
2. Create an IAM user with SNS permissions
3. Create an SNS topic and subscribe an endpoint (email, SMS, etc.)

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| Region | `AWS_REGION` | AWS region (e.g., `us-east-1`) |
| Access Key ID | `AWS_ACCESS_KEY_ID` | IAM access key |
| Secret Key | `AWS_SECRET_ACCESS_KEY` | IAM secret key |
| Topic ARN | `AWS_SNS_TOPIC_ARN` | Full ARN of the SNS topic |

### Webhook

Generic webhook for integration with Home Assistant, Node-RED, or any HTTP endpoint.

1. Set up a webhook URL on your automation platform

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| Webhook URL | `WEBHOOK_URL` | Full URL to POST notifications to |

### ntfy

Free, open-source pub-sub notification service. Works with [ntfy.sh](https://ntfy.sh) (hosted) or a self-hosted instance. iOS/Android apps available.

1. Subscribe to a topic at [ntfy.sh](https://ntfy.sh) (e.g., `https://ntfy.sh/your-unique-topic`)
2. Or self-host ntfy and use your own server URL
3. If your topic is access-controlled, generate an **Access Token** in ntfy's account settings

| Wizard Field | Config Variable | Description |
|-------------|----------------|-------------|
| URL & Topic | `NTFY_URL` | Full URL including topic (e.g., `https://ntfy.sh/yourtopic`) |
| Access Token | `NTFY_TOKEN` | Optional auth token for protected topics |
| Priority | `NTFY_PRIORITY` | Message priority 1–5 (default: `3`) |

---

## Manual Configuration (SSH)

For advanced users, notifications can also be configured by editing `/root/sentryusb.conf` directly. Set the `*_ENABLED` variable to `true` and provide the required fields for each provider. Example:

```bash
export PUSHOVER_ENABLED=true
export PUSHOVER_USER_KEY=your_key
export PUSHOVER_APP_KEY=your_app_key
```

Then run `/root/bin/setup-sentryusb` to apply.
