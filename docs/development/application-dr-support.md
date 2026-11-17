# Application DR support status

The agent periodically collects the complete cluster inventory. For each
namespace, the platform persists resource information and the `drSupport`
summary on its application record. The same record's `protectionStatus` controls
which Application DR stage displays it: `unprotected` (Select application),
`pending_protection` (Set up DR), or `protected` (Run DR).

Stage navigation does not request a new inventory. Each stage displays the
persisted `drSupport` value in its table row. A periodic inventory update changes
that value without changing `protectionStatus`, so a namespace is never moved
between stages by a support-status change. Final plan creation and application
protection updates still reject a missing, unknown, or unsupported status.
