import type { Dispatch, SetStateAction } from 'react';

// A page may unmount after submitting a mutation. Its response must not update
// the shared resource state of a different session through retained callbacks.
export function scopedResourceSetter<T>(ownerRef: { current: string }, owner: string, dispatch: Dispatch<SetStateAction<T>>): Dispatch<SetStateAction<T>> {
  return update => {
    if (owner && ownerRef.current === owner) dispatch(update);
  };
}
