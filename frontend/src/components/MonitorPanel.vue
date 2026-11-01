<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NEmpty, NTag } from 'naive-ui'
import {
  dropSession,
  getSessionSamples,
  getSessionState,
  onMonitorUpdate,
  type MonitorSample,
} from '../stores/monitor'
import { useTerminalsStore } from '../stores/terminals'
import { formatSize } from '../stores/transfers'
import Sparkline from './Sparkline.vue'

// 系统监控面板：指标卡 + uPlot 实时曲线 + GPU + Top 进程表
const { t } = useI18n()
const terminals = useTerminalsStore()

const sessionId = computed(() => terminals.activeSessionId ?? '')
const connected = computed(() => terminals.activeTab?.state === 'connected')
const stopped = ref(false)

// uPlot 数据源（非响应式，靠 tick 通知刷新）
const labels: number[] = []
const cpuSeries: (number | null)[] = []
const memSeries: (number | null)[] = []
const netIn: (number | null)[] = []
const netOut: (number | null)[] = []
const ioR: (number | null)[] = []
const ioW: (number | null)[] = []

// 展示值（响应式）
const now = ref<MonitorSample | null>(null)
const tick = ref(0)

function onSample(sid: string) {
  if (sid !== sessionId.value) return
  stopped.value = getSessionState(sid) === 'stopped'
  const arr = getSessionSamples(sid)
  const last = arr.at(-1)
  if (!last) return
  now.value = last
  // 以最新采样重建序列（环形缓冲即真相源，成本低：≤300 点）
  labels.length = 0
  cpuSeries.length = 0
  memSeries.length = 0
  netIn.length = 0
  netOut.length = 0
  ioR.length = 0
  ioW.length = 0
  for (const s of arr) {
    labels.push(s.ts)
    cpuSeries.push(round1(s.cpu.percent))
    const memPct = s.mem.total > 0 ? (s.mem.used / s.mem.total) * 100 : 0
    memSeries.push(round1(memPct))
    netIn.push(round1(s.net.recvBps / 1024))
    netOut.push(round1(s.net.sentBps / 1024))
    ioR.push(round1(s.diskIo.readBps / 1024))
    ioW.push(round1(s.diskIo.writeBps / 1024))
  }
  tick.value++
}

function round1(v: number): number {
  return Math.round(v * 10) / 10
}

let off: (() => void) | undefined
onMounted(() => {
  off = onMonitorUpdate(onSample)
})
onBeforeUnmount(() => {
  off?.()
})

// 会话切换/关闭 → 清空本地序列
watch(sessionId, (sid, old) => {
  if (old && old !== sid) {
    dropSession(old)
    labels.length = 0
    cpuSeries.length = 0
    memSeries.length = 0
    netIn.length = 0
    netOut.length = 0
    ioR.length = 0
    ioW.length = 0
    now.value = null
    tick.value++
  }
})

const memPct = computed(() => {
  const m = now.value
  return m && m.mem.total > 0 ? Math.round((m.mem.used / m.mem.total) * 100) : 0
})
const swapPct = computed(() => {
  const m = now.value
  return m && m.mem.swapTotal > 0 ? Math.round((m.mem.swapUsed / m.mem.swapTotal) * 100) : 0
})
const topProcs = computed(() => (now.value?.processes ?? []).slice(0, 8))
</script>

