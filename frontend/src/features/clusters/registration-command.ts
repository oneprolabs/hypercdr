export function registrationCommand(source: string, platformIP?: string): { command: string; error: string | null } {
  const address = platformIP?.trim();
  const validAddress = address !== undefined && /^(\d{1,3}\.){3}\d{1,3}$/.test(address)
    && address.split('.').every(part => Number(part) <= 255 && String(Number(part)) === part);
  const error = address !== undefined && !validAddress ? 'Enter a valid IPv4 address.' : null;
  if (!source) return { command: '', error: null };
  const tokens: string[] = source.replace(/\\\r?\n/g, ' ').match(/(?:[^\s"'\\]|\\.|"(?:\\.|[^"\\])*"|'[^']*')+/g) || [];
  const pipe = tokens.indexOf('|');
  if (tokens[0] !== 'curl' || tokens.slice(pipe, pipe + 4).join(' ') !== '| bash -s --') {
    return address === undefined ? { command: source, error: null } : { command: '', error: 'Unable to update this installation command. Regenerate the command and try again.' };
  }
  if (address !== undefined) {
    try {
      const downloadIndex = pipe - 1;
      const download = new URL(tokens[downloadIndex].replace(/^(['"])(.*)\1$/, '$2'));
      if (!['http:', 'https:'].includes(download.protocol) || !download.pathname.endsWith('/install.sh')) throw new Error('Unexpected installation URL');
      if (validAddress) {
        download.hostname = address;
        tokens[downloadIndex] = `'${download.href}'`;
      }
      for (let index = tokens.length - 1; index > pipe; index--) {
        if (['--endpoint', '--endpoint-private', '--endpoint-public', '--platform-ip'].includes(tokens[index])) tokens.splice(index, 2);
      }
      tokens.push('--platform-ip');
      if (address) tokens.push(validAddress ? address : `'${address.replace(/'/g, "'\\''")}'`);
    } catch {
      return { command: '', error: 'Unable to update this installation command. Regenerate the command and try again.' };
    }
  }
  const lines = [tokens.slice(0, pipe).join(' ') + ' |', '  bash -s --'];
  for (const token of tokens.slice(pipe + 4)) {
    if (token.startsWith('--')) lines.push(`    ${token}`);
    else if (lines.length > 2) lines[lines.length - 1] += ` ${token}`;
    else return { command: source, error: 'Unable to format this installation command.' };
  }
  return { command: lines.join(' \\\n'), error };
}
