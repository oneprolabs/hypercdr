-- Old tasks without uploader identity remain readable, but are never reused as
-- owner-bound registration requests. The API supports one active instance.
create unique index if not exists tasks_registration_owner_key_idx
on tasks (tenant_id, (payload->>'ownerId'), (payload->>'idempotencyKey'))
where type = 'cluster-registration'
  and coalesce(payload->>'ownerId', '') <> ''
  and coalesce(payload->>'idempotencyKey', '') <> '';

create unique index if not exists tasks_registration_session_idx
on tasks (tenant_id, (payload->>'sessionId'))
where type = 'cluster-registration'
  and coalesce(payload->>'ownerId', '') <> ''
  and coalesce(payload->>'sessionId', '') <> '';
