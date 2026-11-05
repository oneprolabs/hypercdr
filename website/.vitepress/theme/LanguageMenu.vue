<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useSiteLanguages } from './useSiteLanguages'
import { rememberLocale } from './useLocalePreference'

/**
 * 首页的语言下拉。
 *
 * - 数据来自 VitePress 的 locales 配置（useSiteLanguages），加语言不用改这里；
 * - 视觉与文档站导航栏那个下拉保持一致（图标 + 箭头 → 卡片列表），
 *   只为落地页的浅色头部调了配色；
 * - 当前语言不可点，作为列表标题显示，其余语言是普通链接。
 */
defineProps<{ ariaLabel: string }>()

const languages = useSiteLanguages()
const current = computed(() => languages.value.find((item) => item.current))
const others = computed(() => languages.value.filter((item) => !item.current))

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)

function close() {
  open.value = false
}

/** 首页的下拉是整页跳转，必须在离开前记下选择，否则首屏脚本还会再分流一次 */
function choose(code: string) {
  rememberLocale(code)
  close()
}

function handlePointerDown(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) close()
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  event.stopPropagation()
  close()
  trigger.value?.focus()
}

onMounted(() => document.addEventListener('pointerdown', handlePointerDown))
onBeforeUnmount(() => document.removeEventListener('pointerdown', handlePointerDown))
</script>

<template>
  <div ref="root" class="lang-menu" @keydown="handleKeydown">
    <button
      ref="trigger"
      class="lang-menu-button"
      type="button"
      :aria-label="ariaLabel"
      :aria-expanded="open"
      aria-haspopup="true"
      @click="open = !open"
    >
      <svg class="lang-menu-globe" viewBox="0 0 24 24" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18" />
        <path d="M12 3c2.4 2.6 3.6 5.6 3.6 9s-1.2 6.4-3.6 9c-2.4-2.6-3.6-5.6-3.6-9S9.6 5.6 12 3Z" />
      </svg>
      <svg class="lang-menu-caret" viewBox="0 0 24 24" aria-hidden="true">
        <path d="m6 9 6 6 6-6" />
      </svg>
    </button>

    <div v-show="open" class="lang-menu-panel">
      <p class="lang-menu-title">{{ current?.title }}</p>
      <a
        v-for="item in others"
        :key="item.code"
        class="lang-menu-item"
        :href="item.href"
        :hreflang="item.hreflang"
        :lang="item.hreflang"
        @click="choose(item.code)"
      >{{ item.title }}</a>
    </div>
  </div>
</template>
