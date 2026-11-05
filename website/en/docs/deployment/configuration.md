---
title: Configuration reference
description: "HyperCDR environment variables, captcha modes, Community edition registration settings, and platform access address changes."
---

# Configuration reference

The root `env.example`, `docker-compose.yml` and the install scripts are authoritative. The `.env` in the install directory contains real secrets and **must not be committed or made public**. The source examples are not a runnable installation configuration.

## Core environment variables {#env}

| Configuration | Current purpose |
| --- | --- |
| `HCDR_BASE_URL` | External access address of the platform |
| `HCDR_PUBLIC_BASE_URL` | Public address at install time; after installation the platform settings in the database are authoritative |
| `HCDR_HTTPS_PORT` | Edge port, default 12443 |
| `HCDR_WEBSITE_PORT` | Compatibility setting for the legacy product portal, default 12445; current source no longer starts the legacy portal automatically, and this setting does not configure the standalone documentation site |
| `HCDR_AUTH_CHALLENGE_MODE` | `captcha` (also written `image` in install parameters) for the image captcha, or `turnstile` for Cloudflare |
| `HCDR_TURNSTILE_SITE_KEY` / `HCDR_TURNSTILE_SECRET_KEY` | Turnstile browser and server credentials |
| `HCDR_TURNSTILE_VERIFY_URL` | Server-side verification address; an empty value uses the backend default |
| `HCDR_DATABASE_URL` | PostgreSQL connection address |
| `HCDR_SECRET_KEY` | Key that protects platform secret data; keep it when backing up and migrating |
| `HCDR_RELEASE_MANIFEST_PATH` | Path of the current release manifest inside the container |
| `HCDR_IMAGE_REGISTRY` | Component image registry |
| `HCDR_TLS_CERT_FILE` / `HCDR_TLS_KEY_FILE` | TLS files on the host |
| `HCDR_RELEASE_TOKEN` | Access credential for the upgrade runner |
| `HCDR_REGISTRATION_EXECUTOR_TOKEN` | Internal registration executor credential |

## Captcha modes {#captcha}

When Compose does not set a captcha mode, the default is `turnstile`. The source `env.example` shows `captcha`, and a release package may carry a captcha setting injected by CI. **The `.env` after installation is authoritative**; do not assume every installation uses the same mode.

Turnstile requires that the browser can reach Cloudflare widgets, the API can reach the verification service outbound, and the site configuration is valid. `captcha` needs no Cloudflare credentials.

## After changing configuration {#apply}

After changing configuration, rebuild the affected containers with the Compose configuration in the install directory. Refreshing the page or restarting existing containers does **not** reload Compose environment variables.

Do not replace a blue-green upgrade by editing image tags manually.

## Community edition registration and organizations {#signup}

Community edition supports email registration, Google sign-in and organization invitations. An account belongs to exactly one organization: self-registration creates an organization and makes the person who registers its administrator, while registering through an invitation joins the organization named in the invitation. The platform has no organization switcher. An organization administrator manages the organization through Settings → Organization / Members and cannot change platform SMTP or another organization's data.

| Configuration | Purpose |
| --- | --- |
| `HCDR_DEPLOYMENT_MODE` | `saas` (default) or `private`; private disables public registration |
| `HCDR_SAAS_SIGNUP_ENABLED` | Whether self-registration may create an organization, default true; when disabled, explicit invitations still work |
| `HCDR_GOOGLE_CLIENT_ID` / `HCDR_GOOGLE_CLIENT_SECRET` | Google OAuth web application credentials; both must be set, and the entry is hidden when either is missing |

Verification and invitation email use the defaults a platform administrator sets on the SMTP page. When none is configured, the registration page states that email cannot be sent.

For the Google callback, enter the platform access address followed by `/api/v1/saas/auth/google/callback`, for example `https://app.hypercdr.com/api/v1/saas/auth/google/callback`. The callback must match the address registered in Google Cloud exactly. Do not put the client secret in the frontend or commit it to a repository. A LAN IP is not a valid callback domain for regular Google web OAuth; development verification needs a domain or localhost forwarding.

::: info Scope of Google sign-in validation

Google sign-in verifies the signature, issuer, audience, validity period, verified email, state, nonce, PKCE and the browser that started the flow. A password account with the same email is **not** linked to Google automatically; sign in, verify the password in Sign-in & Security, then link it. A sign-in email cannot be changed to an unverified address from personal settings.

:::

Newly registered users get the light theme by default, and the theme preference is stored in the account. The sign-in and registration pages both use the dark style.

## Changing the platform access address (from IP to domain) {#access-url}

A platform administrator opens **Settings → Access & Login**, compares the current browser address with the configured address, enters the HTTPS origin (without a path), and saves once DNS, reverse proxy and TLS are ready. The page shows the verification email, invitation links and the Google callback address. Organization administrators do not have this permission.

The change takes effect on all API instances immediately and needs no restart; database settings survive an upgrade. `HCDR_PUBLIC_BASE_URL` only supplies a default during the first initialization of platform settings and does not override a saved address.

When the page is unavailable, run this on the install host:

```bash
sudo /var/lib/hypercdr/configure-public-url.sh https://app.example.com
```

The command runs `platform-configure-url` from the current API image, uses the same validation and updates the same database record; it does **not** change DNS, certificates or an external reverse proxy automatically. In a source development environment you can run `go run ./backend/cmd/platform-configure-url https://app.example.com` (requires `HCDR_DATABASE_URL`). The database and the current API image must be available; the API itself does not need to run.

::: warning Effects of changing the address

- New email uses the new address; links in existing email are not rewritten.
- The callback URI in Google Cloud must be updated as well, and an in-progress Google sign-in may need to start over.
- The connection address of an installed agent is not migrated automatically; keep the old entry point reachable or schedule a separate agent address migration.
- When the registration page detects a mismatched address, it offers the official entry point; a platform administrator can still sign in through the original entry point and fix the settings.

:::
