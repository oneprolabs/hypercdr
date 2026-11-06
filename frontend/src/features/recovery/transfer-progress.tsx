import React from 'react';

export function TransferProgress({ percent, label }: { percent?: number; label: string }) {
  const value = percent !== undefined && Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) : undefined;
  return <div className="hbdr-transfer-progress-track" role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={value}>
    {value !== undefined && <div style={{ width: `${value}%` }} />}
  </div>;
}
