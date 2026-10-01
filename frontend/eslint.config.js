// Статический анализ SPA (NFR-12).
//
// Набор минимальный и по делу: типовые ошибки TypeScript и Vue. Стилевые
// правила (кавычки, отступы, порядок импортов) не включены — их роль
// выполняет единый стиль правок, а шумные линтеры перестают читать.
import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import pluginVue from 'eslint-plugin-vue'

export default tseslint.config(
  { ignores: ['dist/**', '../panel/web/dist/**', 'node_modules/**'] },

  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],

  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        // <script setup lang="ts"> разбирается TS-парсером внутри vue-парсера.
        parser: tseslint.parser,
        ecmaVersion: 'latest',
        sourceType: 'module',
      },
    },
  },

  {
    files: ['**/*.{ts,vue}'],
    rules: {
      // Неиспользуемое допускается только с префиксом _ (осознанная заглушка).
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      // Многословные имена компонентов не требуем: экраны называются по разделу.
      'vue/multi-word-component-names': 'off',
      // Браузерные глобалы и типы проверяет tsc (vue-tsc в сборке) — он делает
      // это точнее, а eslint без type-aware конфига даёт ложные no-undef.
      'no-undef': 'off',

      // Форматирование разметки — вкусовое; правила ниже давали 600+
      // предупреждений на существующем коде и заглушали содержательные.
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'vue/first-attribute-linebreak': 'off',
      'vue/html-closing-bracket-newline': 'off',
      'vue/attributes-order': 'off',
      'vue/html-indent': 'off',
    },
  },
)
