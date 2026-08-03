import fs from 'node:fs';
import ts from 'typescript';

const [fileName, ...names] = process.argv.slice(2);
if (!fileName || names.length === 0) {
  throw new Error('usage: node refactor-remove-declarations.mjs <file> <declaration>...');
}

const sourceText = fs.readFileSync(fileName, 'utf8');
const sourceFile = ts.createSourceFile(fileName, sourceText, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const targets = new Set(names);
const ranges = sourceFile.statements
  .filter(statement => statement.name && targets.has(statement.name.text))
  .map(statement => ({ start: statement.getFullStart(), end: statement.end }))
  .sort((left, right) => right.start - left.start);

const found = new Set(sourceFile.statements.filter(statement => statement.name && targets.has(statement.name.text)).map(statement => statement.name.text));
const missing = names.filter(name => !found.has(name));
if (missing.length > 0) throw new Error(`declarations not found: ${missing.join(', ')}`);

let next = sourceText;
for (const range of ranges) next = next.slice(0, range.start) + next.slice(range.end);
fs.writeFileSync(fileName, next);
