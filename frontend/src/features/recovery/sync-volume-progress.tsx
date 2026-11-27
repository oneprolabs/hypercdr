import React from 'react';
import type { ApiTask, ApiTaskEvent } from './types';
import { syncVolumeDisplay } from './sync-volume-model';
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
  const summary = syncVolumeDisplay(task, events, completed);
  const percent = summary.percent;
  const counts = [
    `Volumes discovered: ${summary.volumes.length}`,
    ...(summary.failed ? [`Failed: ${summary.failed}`] : []),
    ...(summary.canceled ? [`Canceled: ${summary.canceled}`] : []),
  ];
  const discoveryDone = completed || summary.discoveryComplete;
  const discoveryAt = summary.discoveryCompletedAt || summary.discoveryStartedAt;
  const failed = ['failed', 'canceled', 'cancelled'].includes(task.status);
  const total = summary.volumes.length;
  const snapshots = summary.volumes.filter(volume => volume.snapshotReady).length;
  const mounted = summary.volumes.filter(volume => volume.temporaryVolumeReady).length;
  const preparation = [
    { title: 'Creating CSI snapshots', doneTitle: 'CSI snapshots ready', count: snapshots, at: summary.snapshotStartedAt },
    { title: 'Preparing and mounting temporary volumes', doneTitle: 'Temporary volumes ready', count: mounted, at: summary.temporaryVolumeStartedAt },
  ];
  return <div className="hbdr-task-detail-events hbdr-sync-volume-timeline" aria-label="Volume backup events">
    {summary.discoveryStartedAt && <TimelineItem status={discoveryDone ? 'completed' : failed ? 'failed' : 'in_progress'} className="hbdr-task-log-entry" title={<><time>[{formatLocalDateTime(discoveryAt)}]</time><span>{discoveryDone ? 'Volume discovery completed.' : 'Discovering volume data.'} {counts.join(' · ')}</span></>} />}
    {summary.csi && preparation.filter(stage => stage.at).map(stage => <TimelineItem key={stage.title} status={total > 0 && stage.count === total ? 'completed' : failed ? 'failed' : 'in_progress'} className="hbdr-task-log-entry" title={<><time>[{formatLocalDateTime(stage.at)}]</time><span>{total > 0 && stage.count === total ? stage.doneTitle : stage.title} · Ready: {stage.count}/{total}</span></>} />)}
    {(summary.transferStartedAt || completed) && <TimelineItem status={completed ? 'completed' : failed ? 'failed' : 'in_progress'} className="hbdr-task-log-entry" title={<>{summary.transferStartedAt && <time>[{formatLocalDateTime(summary.transferStartedAt)}]</time>}<span>{summary.csi ? 'Scanning and backing up snapshot data.' : 'Scanning and backing up live volume data.'}</span></>}>
    <div className="hbdr-sync-transfer-body">
      <TransferProgress percent={completed ? 100 : percent} active={!completed && !failed} label={completed ? 'Volume data backup completed' : 'Volume data backup progress'} />
      <div className="hbdr-sync-volume-context">Processed {dataSize(summary.bytesDone)}{!completed && percent !== undefined ? ` / Estimated ${dataSize(summary.totalBytes)} · ~${percent.toFixed(2)}% · Estimated` : completed ? ' · Completed' : ''}</div>
      <div className="hbdr-sync-volume-context">Volumes completed: {summary.completed}/{total}
        {!completed && summary.speedBytesPerSecond > 0 ? ` · ${dataSize(summary.speedBytesPerSecond)}/s` : ''}
        {!completed && summary.etaSeconds > 0 ? ` · Estimated remaining: ${Math.ceil(summary.etaSeconds / 60)} min` : ''}
        {!completed && percent === undefined && !summary.allDataComplete ? ' · Total size not yet fully known' : ''}
      </div>
      {!completed && <div className="hbdr-sync-volume-context" role="status">{summary.activity}</div>}
      {summary.lastObservedAt && <div className="hbdr-sync-volume-context">Last observed: {formatLocalDateTime(summary.lastObservedAt)}</div>}
      {summary.volumes.length > 0 && <details><summary>Volume details</summary>
      {summary.volumes.map(volume => <div className="hbdr-sync-volume-row" key={volume.id}>
        <div className="hbdr-sync-volume-heading"><strong className="hbdr-sync-volume-name" title={volume.label}>{volume.label}</strong><span>{volume.state}</span></div>
        <div className="hbdr-sync-volume-context">{volume.context}</div>
        {volume.preparationMessage && <div className="hbdr-sync-volume-context">{volume.preparationMessage}</div>}
        <div className="hbdr-sync-volume-context" title="Processed / total · ~ indicates an estimated total">{dataSize(volume.bytesDone)}{volume.state === 'Completed' ? ` / ${dataSize(volume.bytesDone)}` : volume.knownTotal ? ` / ~${dataSize(volume.totalBytes)}` : ' · Total pending'}</div>
      </div>)}
      </details>}
    </div>
    </TimelineItem>}
    {completed && <TimelineItem status="completed" className="hbdr-task-log-entry" title={<><time>[{formatLocalDateTime(summary.transferCompletedAt || task.completedAt) || '-'}]</time><span>Volume data backup completed.</span></>} />}
  </div>;
}
