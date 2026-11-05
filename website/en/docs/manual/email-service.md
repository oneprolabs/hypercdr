---
title: Email service
description: Configure SMTP delivery so invitation, verification and password recovery emails actually reach the recipient, and verify that they do.
---

# Email service

Entry point: **Settings → Email Service**. Requires a platform administrator account.

Invitations, email verification and password recovery all go out through the SMTP configuration you set up here. Without it, the related pages state plainly that email cannot be sent.

::: warning Saved, tested and delivered are three different things

A saved configuration only means the values reached the database; a successful test only means the server was reachable at that moment. Whether the recipient actually receives the mail also depends on their provider not filtering it. Confirm all three separately.

:::

## Before you start

- SMTP host, port, encryption, username and password (some providers require a dedicated app password rather than the login password);
- A sender address you are authorised to use — usually the domain or address must be verified with the provider first;
- A mailbox that can receive test mail, ideally one internal and one external domain;
- A correct public platform address. If it is wrong the mail still sends, but the invitation or reset link inside points somewhere unusable.

## Create a configuration

1. Open **Settings → Email Service** and select **New**.
2. Fill in configuration name, SMTP Server, Port, Encryption, Username, Password, Sender Name and Sender Email.
3. Select **Create Configuration** to save.
4. Select the row in the list and use **More → Test** to send a test message to an address you control.
5. Check **Test Status** in the list and the mailbox itself. If nothing arrives, check the spam folder and the provider's delivery log as well.
6. To use it for default delivery, select **More → Set as Default** and confirm the default marker in the list.

![The new SMTP configuration window](/images/user-guide/email-new.png)

The screenshot shows part of an empty creation window; fields such as Sender Email require scrolling down. The `587` and STARTTLS shown are the form's initial values, not a recommendation for your provider.

## Fields

| Field | Meaning |
| --- | --- |
| Configuration Name | A label for your own recognition, not the server hostname |
| SMTP Server / Port | Host and port from the provider; must match the encryption mode |
| Encryption | STARTTLS, TLS or None; prefer an encrypted connection the provider supports in production |
| Username / Password | SMTP credentials, possibly a dedicated app password; never reuse sample values |
| Sender Name | The display name recipients see |
| Sender Email | The from-address recipients see; must satisfy the provider's authorisation rules |
| Test Status | Not tested, Succeeded or Failed — the most recent test result |
| Default | The default delivery configuration, not merely a sort marker |

## Confirming the result

- Test status is Succeeded and the target mailbox actually received the message;
- The default marker sits on the configuration you intended;
- You triggered a real invitation or password reset and the link in the email points at the correct platform address.

The first two are not enough — a wrong link leaves the user stuck even though delivery worked.

## Common problems

| Symptom | Where to look |
| --- | --- |
| Tests keep failing | Outbound port blocked by a firewall or cloud security group; encryption mode not matching the port |
| Authentication rejected | Login password used instead of an app password; username may need to be the full email address |
| Sent successfully but nothing arrives | Recipient spam folder, provider delivery log, SPF/DKIM setup for the sending domain |
| Links in the email do not open | Does the platform address under `Settings → Access & Sign-in` match the address users actually visit |
| You want to delete the default configuration | The default cannot be deleted directly; create and verify a replacement, switch the default, then delete |

::: tip Keep the password out of screenshots and tickets

Mask the Password field in any screenshot you share while troubleshooting. A leaked SMTP credential lets anyone send mail as your domain.

:::

## Related

- [Platform configuration](/en/docs/deployment/configuration): environment variables and the public platform address
- [Users and organisations](/en/docs/user-guide/users-and-organizations): inviting members and account rules
