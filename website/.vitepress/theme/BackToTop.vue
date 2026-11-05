<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useSiteLocale } from './useSiteLocale'

/**
 * 全站「返回顶部」按钮。
 *
 * 官网首页与文档站都挂它（见 Layout.vue），所以只管滚动行为和外观，
 * 不依赖任何页面结构。滚过一屏后才出现，避免首屏就压住内容。
 */

const locale = useSiteLocale()

/**
 * 官网首页是固定浅色版式，不跟随 .dark 翻转；
 * 按钮颜色要跟着页面走，所以由 Layout 告知当前是不是首页。
 */
const props = defineProps<{ home?: boolean }>()

/** 出现阈值：约一屏高度，太早出现会挡住首屏按钮 */
const SHOW_AFTER = 480

const visible = ref(false)
let frame = 0

function sync() {
  visible.value = window.scrollY > SHOW_AFTER
  frame = 0
}

function onScroll() {
  if (frame) return
  frame = window.requestAnimationFrame(sync)
}

function toTop() {
  const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  window.scrollTo({ top: 0, behavior: reduceMotion ? 'auto' : 'smooth' })
}

onMounted(() => {
  window.addEventListener('scroll', onScroll, { passive: true })
  sync()
})

onBeforeUnmount(() => {
  window.removeEventListener('scroll', onScroll)
  if (frame) window.cancelAnimationFrame(frame)
})
</script>

<template>
  <button
    type="button"
    class="hcdr-back-to-top"
    :class="{ 'is-visible': visible, 'is-home': props.home }"
    :aria-label="locale === 'en' ? 'Back to top' : '返回顶部'"
    :aria-hidden="!visible"
    :tabindex="visible ? 0 : -1"
    @click="toTop"
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
      stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M12 19V5" />
      <path d="M5 12l7-7 7 7" />
    </svg>
  </button>
</template>
