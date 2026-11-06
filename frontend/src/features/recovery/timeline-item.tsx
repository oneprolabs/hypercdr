import React from 'react';
import { AlertTriangle, Check, LoaderCircle, X } from 'lucide-react';

export type TimelineStatus = 'completed' | 'in_progress' | 'failed' | 'warning' | 'not_started' | 'waiting';

export function TimelineItem({ status, title, className = '', children }: {
  status: TimelineStatus;
  title: React.ReactNode;
  className?: string;
  children?: React.ReactNode;
}) {
  const label = status === 'in_progress' ? 'In progress' : status.replace('_', ' ');
  return <section className={`hbdr-timeline-item is-${status} ${className}`.trim()}>
    <span className="hbdr-timeline-rail" aria-hidden="true" />
    <span className={`hbdr-timeline-marker is-${status}`} role="img" aria-label={label}>
      {status === 'completed' && <Check size={11} strokeWidth={3} />}
      {status === 'in_progress' && <LoaderCircle size={12} strokeWidth={2.5} />}
      {status === 'failed' && <X size={11} strokeWidth={3} />}
      {status === 'warning' && <AlertTriangle size={10} strokeWidth={2.7} />}
    </span>
    <div className="hbdr-timeline-content"><strong className="hbdr-timeline-title">{title}</strong>{children}</div>
  </section>;
}