<template>
  <div class="monitor">
    <n-empty
      v-if="!connected"
      size="small"
      :description="t('monitor.notConnected')"
      class="empty"
    />
    <template v-else>
      <div v-if="stopped" class="stopped">{{ t('monitor.stopped') }}</div>

      <div class="cards">
        <div class="card">
          <div class="card-head">
            <span class="card-label">CPU</span>
            <span class="card-value">{{ (now?.cpu.percent ?? 0).toFixed(1) }}%</span>
          </div>
          <Sparkline
            :labels="labels"
            :series="[{ label: 'CPU %', values: cpuSeries, color: '#63e2b7', fill: '1' }]"
            :y-max="100"
            unit="%"
            :height="70"
          />
          <div class="sub">{{ t('monitor.cores', { n: now?.cpu.cores ?? 0 }) }} · load {{ (now?.loadAvg[0] ?? 0).toFixed(2) }}</div>
        </div>

        <div class="card">
          <div class="card-head">
            <span class="card-label">{{ t('monitor.memory') }}</span>
            <span class="card-value">{{ memPct }}%</span>
          </div>
          <Sparkline
            :labels="labels"
            :series="[{ label: 'MEM %', values: memSeries, color: '#4a9ffd', fill: '1' }]"
            :y-max="100"
            unit="%"
            :height="70"
          />
          <div class="sub">
            {{ formatSize(now?.mem.used ?? 0) }} / {{ formatSize(now?.mem.total ?? 0) }}
            <template v-if="swapPct > 0"> · swap {{ swapPct }}%</template>
          </div>
        </div>

        <div class="card">
          <div class="card-head">
            <span class="card-label">{{ t('monitor.network') }}</span>
            <span class="card-value small">
              ↓{{ formatSize(Math.round(now?.net.recvBps ?? 0)) }}/s · ↑{{ formatSize(Math.round(now?.net.sentBps ?? 0)) }}/s
            </span>
          </div>
          <Sparkline
            :labels="labels"
            :series="[
              { label: '↓ KB/s', values: netIn, color: '#36ad6a' },
              { label: '↑ KB/s', values: netOut, color: '#f0a020' },
            ]"
            unit=" KB/s"
            :height="70"
          />
        </div>

        <div class="card">
          <div class="card-head">
            <span class="card-label">{{ t('monitor.diskIo') }}</span>
            <span class="card-value small">
              R {{ formatSize(Math.round(now?.diskIo.readBps ?? 0)) }}/s · W {{ formatSize(Math.round(now?.diskIo.writeBps ?? 0)) }}/s
            </span>
          </div>
          <Sparkline
            :labels="labels"
            :series="[
              { label: 'R KB/s', values: ioR, color: '#722ed1' },
              { label: 'W KB/s', values: ioW, color: '#d4380d' },
            ]"
            unit=" KB/s"
            :height="70"
          />
        </div>
      </div>

      <!-- 磁盘用量 -->
      <div v-if="(now?.disks ?? []).length" class="section">
        <div class="section-title">{{ t('monitor.disks') }}</div>
        <div v-for="d in now!.disks" :key="d.mount" class="disk-row">
          <span class="disk-mount">{{ d.mount }}</span>
          <div class="disk-bar">
            <div class="disk-fill" :class="{ warn: d.percent > 80 }" :style="{ width: d.percent + '%' }" />
          </div>
          <span class="disk-pct">{{ d.percent.toFixed(0) }}%</span>
          <span class="disk-detail">{{ formatSize(d.used) }} / {{ formatSize(d.total) }}</span>
        </div>
      </div>

      <!-- GPU -->
      <div v-if="(now?.gpu ?? []).length" class="section">
        <div class="section-title">GPU</div>
        <div v-for="g in now!.gpu" :key="g.index" class="gpu-card">
          <div class="gpu-head">
            <span class="gpu-name">#{{ g.index }} {{ g.name }}</span>
            <n-tag size="small" :bordered="false">{{ g.tempC.toFixed(0) }}°C</n-tag>
          </div>
          <div class="gpu-line">
            {{ t('monitor.gpuUtil') }} {{ g.util.toFixed(0) }}% ·
            {{ t('monitor.gpuMem') }} {{ formatSize(g.memUsedMB * 1024 * 1024) }} / {{ formatSize(g.memTotMB * 1024 * 1024) }}
          </div>
        </div>
      </div>

      <!-- Top 进程 -->
      <div class="section">
        <div class="section-title">{{ t('monitor.topProcs') }}</div>
        <div v-if="topProcs.length === 0" class="proc-empty">{{ t('monitor.noData') }}</div>
        <div v-for="p in topProcs" :key="p.pid" class="proc-row">
          <span class="proc-pid">{{ p.pid }}</span>
          <span class="proc-name">{{ p.name }}</span>
          <span class="proc-cpu">CPU {{ p.cpu.toFixed(1) }}%</span>
          <span class="proc-mem">MEM {{ p.mem.toFixed(1) }}%</span>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.monitor {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  height: 100%;
  overflow: auto;
}
.empty {
  margin-top: 40px;
}
.stopped {
  padding: 6px 10px;
  border-radius: 4px;
  background: rgba(240, 198, 116, 0.12);
  color: #f0c674;
  font-size: 12px;
}
.cards {
  display: grid;
  grid-template-columns: 1fr;
  gap: 8px;
}
.card {
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 6px;
  padding: 8px;
}
.card-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 4px;
}
.card-label {
  font-size: 12px;
  opacity: 0.6;
}
.card-value {
  font-size: 18px;
  font-weight: 600;
  color: #63e2b7;
}
.card-value.small {
  font-size: 12px;
  font-weight: 400;
}
.sub {
  font-size: 11px;
  opacity: 0.5;
  margin-top: 2px;
}
.section {
  margin-top: 4px;
}
.section-title {
  font-size: 12px;
  opacity: 0.6;
  margin-bottom: 4px;
}
.disk-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  padding: 2px 0;
}
.disk-mount {
  width: 110px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.disk-bar {
  flex: 1;
  height: 6px;
  background: rgba(255, 255, 255, 0.1);
  border-radius: 3px;
  overflow: hidden;
}
.disk-fill {
  height: 100%;
  background: #63e2b7;
}
.disk-fill.warn {
  background: #f0a020;
}
.disk-pct {
  width: 36px;
  text-align: right;
}
.disk-detail {
  width: 130px;
  text-align: right;
  opacity: 0.5;
  font-size: 11px;
}
.gpu-card {
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 6px;
  padding: 6px 8px;
  margin-bottom: 4px;
  font-size: 12px;
}
.gpu-head {
  display: flex;
  justify-content: space-between;
  margin-bottom: 2px;
}
.gpu-name {
  font-weight: 600;
}
.gpu-line {
  opacity: 0.7;
}
.proc-row {
  display: flex;
  gap: 8px;
  font-size: 12px;
  padding: 2px 4px;
  border-radius: 3px;
}
.proc-row:hover {
  background: rgba(255, 255, 255, 0.05);
}
.proc-pid {
  width: 56px;
  opacity: 0.5;
}
.proc-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.proc-cpu {
  width: 84px;
  text-align: right;
}
.proc-mem {
  width: 84px;
  text-align: right;
  opacity: 0.7;
}
.proc-empty {
  font-size: 12px;
  opacity: 0.4;
}
</style>
