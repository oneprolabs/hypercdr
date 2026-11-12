import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { registrationCommand } from './registration-command.ts';

const source = 'curl -k -sSL https://platform.example:12443/install.sh | bash -s -- --token hcdr_example --cluster-type native-kubernetes --namespace hypercdr-agent --executor-mode kubernetes';

test('default command keeps its arguments and omits the endpoint override', () => {
  const result = registrationCommand(source);
  assert.equal(result.error, null);
  assert.equal(result.command.replace(/\\\n\s*/g, '').replace(/\s+/g, ' '), source);
  assert.doesNotMatch(result.command, /--endpoint/);
  assert.match(result.command, /\n    --token hcdr_example/);
  execFileSync('bash', ['-n'], { input: result.command });
});

test('custom IP updates the download host and passes only the IP to the installer', () => {
  const result = registrationCommand(source, ' 192.168.8.149 ');
  assert.equal(result.error, null);
  assert.match(result.command, /https:\/\/192\.168\.8\.149:12443\/install.sh/);
  assert.match(result.command, /--platform-ip 192\.168\.8\.149/);
  assert.doesNotMatch(result.command, /--endpoint/);
  assert.match(result.command, /--token hcdr_example/);
  assert.doesNotMatch(result.command, /platform.example/);
  execFileSync('bash', ['-n'], { input: result.command });
});

test('HTTP, standard ports and URL prefixes are preserved', () => {
  for (const origin of ['http://platform.example:18080/hcdr', 'https://platform.example']) {
    const result = registrationCommand(source.replace('https://platform.example:12443', origin), '10.0.0.1');
    const expected = origin.replace('platform.example', '10.0.0.1');
    assert.ok(result.command.includes(`${expected}/install.sh`));
    assert.match(result.command, /--platform-ip 10\.0\.0\.1/);
  }
});

test('explicit IP replaces legacy endpoint options without ambiguous routing', () => {
  const result = registrationCommand(`${source} --endpoint wss://old.example:9443/custom/agent --endpoint-public wss://public.example/ws/agent`, '10.0.0.2');
  assert.doesNotMatch(result.command, /--endpoint/);
  assert.equal(result.command.match(/--platform-ip /g).length, 1);
  assert.match(result.command, /--platform-ip 10\.0\.0\.2/);
});

test('empty, malformed and shell-interpolated addresses cannot produce copyable commands', () => {
  for (const address of ['', 'localhost', '256.1.1.1', '1.2.3', '01.2.3.4', '1.2.3.4:443', '1.2.3.4; echo bad', '$(echo bad)', 'https://1.2.3.4']) {
    const result = registrationCommand(source, address);
    assert.match(result.command, /--token hcdr_example/);
    assert.match(result.command, /--platform-ip/);
    assert.match(result.command, /https:\/\/platform.example:12443\/install.sh/);
    execFileSync('bash', ['-n'], { input: result.command });
    assert.ok(result.error);
  }
});

test('checking the override keeps the command visible with an empty IP argument', () => {
  const result = registrationCommand(source, '');
  assert.ok(result.command.endsWith('--platform-ip'));
  assert.match(result.command, /--namespace hypercdr-agent/);
  assert.ok(result.error);
});

test('quoted arguments, provider namespaces and regeneration are preserved', () => {
  const openshift = source.replace('hcdr_example', "'token with spaces'").replace('native-kubernetes', 'openshift').replace('hypercdr-agent', 'openshift-adp');
  const result = registrationCommand(openshift, '10.0.0.3');
  assert.match(result.command, /--token 'token with spaces'/);
  assert.match(result.command, /--namespace openshift-adp/);
  assert.match(registrationCommand(source.replace('hcdr_example', 'hcdr_new'), '10.0.0.3').command, /--token hcdr_new/);
  assert.equal(registrationCommand(result.command).error, null);
  execFileSync('bash', ['-n'], { input: result.command });
});

test('unknown templates fail closed when an IP override is requested', () => {
  assert.equal(registrationCommand('unexpected command').command, 'unexpected command');
  assert.equal(registrationCommand('unexpected command', '10.0.0.4').command, '');
  assert.ok(registrationCommand('unexpected command', '10.0.0.4').error);
});
