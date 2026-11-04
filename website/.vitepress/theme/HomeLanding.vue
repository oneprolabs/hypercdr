<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import SiteHeader from './SiteHeader.vue'
import SiteFooter from './SiteFooter.vue'
import { homeCopy } from './homeCopy'
import { useSiteLocale } from './useSiteLocale'

const locale = useSiteLocale()
const copy = computed(() => homeCopy[locale.value])

/** 控制台预览里三个节点的圆点配色，纯装饰。 */
const FLOW_DOT_CLASSES = ['', 'purple', 'green-dot']

const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

async function copyInstallCommand() {
  const command = copy.value.openSource.install.command
  try {
    await navigator.clipboard.writeText(command)
  } catch {
    // 非 HTTPS 或权限被拒时退回到临时 textarea
    const area = document.createElement('textarea')
    area.value = command
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.select()
    document.execCommand('copy')
    document.body.removeChild(area)
  }
  copied.value = true
  clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => (copied.value = false), 2000)
}

onBeforeUnmount(() => clearTimeout(copiedTimer))
</script>

<template>
  <div class="hcdr-home">
    <SiteHeader :copy="copy" />

    <main id="top">
      <!-- Hero -->
      <section class="hero section-shell">
        <div class="hero-copy">
          <a class="eyebrow" :href="copy.hero.eyebrowHref" target="_blank" rel="noopener noreferrer">
            {{ copy.hero.eyebrow }}
          </a>
          <h1>{{ copy.hero.titleLine1 }}<br /><em>{{ copy.hero.titleLine2 }}</em></h1>
          <p class="lede">{{ copy.hero.lede }}</p>
          <div class="hero-actions">
            <a class="button" :href="copy.hero.primaryHref" target="_blank" rel="noopener noreferrer">
              {{ copy.hero.primaryLabel }}
              <span v-if="copy.hero.primaryNote">{{ copy.hero.primaryNote }}</span>
            </a>
            <a class="button button-ghost" :href="copy.hero.secondaryHref" target="_blank" rel="noopener noreferrer">
              {{ copy.hero.secondaryLabel }} <span>↗</span>
            </a>
          </div>
          <div class="quality-list">
            <span v-for="item in copy.hero.quality" :key="item">{{ item }}</span>
          </div>
        </div>

        <div class="console-preview" :aria-label="copy.preview.ariaLabel">
          <div class="preview-top">
            <span class="window-dots">● ● ●</span>
            <span>{{ copy.preview.title }}</span>
            <span class="live">{{ copy.preview.status }}</span>
          </div>
          <div class="preview-body">
            <aside>
              <b>H</b>
              <i></i><i></i><i></i><i></i>
            </aside>
            <div class="preview-content">
              <div class="preview-heading">
                <span>{{ copy.preview.heading }}</span>
                <small>{{ copy.preview.updated }}</small>
              </div>
              <div class="metric-grid">
                <div v-for="(metric, index) in copy.preview.metrics" :key="metric.label">
                  <small>{{ metric.label }}</small>
                  <strong>{{ metric.value }}</strong>
                  <label :class="{ green: index < 2 }">{{ metric.note }}</label>
                </div>
              </div>
              <div class="flow-card">
                <div class="flow-title">
                  {{ copy.preview.flowTitle }}
                  <span>{{ copy.preview.flowAction }}</span>
                </div>
                <div class="flow">
                  <template v-for="(step, index) in copy.preview.flowSteps" :key="step.label">
                    <div>
                      <b :class="FLOW_DOT_CLASSES[index]">●</b>
                      <small>{{ step.label }}</small>
                      <strong>{{ step.value }}</strong>
                    </div>
                    <i v-if="index < copy.preview.flowSteps.length - 1">→</i>
                  </template>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- 产品能力 -->
      <section id="capabilities" class="section-shell section">
        <div class="section-intro">
          <p class="kicker">{{ copy.capabilities.kicker }}</p>
          <h2>{{ copy.capabilities.title }}</h2>
          <p>{{ copy.capabilities.description }}</p>
        </div>
        <div class="card-grid">
          <article v-for="item in copy.capabilities.items" :key="item.title">
            <span class="card-icon">{{ item.icon }}</span>
            <h3>{{ item.title }}</h3>
            <p>{{ item.description }}</p>
          </article>
        </div>
      </section>

      <!-- 应用场景 -->
      <section id="use-cases" class="section-shell section dark-section">
        <div class="section-intro">
          <p class="kicker">{{ copy.useCases.kicker }}</p>
          <h2>{{ copy.useCases.title }}</h2>
        </div>
        <div class="use-case-grid">
          <article v-for="item in copy.useCases.items" :key="item.title">
            <span>{{ item.index }}</span>
            <h3>{{ item.title }}</h3>
            <p>{{ item.description }}</p>
          </article>
        </div>
      </section>

      <!-- 工作原理 -->
      <section id="how-it-works" class="section-shell section">
        <div class="section-intro">
          <p class="kicker">{{ copy.howItWorks.kicker }}</p>
          <h2>{{ copy.howItWorks.title }}</h2>
          <p>{{ copy.howItWorks.description }}</p>
        </div>
        <div class="architecture">
          <template v-for="(step, index) in copy.howItWorks.steps" :key="step.title">
            <div>
              <b>{{ step.title }}</b>
              <small>{{ step.description }}</small>
            </div>
            <i v-if="index < copy.howItWorks.steps.length - 1">→</i>
          </template>
        </div>
      </section>

      <!-- 开源与安装 -->
      <section id="open-source" class="section-shell section install-section">
        <div>
          <p class="kicker">{{ copy.openSource.kicker }}</p>
          <h2>{{ copy.openSource.title }}</h2>
          <p>{{ copy.openSource.description }}</p>
          <div class="hero-actions">
            <a
              v-for="(action, index) in copy.openSource.actions"
              :key="action.label"
              :class="index === 0 ? 'button' : 'button button-ghost'"
              :href="action.href"
              :target="action.external ? '_blank' : undefined"
              :rel="action.external ? 'noopener noreferrer' : undefined"
            >{{ action.label }}</a>
          </div>
        </div>
        <div class="install-card">
          <div>
            <b>{{ copy.openSource.install.badge }}</b>
            <span>{{ copy.openSource.install.note }}</span>
            <button type="button" data-copy-command @click="copyInstallCommand">
              {{ copied ? copy.openSource.install.copied : copy.openSource.install.copy }}
            </button>
          </div>
          <code data-install-command>{{ copy.openSource.install.command }}</code>
          <small>{{ copy.openSource.install.footer }}</small>
        </div>
      </section>

      <!-- 联系我们 -->
      <section id="contact" class="section-shell section contact-section">
        <div class="section-intro">
          <p class="kicker">{{ copy.contact.kicker }}</p>
          <h2>{{ copy.contact.title }}</h2>
        </div>
        <div class="contact-grid">
          <article v-for="item in copy.contact.items" :key="item.title">
            <h3>{{ item.title }}</h3>
            <p>{{ item.description }}</p>
            <a :href="item.href">{{ item.linkLabel }}</a>
          </article>
        </div>
      </section>
    </main>

    <SiteFooter :copy="copy" />
  </div>
</template>
