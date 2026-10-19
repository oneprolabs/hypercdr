-- Login names and verified delivery addresses are separate identity attributes.
alter table users rename column email to username;
alter index idx_users_email_global_unique rename to idx_users_username_global_unique;
alter table users add column email text;
alter table users add column email_verified_at timestamptz;
update users set email=lower(username),email_verified_at=created_at where username like '%@%';
update users u set email=lower(r.email),email_verified_at=r.verified_at
from admin_recovery_email r where r.user_id=u.id and u.email is null
and not exists(select 1 from users other where other.id<>u.id and (lower(other.username)=lower(r.email) or lower(other.email)=lower(r.email)));
drop table admin_recovery_email;
create unique index idx_users_email_global_unique on users(lower(email)) where email is not null;
alter table users add constraint users_bound_email_valid check(email is null or (email=lower(trim(email)) and email like '%@%' and email_verified_at is not null));
-- Reserve login names and delivery addresses in one namespace. This also
-- serializes concurrent registrations and email changes to prevent ambiguity.
create function check_user_login_addresses() returns trigger language plpgsql as $$
begin
 perform pg_advisory_xact_lock(72419044);
 if exists(select 1 from users u where u.id<>new.id and
 (lower(u.username)=lower(new.username) or lower(u.email)=lower(new.username)
 or (new.email is not null and (lower(u.username)=lower(new.email) or lower(u.email)=lower(new.email))))) then
 raise unique_violation using message='Login account or bound email already belongs to another user';
 end if;
 return new;
end $$;
create trigger users_login_addresses before insert or update of username,email on users for each row execute function check_user_login_addresses();
create table user_email_verifications (
 token_hash text primary key,
 user_id uuid not null references users(id) on delete cascade,
 email text not null,
 previous_email text not null,
 expires_at timestamptz not null,
 created_at timestamptz not null default now()
);
create index on user_email_verifications(user_id);
