# OpenAPI route contract

Authenticated `GET /api/v1/schema` returns an OpenAPI 3.1 document built from the
live route registrations, including method/path, path parameters, access scope,
bearer security, endpoint-specific release-token alternatives, migration-session
authorization, and the shared error envelope (`error`, optional `message` and
`requestId`). It includes edition extension routes only when they are mounted.

Authentication routes share typed wire DTOs with their handlers and expose
request fields and successful payloads, including the configured challenge mode.
Tests check mounted auth coverage and compare actual login/profile/config/logout
responses with the schemas.

Application and tag routes also share DTOs with their handlers and document
query fields, collection responses, batch tag replacement, and creation status.
Domain route coverage tests fail when one of these mounted routes lacks a
payload contract.

This is an incremental contract: other endpoint request bodies and successful
payloads are not yet comprehensively modeled. Do not generate a client from it yet.
The route/access inventory is generated at runtime rather than copied by hand.
Tests verify coverage and default tenant guards. Unknown API domains fail route
registration until an access policy is explicitly added.

Release-token eligibility is shared with the authentication middleware and tested
against every mounted operation. Migration source session routes document
`Authorization: Migration <sessionToken>` separately from platform Bearer auth.
