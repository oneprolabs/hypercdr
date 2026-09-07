import assert from 'node:assert/strict';
import test from 'node:test';
import { clustersAreDRCompatible } from './cluster-compatibility.ts';

test('DR compatibility keeps native Kubernetes and CCE interoperable', () => {
  assert.equal(clustersAreDRCompatible('native-kubernetes', 'huaweicloud-cce'), true);
  assert.equal(clustersAreDRCompatible('huaweicloud-cce', 'native-kubernetes'), true);
  assert.equal(clustersAreDRCompatible('native-kubernetes', 'native-kubernetes'), true);
});

test('DR compatibility isolates OpenShift from Kubernetes and CCE', () => {
  assert.equal(clustersAreDRCompatible('openshift', 'openshift'), true);
  assert.equal(clustersAreDRCompatible('openshift', 'native-kubernetes'), false);
  assert.equal(clustersAreDRCompatible('huaweicloud-cce', 'openshift'), false);
});
