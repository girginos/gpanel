// ESLint flat-config — gPanel frontend (React 18 + TypeScript + Vite)
// Amac: React Hooks kural ihlalleri (beyaz-ekran sinifi hatalar) ile temel TS
// bug'larini yakalamak. Yalnizca `src` lintlenir (bkz. package.json "lint" script).
//
//   - @eslint/js recommended        -> temel JS dogruluk kurallari
//   - typescript-eslint recommended -> tip-bilgisi GEREKTIRMEYEN hizli varyant
//   - eslint-plugin-react-hooks     -> rules-of-hooks ERROR, exhaustive-deps WARN
//   - eslint-plugin-react-refresh   -> Vite HMR icin sadece-bilesen-export uyarisi
//
// dist/ ve node_modules/ yok sayilir.
import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist/**', 'node_modules/**'] },
  {
    files: ['src/**/*.{ts,tsx}'],
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      globals: { ...globals.browser, ...globals.es2021 },
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      // React18 hook kurallari — beyaz-ekran sinifi hatalar burada yakalanir.
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
    },
  },
)
