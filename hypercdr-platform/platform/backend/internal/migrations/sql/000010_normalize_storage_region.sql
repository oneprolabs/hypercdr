update storage_repositories
set region = null,
    updated_at = now()
where lower(trim(coalesce(region, ''))) in ('n/a', 'na', '-');
