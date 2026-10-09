# OpenAPI route contract

Authenticated `GET /api/v1/schema` returns an OpenAPI 3.1 document built from the
live route registrations, including method/path, path parameters, access scope,
bearer security, and the shared error envelope (`error`, optional `message` and
`requestId`). It includes edition extension routes only when they are mounted.

This is an incremental contract: endpoint request bodies and successful payloads
are not yet comprehensively modeled. Do not generate a client from it yet.
The route/access inventory is generated at runtime rather than copied by hand.
Tests verify coverage and default tenant guards. Unknown API domains fail route
registration until an access policy is explicitly added.
