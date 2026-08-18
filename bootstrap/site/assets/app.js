const releases = {
  community: { path: './releases/community', version: 'current', available: false },
  enterprise: { path: './releases/enterprise', version: 'current', available: false },
};
const portalHost = window.location.hostname || '127.0.0.1';

function element(id) { return document.getElementById(id); }
function value(id) { return element(id).value.trim(); }
function shellQuote(input) {
  if (/^[A-Za-z0-9_./:@=-]+$/.test(input)) return input;
  return `'${input.replace(/'/g, "'\\''")}'`;
}
function releaseURL(edition) {
  return new URL(releases[edition].path.replace(/^\.\//, ''), window.location.href).toString().replace(/\/$/, '');
}

function updateCommands() {
  const communityURL = releaseURL('community');
  const enterpriseURL = releaseURL('enterprise');
  const communityVersion = releases.community.version;
  const enterpriseVersion = releases.enterprise.version;
  const nodeIP = value('k8s-node-ip') || portalHost;
  const nodePort = value('k8s-node-port') || '30080';

  element('community-docker-command').textContent = [
    `curl -fsSL ${communityURL}/hypercdr-bootstrap.tar.gz -o hypercdr-bootstrap.tar.gz`,
    'mkdir -p hypercdr-bootstrap && tar -xzf hypercdr-bootstrap.tar.gz -C hypercdr-bootstrap',
    'cd hypercdr-bootstrap',
    './install-platform.sh docker \\',
    `  --public-base-url ${shellQuote(value('host-public-url') || `https://${portalHost}:3002`)} \\`,
    `  --image-tag ${shellQuote(communityVersion)} --execute`,
  ].join('\n');

  element('community-k8s-command').textContent = [
    `curl -fsSL ${communityURL}/hypercdr-bootstrap.tar.gz -o hypercdr-bootstrap.tar.gz`,
    'mkdir -p hypercdr-bootstrap && tar -xzf hypercdr-bootstrap.tar.gz -C hypercdr-bootstrap',
    'cd hypercdr-bootstrap',
    './install-platform.sh k8s \\',
    '  --namespace hypercdr-system \\',
    `  --public-base-url https://${nodeIP}:${nodePort} \\`,
    `  --image-tag ${shellQuote(communityVersion)} \\`,
    `  --storage-class ${shellQuote(value('k8s-storage-class') || 'longhorn')} --node-port ${shellQuote(nodePort)} --database-mode bundled --execute`,
  ].join('\n');

  const namespace = value('enterprise-namespace') || 'hypercdr-enterprise';
  element('enterprise-command').textContent = [
    `curl -fsSL ${enterpriseURL}/hypercdr-enterprise-installer-${enterpriseVersion}.tar.gz -o hypercdr-enterprise.tar.gz`,
    'mkdir -p hypercdr-enterprise && tar -xzf hypercdr-enterprise.tar.gz -C hypercdr-enterprise',
    'cd hypercdr-enterprise',
    '# Review registry, URL, database, and secret settings before installation',
    'vi values.production.yaml',
    `./install.sh values.production.yaml hypercdr-enterprise ${shellQuote(namespace)}`,
  ].join('\n');
}

async function loadManifest(edition) {
  try {
    const response = await fetch(`${releases[edition].path}/manifest.json`, { cache: 'no-store' });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const manifest = await response.json();
    releases[edition].version = manifest.version;
    releases[edition].available = true;
    element(`${edition}-version`).textContent = manifest.version;
  } catch (_) {
    element(`${edition}-version`).textContent = 'Not published';
    if (edition === 'enterprise') element('enterprise-unavailable').classList.remove('hidden');
  }
  updateCommands();
}

async function copyText(text) {
  if (window.isSecureContext && navigator.clipboard?.writeText) return navigator.clipboard.writeText(text);
  const textarea = document.createElement('textarea');
  textarea.value = text; textarea.setAttribute('readonly', ''); textarea.style.position = 'fixed'; textarea.style.left = '-9999px';
  document.body.appendChild(textarea); textarea.select();
  const copied = document.execCommand('copy'); textarea.remove();
  if (!copied) throw new Error('Clipboard copy is unavailable');
}

element('host-public-url').value = `https://${portalHost}:3002`;
element('k8s-node-ip').value = portalHost;
for (const card of document.querySelectorAll('[data-edition]')) {
  card.addEventListener('click', () => {
    const edition = card.dataset.edition;
    for (const item of document.querySelectorAll('[data-edition]')) { const active = item === card; item.classList.toggle('is-selected', active); item.setAttribute('aria-pressed', String(active)); }
    element('community-panel').classList.toggle('hidden', edition !== 'community');
    element('enterprise-panel').classList.toggle('hidden', edition !== 'enterprise');
  });
}
for (const tab of document.querySelectorAll('[data-mode]')) {
  tab.addEventListener('click', () => {
    const mode = tab.dataset.mode;
    for (const item of document.querySelectorAll('[data-mode]')) { const active = item === tab; item.classList.toggle('is-active', active); item.setAttribute('aria-selected', String(active)); }
    element('community-docker').classList.toggle('hidden', mode !== 'docker');
    element('community-k8s').classList.toggle('hidden', mode !== 'k8s');
  });
}
for (const id of ['host-public-url', 'k8s-node-ip', 'k8s-node-port', 'k8s-storage-class', 'enterprise-namespace']) element(id).addEventListener('input', updateCommands);
for (const button of document.querySelectorAll('[data-copy-target]')) {
  button.addEventListener('click', async () => {
    try { await copyText(element(button.dataset.copyTarget).textContent); button.textContent = 'Copied'; }
    catch (_) { button.textContent = 'Copy failed'; }
    setTimeout(() => { button.textContent = 'Copy'; }, 1200);
  });
}
updateCommands();
void Promise.all([loadManifest('community'), loadManifest('enterprise')]);
