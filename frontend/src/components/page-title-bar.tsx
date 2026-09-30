import type { ReactNode } from 'react';

/** Shared outer frame for primary page titles and their optional controls. */
export default function PageTitleBar({ children }: { children: ReactNode }) {
  return <div className="hbdr-page-title-bar">{children}</div>;
}
