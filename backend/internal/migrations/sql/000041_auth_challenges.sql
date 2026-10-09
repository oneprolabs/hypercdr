-- Authentication state must survive a single-active blue/green handoff.
-- Hash IDs and answers so database access does not expose usable challenges.
create table auth_challenges (
    kind text not null check (kind in ('captcha', 'google-oauth')),
    id_hash bytea not null,
    answer_hash bytea not null,
    expires_at timestamptz not null,
    primary key (kind, id_hash)
);
create index auth_challenges_expiry_idx on auth_challenges(expires_at);
