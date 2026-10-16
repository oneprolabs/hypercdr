// Translate built-in product terminology, never organization or custom role names.
export function organizationRole<T extends { name: string; roleType?: string; description?: string }>(role: T): T {
 if(role.roleType==='organization'&&(role.name==='admin'||role.name==='operator')) return {...role,name:role.name==='admin'?'Organization administrator':'Member'};
 if (role.roleType !== 'global_builtin' && role.roleType !== 'system') return role;
 const replace = (text:string) => text.replace(/\bTenant\b/g,'Organization').replace(/\btenant\b/g,'organization');
 return { ...role, name:replace(role.name), ...(role.description === undefined ? {} : {description:replace(role.description)}) };
}
