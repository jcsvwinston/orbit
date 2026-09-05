import js from '@eslint/js'
import globals from 'globals'
import tseslint from 'typescript-eslint'
import reactHooks from 'eslint-plugin-react-hooks'

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'coverage'] },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    plugins: { 'react-hooks': reactHooks },
    languageOptions: {
      ecmaVersion: 2022,
      globals: { ...globals.browser, ...globals.es2021 },
    },
    rules: {
      // The two classic hooks rules. The plugin's v7 "recommended" preset
      // also ships the React Compiler lints (set-state-in-effect,
      // immutability, ...), which flag idiomatic load-on-mount effects; the
      // SPA does not use the compiler, so those stay off.
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'error',
      // The panel talks to a typed backend; `any` hides contract drift.
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrorsIgnorePattern: '^_' }],
    },
  },
  {
    files: ['vite.config.ts', 'eslint.config.js', 'tools/**/*.ts'],
    languageOptions: { globals: { ...globals.node } },
  },
)
