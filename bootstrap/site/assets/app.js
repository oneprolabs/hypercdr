const releasePath = './releases/dev';
const releaseURL = new URL('releases/dev', window.location.href).toString().replace(/\/$/, '');
const portalHost = window.location.hostname || '127.0.0.1';
const defaultRegistry = `${portalHost}:5001/hypercdr`;
const defaultImageTag = 'v20260714.5';
const defaultNamespace = 'hypercdr-system';
const defaultStorageClass = 'longhorn';
const defaultHostDataDir = '/data/hypercdr/deploy';

const fields = [
  'k8s-access-mode',
  'k8s-node-ip',
  'k8s-node-port',
  'k8s-public-url',
  'k8s-registry',
  'host-public-url',
  'host-registry',
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
  document.getElementById('current-host').textContent = portalHost;
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
    `./prepare-docker-registry.sh --registry ${shellQuote(value('host-registry'))}`,
  ].join('\n');

  const hostCheckCommand = [
    'test -d hypercdr-bootstrap || { echo "Run step 1 first."; exit 1; }',
    'cd hypercdr-bootstrap',
    'chmod +x check-harbor.sh',
    `./check-harbor.sh --registry ${shellQuote(value('host-registry'))} --image-tag ${shellQuote(defaultImageTag)}`,
  ].join('\n');

  const hostInstallCommand = [
    'test -d hypercdr-bootstrap || { echo "Run step 1 first."; exit 1; }',
    'cd hypercdr-bootstrap',
    'chmod +x install-platform.sh',
    './install-platform.sh docker \\',
    `  --public-base-url ${shellQuote(value('host-public-url'))} \\`,
    `  --data-dir ${shellQuote(defaultHostDataDir)} \\`,
    `  --registry ${shellQuote(value('host-registry'))} \\`,
    `  --image-tag ${shellQuote(defaultImageTag)} \\`,
    '  --execute',
  ].join('\n');

  document.getElementById('k8s-command').textContent = k8sCommand;
  document.getElementById('host-prepare-command').textContent = hostPrepareCommand;
  document.getElementById('host-check-command').textContent = hostCheckCommand;
  document.getElementById('host-install-command').textContent = hostInstallCommand;
}

async function loadManifest() {
  const list = document.getElementById('artifact-list');
  try {
    const response = await fetch(`${releasePath}/manifest.json`, { cache: 'no-store' });
    const manifest = await response.json();
    document.getElementById('release-version').textContent = manifest.version;
    document.getElementById('release-build').textContent = `Built ${manifest.buildTime}`;

    list.innerHTML = '';
    for (const artifact of manifest.artifacts) {
      const link = document.createElement('a');
      link.className = 'artifact';
      link.href = `${releasePath}/${artifact.file}`;
      link.download = artifact.file;
      link.innerHTML = `<strong>${artifact.name}</strong><small>${artifact.file}</small><small>${artifact.description}</small>`;
      list.appendChild(link);
    }
  } catch (error) {
    document.getElementById('release-version').textContent = 'dev';
    document.getElementById('release-build').textContent = 'Manifest unavailable';
    list.textContent = 'Unable to load release manifest.';
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
