import tseslint from 'typescript-eslint';
export default [{
  files: ['src/**/*.{ts,tsx}'],
  languageOptions: { parser: tseslint.parser, parserOptions: { ecmaVersion: 'latest', sourceType: 'module', ecmaFeatures: { jsx: true } } },
  rules: {
    'no-debugger': 'error', 'no-dupe-args': 'error', 'no-dupe-keys': 'error',
    'no-duplicate-case': 'error', 'no-unreachable': 'error', 'no-unsafe-finally': 'error',
    'no-eval': 'error', 'constructor-super': 'error', 'valid-typeof': 'error',
  },
}, {
  files: ['src/**/*.{ts,tsx}'],
  ignores: ['src/api/**'],
  rules: { 'no-restricted-globals': ['error', { name: 'fetch', message: 'Use the shared authenticated API client.' }] },
}];
