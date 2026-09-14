const releasePath = './releases/dev';
const releaseURL = new URL('releases/dev', window.location.href).toString().replace(/\/$/, '');
const portalHost = window.location.hostname || '127.0.0.1';
const defaultRegistry = `${portalHost}:5001/hypercdr`;
const defaultImageTag = 'v20260714.5';
const defaultNamespace = 'hypercdr-system';
const defaultStorageClass = 'longhorn';
const defaultHostDataDir = '/var/lib/hypercdr';

const fields = [
  'k8s-access-mode',
  'k8s-node-ip',
  'k8s-node-port',
  'k8s-public-url',
  'k8s-registry',
  'k8s-registry-trust',
  'k8s-registry-ca-file',
  'host-public-url',
  'host-registry',
  'host-registry-trust',
  'host-registry-ca-file',
];

function value(id) {
  return document.getElementById(id).value.trim();
}

function setDefaultValue(id, nextValue) {
  const element = document.getElementById(id);
  if (element && !element.value.trim()) element.value = nextValue;
}

function shellQuote(input) {
  if (/^[A-Za-z0-9_./:@=-]+$/.test(input)) return input;
  return `'${input.replace(/'/g, "'\\''")}'`;
}

function k8sPublicBaseURL() {
  if (value('k8s-access-mode') === 'nodeport') {
    return `https://${value('k8s-node-ip')}:${value('k8s-node-port')}`;
  }
  return value('k8s-public-url');
}

function initializeDefaults() {
  const deploy = document.getElementById('deploy');
  const dockerPanel = document.getElementById('docker-panel');
  if (deploy && dockerPanel) deploy.prepend(dockerPanel);
  setDefaultValue('k8s-node-ip', portalHost);
  setDefaultValue('k8s-public-url', `https://${portalHost}:30080`);
  setDefaultValue('k8s-registry', defaultRegistry);
  setDefaultValue('host-public-url', `https://${portalHost}:3002`);
  setDefaultValue('host-registry', defaultRegistry);
}

function updateAccessMode() {
  const nodePortMode = value('k8s-access-mode') === 'nodeport';
  for (const element of document.querySelectorAll('.k8s-nodeport-field')) {
    element.classList.toggle('hidden', !nodePortMode);
  }
  for (const element of document.querySelectorAll('.k8s-custom-url-field')) {
    element.classList.toggle('hidden', nodePortMode);
  }
  document.getElementById('k8s-derived-url').textContent = k8sPublicBaseURL();
}

function updateCommands() {
  updateAccessMode();
  const k8sPrivateCA = value('k8s-registry-trust') === 'private-ca';
  const hostPrivateCA = value('host-registry-trust') === 'private-ca';
  for (const element of document.querySelectorAll('.k8s-private-ca-field')) element.classList.toggle('hidden', !k8sPrivateCA);
  for (const element of document.querySelectorAll('.host-private-ca-field')) element.classList.toggle('hidden', !hostPrivateCA);
  document.getElementById('host-private-ca-step').classList.toggle('hidden', !hostPrivateCA);
  document.getElementById('host-install-number').textContent = hostPrivateCA ? '2' : '1';
  document.getElementById('host-install-title').textContent = hostPrivateCA ? 'step-2-install-platform.sh' : 'step-1-install-platform.sh';
  const k8sTrustArgs = k8sPrivateCA
    ? [`  --registry-trust private-ca \\`, `  --registry-ca-file ${shellQuote(value('k8s-registry-ca-file') || '/path/to/registry-ca.crt')} \\`]
    : ['  --registry-trust system \\'];
  const k8sCommand = [
    `curl -fsSL ${releaseURL}/hypercdr-bootstrap.tar.gz -o hypercdr-bootstrap.tar.gz`,
    'rm -rf hypercdr-bootstrap',
    'mkdir -p hypercdr-bootstrap',
    'tar -xzf hypercdr-bootstrap.tar.gz -C hypercdr-bootstrap',
    'cd hypercdr-bootstrap',
    'chmod +x install-platform.sh',
    './install-platform.sh k8s \\',
    `  --namespace ${shellQuote(defaultNamespace)} \\`,
    `  --public-base-url ${shellQuote(k8sPublicBaseURL())} \\`,
    `  --registry ${shellQuote(value('k8s-registry'))} \\`,
    ...k8sTrustArgs,
    `  --image-tag ${shellQuote(defaultImageTag)} \\`,
    `  --storage-class ${shellQuote(defaultStorageClass)} \\`,
    `  --node-port ${shellQuote(value('k8s-node-port'))} \\`,
    '  --database-mode bundled \\',
    '  --execute',
  ].join('\n');

  const hostPrepareCommand = [
    `curl -fsSL ${releaseURL}/hypercdr-bootstrap.tar.gz -o hypercdr-bootstrap.tar.gz`,
    'rm -rf hypercdr-bootstrap',
    'mkdir -p hypercdr-bootstrap',
    'tar -xzf hypercdr-bootstrap.tar.gz -C hypercdr-bootstrap',
    'cd hypercdr-bootstrap',
    'chmod +x prepare-docker-registry.sh',
    './prepare-docker-registry.sh \\',
    `  --registry ${shellQuote(value('host-registry'))} \\`,
    '  --registry-trust private-ca \\',
    `  --ca-file ${shellQuote(value('host-registry-ca-file') || '/path/to/registry-ca.crt')}`,
  ].join('\n');

  const hostInstallCommand = [
    `curl -fsSL ${releaseURL}/hypercdr-bootstrap.tar.gz -o hypercdr-bootstrap.tar.gz`,
    'rm -rf hypercdr-bootstrap',
    'mkdir -p hypercdr-bootstrap',
    'tar -xzf hypercdr-bootstrap.tar.gz -C hypercdr-bootstrap',
    'cd hypercdr-bootstrap',
    'chmod +x install-platform.sh',
    './install-platform.sh docker \\',
    `  --public-base-url ${shellQuote(value('host-public-url'))} \\`,
    `  --data-dir ${shellQuote(defaultHostDataDir)} \\`,
    `  --registry ${shellQuote(value('host-registry'))} \\`,
    `  --registry-trust ${hostPrivateCA ? 'private-ca' : 'system'} \\`,
    ...(hostPrivateCA ? [`  --registry-ca-file ${shellQuote(value('host-registry-ca-file') || '/path/to/registry-ca.crt')} \\`] : []),
    `  --image-tag ${shellQuote(defaultImageTag)} \\`,
    '  --execute',
  ].join('\n');

  document.getElementById('k8s-command').textContent = k8sCommand;
  document.getElementById('host-prepare-command').textContent = hostPrepareCommand;
  document.getElementById('host-install-command').textContent = hostInstallCommand;
}

async function loadManifest() {
  try {
    const response = await fetch(`${releasePath}/manifest.json`, { cache: 'no-store' });
    const manifest = await response.json();
    document.getElementById('release-version').textContent = manifest.version;
  } catch (error) {
    document.getElementById('release-version').textContent = 'dev';
  }
}

for (const id of fields) {
  document.getElementById(id).addEventListener('input', updateCommands);
}

for (const button of document.querySelectorAll('[data-copy-target]')) {
  button.addEventListener('click', async () => {
    const target = document.getElementById(button.dataset.copyTarget);
    await navigator.clipboard.writeText(target.textContent);
    const previous = button.textContent;
    button.textContent = 'Copied';
    setTimeout(() => {
      button.textContent = previous;
    }, 1200);
  });
}

initializeDefaults();
updateCommands();
void loadManifest();
