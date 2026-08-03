import fs from 'node:fs';
import path from 'node:path';
import ts from 'typescript';

const [sourceName, destinationName, functionName] = process.argv.slice(2);
if (!sourceName || !destinationName || !functionName) {
  throw new Error('usage: node refactor-extract-function.mjs <source> <destination> <function>');
}

const sourceText = fs.readFileSync(sourceName, 'utf8');
const sourceFile = ts.createSourceFile(sourceName, sourceText, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const declaration = sourceFile.statements.find(statement => ts.isFunctionDeclaration(statement) && statement.name?.text === functionName);
if (!declaration) throw new Error(`function not found: ${functionName}`);

const functionText = sourceText.slice(declaration.getStart(sourceFile), declaration.end)
  .replace(`function ${functionName}`, `export default function ${functionName}`);
fs.mkdirSync(path.dirname(destinationName), { recursive: true });
fs.writeFileSync(destinationName, `${functionText}\n`);
fs.writeFileSync(sourceName, sourceText.slice(0, declaration.getFullStart()) + sourceText.slice(declaration.end));
