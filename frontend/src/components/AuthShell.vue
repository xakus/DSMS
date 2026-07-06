<script setup lang="ts">
// Оболочка экранов аутентификации (Login/Setup): анимированный фон по теме,
// логотип DSMS и центрированная карточка. Контент — через слот.
//
// ВАЖНО: тема применяется через класс .dark на корне компонента, а НЕ через
// :global(:root[data-theme]) — Vue scoped-компилятор отбрасывал хвост
// селектора после :global(), из-за чего opacity/фон применялись ко всему
// <html> (белая «пелена» на всё приложение) — см. фикс v1.0.8.
import { NIcon, NButton } from 'naive-ui'
import { SunnyOutline, MoonOutline } from '@vicons/ionicons5'
import { useThemeStore } from '../stores/theme'

defineProps<{
  /** Подпись под логотипом (по умолчанию — назначение продукта). */
  subtitle?: string
}>()

const theme = useThemeStore()
</script>

<template>
  <div class="auth-page" :class="{ dark: theme.isDark }">
    <!-- Анимированный фон: медленно плавающие цветные пятна -->
    <div class="blob blob-1" />
    <div class="blob blob-2" />
    <div class="blob blob-3" />

    <!-- Переключатель темы: иконка = ТЕКУЩАЯ тема (как в шапке приложения:
         тёмная → луна, светлая → солнце). Раньше было наоборот — инверсия. -->
    <n-button circle quaternary class="theme-toggle" @click="theme.toggle()">
      <template #icon>
        <n-icon :component="theme.isDark ? MoonOutline : SunnyOutline" />
      </template>
    </n-button>

    <div class="auth-card">
      <div class="brand">
        <img class="brand-logo" src="/logo.png" alt="DSMS" />
        <div class="brand-name">DSMS</div>
        <div class="brand-sub">{{ subtitle ?? 'Docker Swarm Management System' }}</div>
      </div>
      <slot />
    </div>
  </div>
</template>

<style scoped>
/* Полноэкранная сцена входа. Светлая тема — чистый светлый фон. */
.auth-page {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  background: #eef0f4;
}
.auth-page.dark {
  background: #0c0d12;
}

/* --- Анимированные цветные пятна фона ---
   Компактные и не слишком размытые — иначе светлая тема выглядела «в тумане». */
.blob {
  position: absolute;
  border-radius: 50%;
  filter: blur(48px);
  opacity: 0.38;
  will-change: transform;
}
.auth-page.dark .blob {
  opacity: 0.3;
}
.blob-1 {
  width: 380px;
  height: 380px;
  background: #3b5bdb;
  top: -60px;
  left: -40px;
  animation: float1 16s ease-in-out infinite;
}
.blob-2 {
  width: 320px;
  height: 320px;
  background: #4dabf7;
  bottom: -80px;
  right: -30px;
  animation: float2 20s ease-in-out infinite;
}
.blob-3 {
  width: 260px;
  height: 260px;
  background: #9775fa;
  bottom: 60px;
  left: 18%;
  animation: float3 24s ease-in-out infinite;
}
@keyframes float1 {
  0%, 100% { transform: translate(0, 0) scale(1); }
  50% { transform: translate(60px, 40px) scale(1.12); }
}
@keyframes float2 {
  0%, 100% { transform: translate(0, 0) scale(1); }
  50% { transform: translate(-50px, -30px) scale(1.08); }
}
@keyframes float3 {
  0%, 100% { transform: translate(0, 0) scale(1); }
  50% { transform: translate(30px, -50px) scale(1.15); }
}

/* --- Карточка входа: непрозрачная, по теме --- */
.auth-card {
  position: relative;
  z-index: 1;
  width: 400px;
  max-width: calc(100vw - 32px);
  padding: 40px 36px 36px;
  border-radius: 18px;
  background: #ffffff;
  /* Цвет текста задаём явно — карточка это div, Naive его не красит,
     иначе мои подписи (под логотипом, подсказки) остаются чёрными в тёмной. */
  color: #171a21;
  border: 1px solid rgba(20, 23, 33, 0.08);
  box-shadow: 0 20px 50px rgba(20, 23, 33, 0.18);
  animation: card-in 0.6s cubic-bezier(0.22, 1, 0.36, 1);
}
.auth-page.dark .auth-card {
  background: #17191f;
  color: #e7e8ec;
  border-color: rgba(255, 255, 255, 0.09);
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.55);
}
@keyframes card-in {
  from { opacity: 0; transform: translateY(24px) scale(0.98); }
  to { opacity: 1; transform: translateY(0) scale(1); }
}

/* Переключатель темы в правом верхнем углу */
.theme-toggle {
  position: absolute;
  top: 20px;
  right: 20px;
  z-index: 2;
  font-size: 18px;
}

/* Убираем цветной фон автозаполнения Chrome в полях ввода. */
:deep(input:-webkit-autofill),
:deep(input:-webkit-autofill:hover),
:deep(input:-webkit-autofill:focus) {
  -webkit-box-shadow: 0 0 0 1000px transparent inset;
  transition: background-color 9999s ease-in-out 0s;
  -webkit-text-fill-color: currentColor;
  caret-color: currentColor;
}

/* --- Логотип и заголовок --- */
.brand {
  text-align: center;
  margin-bottom: 28px;
}
.brand-logo {
  display: block;
  width: 76px;
  height: 76px;
  margin: 0 auto 14px;
  border-radius: 18px;
  box-shadow: 0 8px 22px rgba(31, 58, 130, 0.35);
}
.brand-name {
  font-size: 26px;
  font-weight: 700;
  letter-spacing: 1px;
  background: linear-gradient(135deg, #3b5bdb, #7048e8);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
}
.brand-sub {
  font-size: 14px;
  opacity: 0.65;
  margin-top: 2px;
}

/* Уважаем настройку «уменьшить движение». */
@media (prefers-reduced-motion: reduce) {
  .blob,
  .auth-card {
    animation: none;
  }
}
</style>
