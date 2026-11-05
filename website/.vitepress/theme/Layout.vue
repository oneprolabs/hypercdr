<script setup lang="ts">
import { computed, watch } from 'vue'
import DefaultTheme from 'vitepress/theme'
import { useData } from 'vitepress'
import HomeLanding from './HomeLanding.vue'
import BackToTop from './BackToTop.vue'
import { rememberLocale } from './useLocalePreference'

const { frontmatter, localeIndex } = useData()
const isHome = computed(() => frontmatter.value.layout === 'hcdr-home')

/**
 * 文档站的语言切换走前端路由，不会整页重载，所以在语言变化时补记一次偏好。
 * 首页的语言下拉是整页跳转，由 LanguageMenu 自己在点击时记录。
 */
watch(localeIndex, (next) => rememberLocale(next))
</script>

<template>
  <!--
    首页走独立整页布局（自带 header/footer，不复用文档站导航）；
    其余页面都交给 VitePress 默认主题。
    新增整页布局时在这里加分支，frontmatter.layout 的值由页面 md 指定。
  -->
  <HomeLanding v-if="isHome" />
  <DefaultTheme.Layout v-else />
  <!-- 按钮是固定定位，挂在两种布局之外，官网和文档站共用一份 -->
  <BackToTop :home="isHome" />
</template>
