alter table storage_repositories
  add column if not exists secret_ciphertext text not null default '';

comment on column storage_repositories.secret_payload is
  'Legacy plaintext credential payload. Runtime migration clears this value after encrypting secret_ciphertext.';
