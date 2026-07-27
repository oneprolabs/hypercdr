delete from platform_sessions s
using users u
where s.user_id = u.id
  and u.must_change_password = true;
