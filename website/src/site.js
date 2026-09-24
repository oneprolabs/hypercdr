const menuButton = document.querySelector('[data-menu-button]');
const navigation = document.querySelector('[data-navigation]');
menuButton?.addEventListener('click', () => { const open = navigation?.classList.toggle('is-open') ?? false; menuButton.setAttribute('aria-expanded', String(open)); });
navigation?.querySelectorAll('a').forEach((link) => link.addEventListener('click', () => { navigation.classList.remove('is-open'); menuButton?.setAttribute('aria-expanded', 'false'); }));
document.querySelectorAll('[data-copy-command]').forEach((button) => button.addEventListener('click', async () => { const command = document.querySelector('[data-install-command]')?.textContent?.trim() ?? ''; try { await navigator.clipboard.writeText(command); button.textContent = 'Copied'; window.setTimeout(() => { button.textContent = 'Copy command'; }, 1600); } catch { button.textContent = 'Select command'; } }));
