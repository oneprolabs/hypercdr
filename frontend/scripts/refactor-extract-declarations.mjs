import fs from 'node:fs';
import path from 'node:path';
import ts from 'typescript';

const [sourceName,destinationName,...names]=process.argv.slice(2);
if(!sourceName||!destinationName||names.length===0)throw new Error('usage: node refactor-extract-declarations.mjs <source> <destination> <name>...');
const source=fs.readFileSync(sourceName,'utf8');
const file=ts.createSourceFile(sourceName,source,ts.ScriptTarget.Latest,true,ts.ScriptKind.TSX);
const wanted=new Set(names);
const statementNames=statement=>{
  if(statement.name?.text)return[statement.name.text];
  if(ts.isVariableStatement(statement))return statement.declarationList.declarations.map(declaration=>declaration.name).filter(ts.isIdentifier).map(name=>name.text);
  return[];
};
const declarations=file.statements.filter(statement=>statementNames(statement).some(name=>wanted.has(name)));
const found=new Set(declarations.flatMap(statement=>statementNames(statement)));
const missing=names.filter(name=>!found.has(name));
if(missing.length)throw new Error(`declarations not found: ${missing.join(', ')}`);
const content=declarations.map(statement=>{const text=source.slice(statement.getStart(file),statement.end);return /^export\s/.test(text)?text:`export ${text}`}).join('\n\n');
let next=source;
for(const declaration of [...declarations].sort((a,b)=>b.getFullStart()-a.getFullStart()))next=next.slice(0,declaration.getFullStart())+next.slice(declaration.end);
fs.mkdirSync(path.dirname(destinationName),{recursive:true});
fs.writeFileSync(destinationName,`${content}\n`);
fs.writeFileSync(sourceName,next);
