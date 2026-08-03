import fs from 'node:fs';

const [sourceName, extractedName, functionName, beforeMarker] = process.argv.slice(2);
if (!sourceName || !extractedName || !functionName || !beforeMarker) {
  throw new Error('usage: node refactor-restore-function.mjs <source> <extracted> <function> <before-marker>');
}
const source = fs.readFileSync(sourceName, 'utf8');
const extracted = fs.readFileSync(extractedName, 'utf8').replace(`export default function ${functionName}`, `function ${functionName}`);
const marker = source.indexOf(beforeMarker);
if (marker < 0) throw new Error(`marker not found: ${beforeMarker}`);
fs.writeFileSync(sourceName, `${source.slice(0, marker)}${extracted}\n${source.slice(marker)}`);
fs.unlinkSync(extractedName);
