import test from 'node:test';import assert from 'node:assert/strict';
import {recoveryApplicationImages} from './recovery-images.ts';
const tool='old.registry/team/velero:v1.18.2';
test('legacy drill catalogs hide injected helper without changing cached resources',()=>{
 const resources=[{kind:'ReplicaSet',name:'mysql-abc',namespace:'demo',images:['mysql:v1']},{kind:'Pod',name:'mysql-abc-xyz',namespace:'demo',images:['mysql:v1',tool]}];
 assert.deepEqual(recoveryApplicationImages(resources),['mysql:v1']);assert.equal(resources[1].images.length,2);
});
test('preserves business template init images and standalone Pod images',()=>{
 assert.deepEqual(recoveryApplicationImages([{kind:'ReplicaSet',name:'app-abc',images:[tool]},{kind:'Pod',name:'app-abc-xyz',images:[tool]}]),[tool]);
 assert.deepEqual(recoveryApplicationImages([{kind:'Pod',name:'standalone',images:[tool]}]),[tool]);
});
test('recognizes unified component tags without hiding similarly named business images',()=>{
 const business='registry/team/velero-dashboard:v1';const tool='registry/team/hypercdr:velero-v1.18';
 assert.deepEqual(recoveryApplicationImages([{kind:'StatefulSet',name:'db',images:['db:v1']},{kind:'Pod',name:'db-0',images:['db:v1',tool,business]}]),['db:v1',business]);
});
