<script setup lang="ts">
// Оболочка экранов аутентификации (Login/Setup): анимированный фон по теме,
// логотип DSMS и центрированная стеклянная карточка. Контент — через слот.
import { NIcon } from 'naive-ui'
import { CubeOutline } from '@vicons/ionicons5'

defineProps<{
  /** Подпись под логотипом (по умолчанию — назначение продукта). */
  subtitle?: string
}>()
</script>

<template>
  <div class="auth-page">
    <!-- Анимированный фон: медленно плавающие цветные пятна -->
    <div class="blob blob-1" />
    <div class="blob blob-2" />
    <div class="blob blob-3" />

    <div class="auth-card">
      <div class="brand">
        <div class="brand-logo">
          <n-icon :component="CubeOutline" :size="30" />
        </div>
        <div class="brand-name">DSMS</div>
        <div class="brand-sub">{{ subtitle ?? 'Docker Swarm Management' }}</div>
      </div>
      <slot />
    </div>
  </div>
</template>

<style scoped>
/* Полноэкранная сцена; фон по теме через data-theme на <html>. */
.auth-page {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  background: #e6e8ee;
}
:global(:root[data-theme='dark']) .auth-page {
  background: #0c0d12;
}

/* --- Анимированные цветные пятна фона --- */
.blob {
  position: absolute;
  border-radius: 50%;
  filter: blur(70px);
  opacity: 0.5;
  will-change: transform;
}
.blob-1 {
  width: 420px;
  height: 420px;
  background: #3b5bdb;
  top: -80px;
  left: -60px;
  animation: float1 16s ease-in-out infinite;
}
.blob-2 {
  width: 360px;
  height: 360px;
  background: #63b3ed;
  bottom: -100px;
  right: -40px;
  animation: float2 20s ease-in-out infinite;
}
.blob-3 {
  width: 300px;
  height: 300px;
  background: #9f7aea;
  bottom: 40px;
  left: 20%;
  animation: float3 24s ease-in-out infinite;
}
:global(:root[data-theme='dark']) .blob {
  opacity: 0.35;
}
@keyframes float1 {
  0%, 100% { transform: translate(0, 0) scale(1); }
  50% { transform: translate(60px, 40px) scale(1.15); }
}
@keyframes float2 {
  0%, 100% { transform: translate(0, 0) scale(1); }
  50% { transform: translate(-50px, -30px) scale(1.1); }
}
@keyframes float3 {
  0%, 100% { transform: translate(0, 0) scale(1); }
  50% { transform: translate(30px, -50px) scale(1.2); }
}

/* --- Стеклянная карточка с появлением --- */
.auth-card {
  position: relative;
  z-index: 1;
  width: 380px;
  max-width: calc(100vw - 32px);
  padding: 36px 32px 32px;
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.72);
  backdrop-filter: blur(18px) saturate(1.4);
  -webkit-backdrop-filter: blur(18px) saturate(1.4);
  border: 1px solid rgba(255, 255, 255, 0.6);
  box-shadow: 0 20px 50px rgba(20, 23, 33, 0.18);
  animation: card-in 0.6s cubic-bezier(0.22, 1, 0.36, 1);
}
:global(:root[data-theme='dark']) .auth-card {
  background: rgba(26, 28, 36, 0.66);
  border-color: rgba(255, 255, 255, 0.08);
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.5);
}
@keyframes card-in {
  from { opacity: 0; transform: translateY(24px) scale(0.98); }
  to { opacity: 1; transform: translateY(0) scale(1); }
}

/* --- Логотип и заголовок --- */
.brand {
  text-align: center;
  margin-bottom: 28px;
}
.brand-logo {
  width: 60px;
  height: 60px;
  margin: 0 auto 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 16px;
  color: #fff;
  background: linear-gradient(135deg, #3b5bdb, #7048e8);
  box-shadow: 0 8px 20px rgba(59, 91, 219, 0.4);
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
  font-size: 13px;
  opacity: 0.6;
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
