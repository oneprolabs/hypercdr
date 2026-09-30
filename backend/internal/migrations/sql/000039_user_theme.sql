alter table users add column if not exists theme text not null default 'light';
alter table users add constraint users_theme_valid check (theme in ('light', 'dark'));
