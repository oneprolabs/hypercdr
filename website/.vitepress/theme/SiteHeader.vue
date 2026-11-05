<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { HomeCopy } from './homeCopy'
import LanguageMenu from './LanguageMenu.vue'

const props = defineProps<{ copy: HomeCopy }>()

const mobileNavOpen = ref(false)

function toggleMobileNav() {
  mobileNavOpen.value = !mobileNavOpen.value
}

function closeMobileNav() {
  mobileNavOpen.value = false
}

watch(mobileNavOpen, (open) => {
  document.body.classList.toggle('hcdr-nav-open', open)
})

onBeforeUnmount(() => {
  document.body.classList.remove('hcdr-nav-open')
})

const ctaHref = computed(() => props.copy.hero.primaryHref)
</script>

<template>
  <!--
    外层只负责吸顶：需要一条通栏底色和分隔线，而 .site-header 受 max-width 约束，
    两者放在一起会出现横向错位，所以拆成 shell + header 两层。
  -->
  <div class="site-header-shell">
    <header class="site-header">
      <a class="brand" href="#top" :aria-label="copy.header.brandAria">
        <span class="brand-mark">H</span>
        <span>HyperCDR</span>
      </a>

      <button
        class="menu-button"
        type="button"
        data-menu-button
        :aria-expanded="mobileNavOpen"
        :aria-label="mobileNavOpen ? copy.header.closeMenu : copy.header.openMenu"
        aria-controls="main-navigation"
        @click="toggleMobileNav"
      >
        {{ copy.header.menuLabel }}
      </button>

      <nav
        id="main-navigation"
        class="main-nav"
        :class="{ 'is-open': mobileNavOpen }"
        data-navigation
        :aria-label="copy.header.navAria"
      >
        <a
          v-for="item in copy.nav"
          :key="item.href"
          :href="item.href"
          :target="item.external ? '_blank' : undefined"
          :rel="item.external ? 'noopener noreferrer' : undefined"
          @click="closeMobileNav"
        >{{ item.label }}</a>
        <LanguageMenu :aria-label="copy.header.langAria" />
        <a class="button button-small" :href="ctaHref" @click="closeMobileNav">
          {{ copy.header.cta }}
        </a>
      </nav>
    </header>
  </div>
</template>
