import React from 'react';
import type { ApiTask, ApiTaskEvent } from './types';
import { syncVolumeSummary } from './sync-volume-model';
import { TimelineItem } from './timeline-item';
import { TransferProgress } from './transfer-progress';
import { formatLocalDateTime } from '../../lib/date-time';

function dataSize(bytes: number): string {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit += 1; }
  return `${value.toFixed(unit === 0 ? 0 : 2)} ${units[unit]}`;
}

export function SyncVolumeProgress({ task, events, completed }: { task: ApiTask; events: ApiTaskEvent[]; completed: boolean }) {
  const summary = syncVolumeSummary(task, events);
  const counts = [
    `Volumes discovered: ${summary.volumes.length}`,
    ...(summary.failed ? [`Failed: ${summary.failed}`] : []),
    ...(summary.canceled ? [`Canceled: ${summary.canceled}`] : []),
  ];
  const discoveryDone = completed || summary.discoveryComplete;
  const discoveryAt = summary.discoveryCompletedAt || summary.discoveryStartedAt;
  const failed = ['failed', 'canceled', 'cancelled'].includes(task.status);
  return <div className="hbdr-task-detail-events hbdr-sync-volume-timeline" aria-label="Volume backup events">
    <TimelineItem status={discoveryDone ? 'completed' : failed ? 'failed' : 'in_progress'} className="hbdr-task-log-entry" title={<><time>[{formatLocalDateTime(discoveryAt) || '-'}]</time><span>{discoveryDone ? 'Volume discovery completed.' : 'Discovering volume data.'} {counts.join(' · ')}{!completed && !summary.discoveryComplete && summary.totalBytes > 0 ? ` · Estimated discovered size: ${dataSize(summary.totalBytes)}` : ''}</span></>} />
    {discoveryDone && <TimelineItem status={completed ? 'completed' : failed ? 'failed' : summary.transferStartedAt ? 'in_progress' : 'waiting'} className="hbdr-task-log-entry" title={<><time>[{formatLocalDateTime(completed ? summary.transferCompletedAt || summary.lastObservedAt : summary.transferStartedAt) || '-'}]</time><span>{completed ? 'Volume data backup completed.' : summary.transferStartedAt ? 'Syncing volume data.' : 'Waiting for volume data transfer.'}</span></>}>
    <div className="hbdr-sync-transfer-body">
      {(completed || summary.percent !== undefined || summary.progressStarted) && <TransferProgress percent={completed ? 100 : summary.percent} label={completed ? 'Volume data backup completed' : 'Estimated total volume progress'} />}
      <div className="hbdr-sync-volume-context">Processed {dataSize(summary.bytesDone)}{!completed && summary.percent !== undefined ? ` / Estimated ${dataSize(summary.totalBytes)} · ~${summary.percent.toFixed(1)}% · Estimated` : completed ? ' · Completed' : summary.progressStarted ? ' · Updating volume statistics' : ''}</div>
      {summary.volumes.length > 0 && <details><summary>Volume details</summary>
      {summary.volumes.map(volume => <div className="hbdr-sync-volume-row" key={volume.id}>
        <div className="hbdr-sync-volume-heading"><strong className="hbdr-sync-volume-name" title={volume.label}>{volume.label}</strong><span>{volume.state}</span></div>
        <div className="hbdr-sync-volume-context" title="Processed / total · ~ indicates an estimated total">{dataSize(volume.bytesDone)}{volume.state === 'Completed' ? ` / ${dataSize(volume.bytesDone)}` : volume.knownTotal ? ` / ~${dataSize(volume.totalBytes)}` : ' · Total pending'}</div>
      </div>)}
      </details>}
    </div>
    </TimelineItem>}
  </div>;
}
