-- Preserve historical ownership records while allowing ordinary users to be removed.

alter table agent_tokens
  drop constraint if exists agent_tokens_created_by_fkey,
  add constraint agent_tokens_created_by_fkey
    foreign key (created_by) references users(id) on delete set null;

alter table tasks
  drop constraint if exists tasks_requested_by_fkey,
  add constraint tasks_requested_by_fkey
    foreign key (requested_by) references users(id) on delete set null;

alter table audit_logs
  drop constraint if exists audit_logs_actor_id_fkey,
  add constraint audit_logs_actor_id_fkey
    foreign key (actor_id) references users(id) on delete set null;
