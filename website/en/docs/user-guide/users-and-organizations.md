---
title: "Users, organizations and platform permissions"
description: "Account ownership, organization isolation, Settings menu permissions, invitation and deletion rules, and bound email handling."
---

# Users, organizations and platform permissions

Each user belongs to exactly one organization. The UI says "organization"; `tenantId` in the API and database is the organization isolation identifier. A platform administrator's view scope is unrelated to organization membership and does not change the organization an account belongs to.

## Settings menu {#settings-menu}

- **Personal settings**: Personal Information and Sign-in & Security. Every signed-in user can change only their own profile and security settings. For an email account, Bound email is read-only; a non-email account can bind or change an email in personal information and verify the new email. The built-in administrator starts with no bound email, and its sign-in account remains `admin`.
- **Organization management**: Organization, Members. Organization members can view their own organization; organization administrators can edit the organization profile, invite and manage members. A member cannot switch to another organization through a URL, organization ID or request header.
- **Platform management**: Users, Organizations, Access & Sign-in, Email Service, Platform Upgrade (Upgrade to Enterprise is hidden for now). These pages are visible only to platform administrators. Users shows the organization, platform identity, organization role and status of every user; Organizations shows all organizations with their member and resource counts.

The member count of an organization and the Members list both include platform administrators; their Role shows `Platform administrator`, and protection limits still apply. When a platform administrator opens Members of an organization from Organizations, the page shows which organization is being viewed and who the actual operator is. This does not sign in as a member of that organization and does not change the platform administrator's own organization.

## Account creation, invitation and deletion {#invite}

A platform administrator creates an organization first, then invites the first organization administrator to that organization. Ordinary members can be invited afterwards.

- The account is created once email verification completes; an unfinished invitation appears in that organization's Invitations and can be revoked.
- Email Service must be configured before invitation and password reset email can be sent.
- One email address cannot join multiple organizations.
- In Users you can edit the name and organization role and enable or disable an account, but you **cannot change the account email or organization membership directly**.
- The delete action is labeled Delete account in the interface; it revokes sessions and linked sign-in identities immediately and **does not delete** the organization's clusters, storage, policies or protection configurations. To stop access temporarily, use Disable.
- In Members and Users, your own row shows `Your account` and offers no management buttons; the backend forbids deleting yourself, disabling yourself and changing your own role, even when the organization has other administrators.
- The built-in platform administrator is protected and cannot be modified, disabled or deleted through the organization member API.
- An organization with active administrators must keep at least one active organization administrator; concurrent operations are also protected by a database transaction lock.
- Disabling an organization revokes the sessions of its ordinary users, and re-enabling does not restore revoked sessions. The system default organization cannot be disabled.

## Business resource scope {#resource-scope}

Clusters, storage, policies, DR applications, tasks and restore points belong **only to the organization of the signed-in user**, platform administrators included. Business pages offer no organization switcher.

A platform administrator's cross-organization permission covers platform management only, such as users and organizations; it grants no access to the business resources of other organizations.

The backend does not widen the business scope through `X-HyperCDR-Organization`; passing another organization or `all` returns `organization_scope_forbidden`. Viewing or managing members of another organization does not change the signed-in user's organization membership.

## Session consistency and audit {#session}

Tabs in the same browser share sign-in storage. When you sign in or sign out in another tab, the current page reloads the identity and clears the previous account's pages. API requests are bound to the session shown in the current tab; when a credential change is detected, no business request is sent, and a delayed response for the previous account is never used on the new account's pages.

Organization and member management operations record the actual operator and the target organization; platform management operations record the target organization and the actual operator separately.

## Sign-in account and bound email {#bound-email}

| Field | Meaning |
| --- | --- |
| `users.username` | Fixed sign-in account: `admin` for the built-in administrator, the registration email for a self-registered user |
| `users.email` | Optional, globally unique bound email |
| `email_verified_at` | Email verification time |
| `display_name` | Name used for display only |

Rules:

- A sign-in account must also differ from every other user's email.
- A verified bound email can be used for sign-in and password recovery. When the sign-in account is an email address, it also serves as the bound email and is shown read-only in personal information; it cannot be changed.
- For a non-email sign-in account such as `admin`, enter a new email in Personal Information → Bound email and verify the current password; the system sends a confirmation link valid for 30 minutes, and the old email stays effective until confirmation. A new request replaces the old link, and a link can be used only once.
- A verified email from Google registration becomes the sign-in account and the bound email directly, with no edit option; the Google link is identified by provider subject.
- The old `admin_recovery_email` table and its dedicated API are removed; no separate administrator recovery address is maintained.

## Structural changes during development {#dev-notes}

This version uses migration `000044_user_bound_email.sql` to create the new identity structure. If a historical administrator recovery email collides with an ordinary account, the ordinary account email is kept and the administrator binding is left empty.

In a development environment, back up the database and platform configuration first, then deploy the new version fresh. **A new database cannot be reused with an old image**; restore a matching database backup instead.
