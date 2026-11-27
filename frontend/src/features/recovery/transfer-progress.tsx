import React from 'react';

export function TransferProgress({ percent, label, active = false }: { percent?: number; label: string; active?: boolean }) {
  const value = percent !== undefined && Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) : undefined;
  return <div className={`hbdr-transfer-progress-track${value === undefined && active ? ' is-indeterminate' : ''}`} role="progressbar" aria-label={label} aria-valuetext={value === undefined && active ? 'Processing data; total size is not yet known' : undefined} aria-valuemin={0} aria-valuemax={100} aria-valuenow={value}>
    {value !== undefined ? <div style={{ width: `${value}%` }} /> : active ? <div /> : null}
  </div>;
}
