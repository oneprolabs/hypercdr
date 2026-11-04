<script setup lang="ts">
import DefaultTheme from 'vitepress/theme'
import { useData } from 'vitepress'
import HomeLanding from './HomeLanding.vue'

const { frontmatter } = useData()
</script>

<template>
  <!--
    首页走独立整页布局（自带 header/footer，不复用文档站导航）；
    其余页面都交给 VitePress 默认主题。
    新增整页布局时在这里加分支，frontmatter.layout 的值由页面 md 指定。
  -->
  <HomeLanding v-if="frontmatter.layout === 'hcdr-home'" />
  <DefaultTheme.Layout v-else />
</template>
