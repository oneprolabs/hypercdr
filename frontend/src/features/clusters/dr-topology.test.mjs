import assert from 'node:assert/strict';
import test from 'node:test';
import { buildDRTopology, orderClustersForTopology, topologyGrid, topologyLayout } from './dr-topology.ts';

function cluster(id, appIds) {
  return {
    id,
    name: id,
    apps: appIds.map((apiId, index) => ({ apiId, name: `${id}-app-${index + 1}`, namespace: `${id}-ns-${index + 1}` })),
  };
}

function plan(id, sourceClusterId, targetClusterId, appIds, status = 'ready') {
  return { id, sourceClusterId, targetClusterId, appId: appIds[0], appIds, status };
}

test('builds one directed relationship and deduplicates applications across plans', () => {
  const clusters = [cluster('a', ['app-1', 'app-2']), cluster('b', [])];
  const model = buildDRTopology(clusters, [
    plan('p1', 'a', 'b', ['app-1', 'app-1']),
    plan('p2', 'a', 'b', ['app-1', 'app-2']),
  ]);

  assert.equal(model.relationships.length, 1);
  assert.deepEqual(model.relationships[0].appIds, ['app-1', 'app-2']);
  assert.equal(model.summaries.a.protectedApps, 2);
  assert.equal(model.summaries.a.outboundApps, 2);
  assert.equal(model.summaries.b.inboundApps, 2);
});

test('keeps bidirectional protection as two independent directed relationships', () => {
  const clusters = [cluster('a', ['a-app']), cluster('b', ['b-app'])];
  const model = buildDRTopology(clusters, [
    plan('p1', 'a', 'b', ['a-app']),
    plan('p2', 'b', 'a', ['b-app']),
  ]);

  assert.deepEqual(model.relationships.map(item => item.id), ['a->b', 'b->a']);
  assert.equal(model.summaries.a.outboundApps, 1);
  assert.equal(model.summaries.a.inboundApps, 1);
  assert.equal(model.summaries.b.outboundApps, 1);
  assert.equal(model.summaries.b.inboundApps, 1);
});

test('returns no relationships for an empty plan set', () => {
  const model = buildDRTopology([cluster('a', ['app-1']), cluster('b', [])], []);
  assert.deepEqual(model.relationships, []);
  assert.equal(model.summaries.a.protectedApps, 0);
  assert.equal(model.summaries.b.inboundApps, 0);
});

test('ignores cleaning, same-cluster, unknown-cluster, and unresolved application plans', () => {
  const clusters = [cluster('a', ['app-1']), cluster('b', [])];
  const model = buildDRTopology(clusters, [
    plan('cleaning', 'a', 'b', ['app-1'], 'cleaning'),
    plan('same', 'a', 'a', ['app-1']),
    plan('unknown', 'a', 'missing', ['app-1']),
    plan('unresolved', 'a', 'b', ['missing-app']),
  ]);
  assert.deepEqual(model.relationships, []);
});

test('uses the most severe business status for a grouped relationship', () => {
  const clusters = [cluster('a', ['app-1', 'app-2']), cluster('b', [])];
  const model = buildDRTopology(clusters, [
    plan('ready', 'a', 'b', ['app-1'], 'ready'),
    plan('failed', 'a', 'b', ['app-2'], 'configuration_failed'),
  ]);
  assert.equal(model.relationships[0].status, 'failed');
});

test('places one-way source clusters before their targets while keeping mutual peers stable', () => {
  const clusters = [cluster('target', []), cluster('source', ['app-a'])];
  const oneWay = buildDRTopology(clusters, [plan('p1', 'source', 'target', ['app-a'], 'ready')]);
  assert.deepEqual(orderClustersForTopology(clusters, oneWay).map(item => item.id), ['source', 'target']);

  const peers = [cluster('left', ['app-l']), cluster('right', ['app-r'])];
  const mutual = buildDRTopology(peers, [
    plan('p2', 'left', 'right', ['app-l'], 'ready'),
    plan('p3', 'right', 'left', ['app-r'], 'ready'),
  ]);
  assert.deepEqual(orderClustersForTopology(peers, mutual).map(item => item.id), ['left', 'right']);
});

test('uses readable topology grids from one to many clusters', () => {
  assert.deepEqual(topologyGrid(1), { columns: 1, rows: 1, canvasHeight: 260 });
  assert.deepEqual(topologyGrid(2), { columns: 2, rows: 1, canvasHeight: 260 });
  assert.deepEqual(topologyGrid(3), { columns: 3, rows: 1, canvasHeight: 260 });
  assert.deepEqual(topologyGrid(4), { columns: 2, rows: 2, canvasHeight: 390 });
  assert.deepEqual(topologyGrid(7), { columns: 3, rows: 3, canvasHeight: 535 });
  assert.deepEqual(topologyGrid(16), { columns: 3, rows: 6, canvasHeight: 970 });
});

test('uses a stable geometric layout for connected clusters', () => {
  const clusters = [cluster('source-a', ['a']), cluster('target-a', []), cluster('source-b', ['b']), cluster('target-b', [])];
  const model = buildDRTopology(clusters, [plan('a', 'source-a', 'target-a', ['a']), plan('b', 'source-b', 'target-b', ['b'])]);
  const layout = topologyLayout(['source-a', 'source-b', 'target-a', 'target-b'], model);
  assert.notDeepEqual(layout.positions['source-a'], layout.positions['target-a']);
  assert.notDeepEqual(layout.positions['source-b'], layout.positions['target-b']);
  assert.ok(layout.positions['source-a'].x >= 0 && layout.positions['source-a'].x <= 100);
  assert.ok(layout.positions['target-a'].y >= 0 && layout.positions['target-a'].y <= 100);
});
