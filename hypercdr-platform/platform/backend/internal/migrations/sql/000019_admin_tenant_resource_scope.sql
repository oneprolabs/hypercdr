-- The built-in system administrator has global management permissions but
-- remains scoped to its own tenant for business resources.

update tenants
set name = 'Admin',
    status = 'active',
    updated_at = now()
where id = '00000000-0000-0000-0000-000000000001';
