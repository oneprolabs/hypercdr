with scheduled as (
  select pp.policy_id, min(pps.next_fire_at) as next_fire_at
  from protection_plans pp
  join protection_plan_schedules pps on pps.protection_plan_id = pp.id
  where pp.policy_id is not null and pps.next_fire_at is not null
  group by pp.policy_id
), anchors as (
  select p.id,
         coalesce(s.next_fire_at,
           case p.schedule_type
             when 'daily' then
               (date '2026-01-15'::timestamp + make_interval(hours => coalesce(p.hour, 0), mins => coalesce(p.minute, 0))) at time zone 'Asia/Shanghai'
             when 'weekly' then
               ((date '2026-01-11' + coalesce(p.week_day, 0))::timestamp + make_interval(hours => coalesce(p.hour, 0), mins => coalesce(p.minute, 0))) at time zone 'Asia/Shanghai'
             when 'monthly' then
               ((date '2026-01-01' + greatest(0, coalesce(p.month_day, 1) - 1))::timestamp + make_interval(hours => coalesce(p.hour, 0), mins => coalesce(p.minute, 0))) at time zone 'Asia/Shanghai'
           end
         ) as utc_anchor
  from policies p
  left join scheduled s on s.policy_id = p.id
  where p.schedule_type in ('daily', 'weekly', 'monthly')
)
update policies p
set hour = extract(hour from a.utc_anchor at time zone 'UTC')::integer,
    minute = extract(minute from a.utc_anchor at time zone 'UTC')::integer,
    week_day = extract(dow from a.utc_anchor at time zone 'UTC')::integer,
    month_day = extract(day from a.utc_anchor at time zone 'UTC')::integer,
    updated_at = now()
from anchors a
where p.id = a.id and a.utc_anchor is not null;
