// Async account mutations belong to the session that submitted them, including
// pending flags, notifications and browser persistence.
export function sessionMutationScope(ownerRef: { current: string }, owner: string) {
  return (apply: () => void): boolean => {
    if (!owner || ownerRef.current !== owner) return false;
    apply();
    return true;
  };
}
