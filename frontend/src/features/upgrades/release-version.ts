// The ce-v/ee-v scheme starts a new numbering era; historical 1.x releases
// remain valid rollback targets rather than being considered newer than 0.x.
export function compareReleaseVersions(left: string, right: string): number {
  const parse = (value: string) => {
    const match = /^(?:(ce|ee)-v|v)?([0-9]+\.[0-9]+\.[0-9]+(?:\.[0-9]{8})?)(?:-ent\.([0-9]+))?$/.exec(value);
    if (!match) return null;
    return { edition: match[1] || (match[3] ? 'ee' : 'ce'), era: match[1] ? 1 : 0,
      numbers: [...match[2].split('.').map(Number), ...(match[3] ? [Number(match[3])] : [])] };
  };
  const a = parse(left), b = parse(right);
  if (!a || !b || a.edition !== b.edition) return 0;
  if (a.era !== b.era) return a.era - b.era;
  for (let i = 0; i < Math.max(a.numbers.length, b.numbers.length); i++) {
    const difference = (a.numbers[i] || 0) - (b.numbers[i] || 0);
    if (difference) return difference;
  }
  return 0;
}
