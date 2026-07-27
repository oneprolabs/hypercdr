const releasePath = './releases/dev';
const releaseURL = new URL('releases/dev', window.location.href).toString().replace(/\/$/, '');
const portalHost = window.location.hostname || '127.0.0.1';
const defaultImageTag = 'v20260714.5';
const defaultHostDataDir = '/var/lib/hypercdr';

function value(id) {
  return document.getElementById(id).value.trim();
}

function shellQuote(input) {
  if (/^[A-Za-z0-9_./:@=-]+$/.test(input)) return input;
  return `'${input.replace(/'/g, "'\\''")}'`;
}

function packageCommands() {
  return [
    `curl -fsSL ${releaseURL}/hypercdr-bootstrap.tar.gz -o hypercdr-bootstrap.tar.gz`,
    'rm -rf hypercdr-bootstrap',
    'mkdir -p hypercdr-bootstrap',
    'tar -xzf hypercdr-bootstrap.tar.gz -C hypercdr-bootstrap',
    'cd hypercdr-bootstrap',
    'chmod +x install-platform.sh',
  ];
}

function updateCommands() {
  const nodeIP = value('k8s-node-ip') || 'node-ip';
  const nodePort = value('k8s-node-port') || '30080';
  const k8sURL = `https://${nodeIP}:${nodePort}`;
  document.getElementById('k8s-derived-url').textContent = k8sURL;

  document.getElementById('host-install-command').textContent = [
    ...packageCommands(),
    './install-platform.sh docker \\',
    `  --public-base-url ${shellQuote(value('host-public-url') || `https://${portalHost}:3002`)} \\`,
    `  --data-dir ${shellQuote(defaultHostDataDir)} \\`,
    `  --image-tag ${shellQuote(defaultImageTag)} \\`,
    '  --execute',
  ].join('\n');

  document.getElementById('k8s-command').textContent = [
    ...packageCommands(),
    './install-platform.sh k8s \\',
    '  --namespace hypercdr-system \\',
    `  --public-base-url ${shellQuote(k8sURL)} \\`,
    `  --image-tag ${shellQuote(defaultImageTag)} \\`,
    `  --storage-class ${shellQuote(value('k8s-storage-class') || 'longhorn')} \\`,
    `  --node-port ${shellQuote(nodePort)} \\`,
    '  --database-mode bundled \\',
    '  --execute',
  ].join('\n');
}

async function loadManifest() {
  try {
    const response = await fetch(`${releasePath}/manifest.json`, { cache: 'no-store' });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const manifest = await response.json();
    document.getElementById('release-version').textContent = manifest.version;
  } catch (_) {
    document.getElementById('release-version').textContent = 'dev';
  }
}

document.getElementById('host-public-url').value = `https://${portalHost}:3002`;
document.getElementById('k8s-node-ip').value = portalHost;
for (const id of ['host-public-url', 'k8s-node-ip', 'k8s-node-port', 'k8s-storage-class']) {
  document.getElementById(id).addEventListener('input', updateCommands);
}
for (const button of document.querySelectorAll('[data-copy-target]')) {
  button.addEventListener('click', async () => {
    const target = document.getElementById(button.dataset.copyTarget);
    await navigator.clipboard.writeText(target.textContent);
    button.textContent = 'Copied';
    setTimeout(() => { button.textContent = 'Copy'; }, 1200);
  });
}
updateCommands();
void loadManifest();
