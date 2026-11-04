<script setup lang="ts">
import type { HomeCopy } from './homeCopy'

defineProps<{ copy: HomeCopy }>()
</script>

<template>
  <footer class="site-footer">
    <div>
      <a class="brand" href="#top">
        <span class="brand-mark">H</span>
        <span>HyperCDR</span>
      </a>
      <p>{{ copy.footer.description }}</p>
    </div>

    <div v-for="column in copy.footer.columns" :key="column.title">
      <b>{{ column.title }}</b>
      <a
        v-for="link in column.links"
        :key="link.label"
        :href="link.href"
        :target="link.external ? '_blank' : undefined"
        :rel="link.external ? 'noopener noreferrer' : undefined"
      >{{ link.label }}</a>
    </div>

    <div class="footer-bottom">{{ copy.footer.copyright }}</div>
  </footer>
</template>
