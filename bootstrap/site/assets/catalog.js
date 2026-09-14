/* Shared catalog loading for the Bootstrap page and regression tests. */
(function (root) {
  function entriesFor(catalog, edition) {
    const pattern = edition === 'enterprise'
      ? /^v(?:\d{8}\.\d+|\d+\.\d+\.\d+)-ent\.\d+$/
      : /^(?:\d+\.\d+\.\d+\.\d{8}|v\d{8}\.\d+)$/;
    return (Array.isArray(catalog.items) ? catalog.items : [catalog])
      .filter(item => item && pattern.test(item.version) && (!item.edition || item.edition === edition))
      .sort((a, b) => b.version.localeCompare(a.version, 'en', { numeric: true }));
  }

  async function load(edition, path, fetcher = fetch) {
    const sources = [
      { url: '/api/release-catalog', source: 'release-center', versioned: true },
      { url: `${path}/index.json`, source: 'local', versioned: true },
      { url: `${path}/manifest.json`, source: 'local', versioned: false },
    ];
    for (const source of sources) {
      try {
        const response = await fetcher(source.url, { cache: 'no-store', signal: AbortSignal.timeout(5000) });
        if (!response.ok) continue;
        const entries = entriesFor(await response.json(), edition);
        if (entries.length) return { ...source, entries };
      } catch (_) {
        // Network failures, timeouts, and invalid JSON must allow local fallback.
      }
    }
    throw new Error(`No published ${edition} releases`);
  }
  root.HyperCDRCatalog = { entriesFor, load };
  if (typeof module !== 'undefined') module.exports = root.HyperCDRCatalog;
})(globalThis);
