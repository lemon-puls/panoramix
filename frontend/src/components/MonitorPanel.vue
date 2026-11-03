<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NEmpty } from 'naive-ui'
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

// 系统监控面板：状态条 + 2×2 指标卡（大数字 + 迷你趋势）+ 磁盘 / GPU / 进程
const { t } = useI18n()
const terminals = useTerminalsStore()

const sessionId = computed(() => terminals.activeSessionId ?? '')
const connected = computed(() => terminals.activePane?.state === 'connected')
const stopped = ref(false)

// 图表数据源（响应式引用，整体换新触发子组件更新）
const labels = ref<number[]>([])
const cpuSeries = ref<(number | null)[]>([])
const memSeries = ref<(number | null)[]>([])
const netIn = ref<(number | null)[]>([])
const netOut = ref<(number | null)[]>([])
const ioR = ref<(number | null)[]>([])
const ioW = ref<(number | null)[]>([])

const now = ref<MonitorSample | null>(null)

function round1(v: number): number {
  return Math.round(v * 10) / 10
}

function onSample(sid: string) {
  if (sid !== sessionId.value) return
  stopped.value = getSessionState(sid) === 'stopped'
  const arr = getSessionSamples(sid)
  const last = arr.at(-1)
  if (!last) return
  now.value = last
  labels.value = arr.map((s) => s.ts)
  cpuSeries.value = arr.map((s) => round1(s.cpu.percent))
  memSeries.value = arr.map((s) =>
    round1(s.mem.total > 0 ? (s.mem.used / s.mem.total) * 100 : 0),
  )
  netIn.value = arr.map((s) => round1(s.net.recvBps / 1024))
  netOut.value = arr.map((s) => round1(s.net.sentBps / 1024))
  ioR.value = arr.map((s) => round1(s.diskIo.readBps / 1024))
  ioW.value = arr.map((s) => round1(s.diskIo.writeBps / 1024))
}

let off: (() => void) | undefined
onMounted(() => {
  off = onMonitorUpdate(onSample)
})
onBeforeUnmount(() => off?.())

watch(sessionId, (sid, old) => {
  if (old && old !== sid) {
    dropSession(old)
    labels.value = []
    cpuSeries.value = []
    memSeries.value = []
    netIn.value = []
    netOut.value = []
    ioR.value = []
    ioW.value = []
    now.value = null
  }
})

// ---------- 派生展示值 ----------
const memPct = computed(() => {
  const m = now.value
  return m && m.mem.total > 0 ? Math.round((m.mem.used / m.mem.total) * 100) : 0
})
const memSub = computed(() => {
  const m = now.value
  if (!m) return ''
  const swap = m.mem.swapTotal > 0 ? ` · swap ${Math.round((m.mem.swapUsed / m.mem.swapTotal) * 100)}%` : ''
  return `${formatSize(m.mem.used)} / ${formatSize(m.mem.total)}${swap}`
})
const cpuSub = computed(() => {
  const m = now.value
  if (!m) return ''
  return `${t('monitor.cores', { n: m.cpu.cores })} · load ${m.loadAvg[0].toFixed(2)}`
})
const netSub = computed(() => {
  const m = now.value
  if (!m) return ''
  return `↓ ${formatSize(Math.round(m.net.recvBps))}/s · ↑ ${formatSize(Math.round(m.net.sentBps))}/s`
})
const ioSub = computed(() => {
  const m = now.value
  if (!m) return ''
  return `R ${formatSize(Math.round(m.diskIo.readBps))}/s · W ${formatSize(Math.round(m.diskIo.writeBps))}/s`
})
const topProcs = computed(() => (now.value?.processes ?? []).slice(0, 8))

// 磁盘颜色：>85% 危险红，>70% 警告橙，其余青绿
function diskColor(pct: number): string {
  if (pct >= 85) return 'var(--danger)'
  if (pct >= 70) return 'var(--warning)'
  return 'var(--accent)'
}
</script>

<template>
  <div class="monitor">
    <n-empty v-if="!connected" size="small" :description="t('monitor.notConnected')" class="empty" />

    <template v-else>
      <!-- 状态条 -->
      <div class="status">
        <span class="dot" :class="{ off: stopped }" />
        <span class="status-text">{{ stopped ? t('monitor.offline') : t('monitor.online') }}</span>
        <span class="spacer" />
        <span class="status-meta">{{ cpuSub }}</span>
      </div>

      <div v-if="stopped" class="stopped">{{ t('monitor.stopped') }}</div>

      <!-- 2×2 指标卡 -->
      <div class="grid">
        <div class="card">
          <div class="card-head">
            <span class="card-label">CPU</span>
            <span class="card-value" style="color: var(--accent)">
              {{ (now?.cpu.percent ?? 0).toFixed(1) }}<small>%</small>
            </span>
          </div>
          <Sparkline
            :labels="labels"
            :series="[{ label: 'CPU', values: cpuSeries, color: 'var(--accent)' }]"
            :y-max="100"
            :height="46"
            unit="%"
          />
        </div>

        <div class="card">
          <div class="card-head">
            <span class="card-label">{{ t('monitor.memory') }}</span>
            <span class="card-value" style="color: #4a9ffd">{{ memPct }}<small>%</small></span>
          </div>
          <Sparkline
            :labels="labels"
            :series="[{ label: 'MEM', values: memSeries, color: '#4a9ffd' }]"
            :y-max="100"
            :height="46"
            unit="%"
          />
          <div class="card-sub">{{ memSub }}</div>
        </div>

        <div class="card">
          <div class="card-head">
            <span class="card-label">{{ t('monitor.network') }}</span>
            <span class="legend">
              <i style="background: #36ad6a" />↓
              <i style="background: var(--warning)" />↑
            </span>
          </div>
          <Sparkline
            :labels="labels"
            :series="[
              { label: '↓', values: netIn, color: '#36ad6a' },
              { label: '↑', values: netOut, color: 'var(--warning)' },
            ]"
            :height="46"
            unit=" KB/s"
          />
          <div class="card-sub">{{ netSub }}</div>
        </div>

        <div class="card">
          <div class="card-head">
            <span class="card-label">{{ t('monitor.diskIo') }}</span>
            <span class="legend">
              <i style="background: #722ed1" />R
              <i style="background: #d4380d" />W
            </span>
          </div>
          <Sparkline
            :labels="labels"
            :series="[
              { label: 'R', values: ioR, color: '#722ed1' },
              { label: 'W', values: ioW, color: '#d4380d' },
            ]"
            :height="46"
            unit=" KB/s"
          />
          <div class="card-sub">{{ ioSub }}</div>
        </div>
      </div>

      <!-- 磁盘 -->
      <div v-if="(now?.disks ?? []).length" class="block">
        <div class="block-title">{{ t('monitor.disks') }}</div>
        <div v-for="d in now!.disks" :key="d.mount" class="disk">
          <div class="disk-head">
            <span class="disk-mount">{{ d.mount }}</span>
            <span class="disk-pct" :style="{ color: diskColor(d.percent) }">
              {{ d.percent.toFixed(0) }}%
            </span>
          </div>
          <div class="disk-bar">
            <div
              class="disk-fill"
              :style="{
                width: Math.max(2, d.percent) + '%',
                background: `linear-gradient(90deg, ${diskColor(d.percent)}88, ${diskColor(d.percent)})`,
                boxShadow: `0 0 8px ${diskColor(d.percent)}66`,
              }"
            />
          </div>
          <div class="disk-detail">{{ formatSize(d.used) }} / {{ formatSize(d.total) }}</div>
        </div>
      </div>

      <!-- GPU -->
      <div v-if="(now?.gpu ?? []).length" class="block">
        <div class="block-title">GPU</div>
        <div v-for="g in now!.gpu" :key="g.index" class="gpu">
          <div class="gpu-line1">
            <span class="gpu-name">{{ g.name }}</span>
            <span class="gpu-temp" :style="{ color: g.tempC > 80 ? 'var(--danger)' : 'var(--warning)' }">
              {{ g.tempC.toFixed(0) }}°C
            </span>
          </div>
          <div class="gpu-bar">
            <div class="gpu-fill" :style="{ width: g.util + '%' }" />
          </div>
          <div class="gpu-detail">
            {{ t('monitor.gpuUtil') }} {{ g.util.toFixed(0) }}% ·
            {{ formatSize(g.memUsedMB * 1024 * 1024) }} / {{ formatSize(g.memTotMB * 1024 * 1024) }}
          </div>
        </div>
      </div>

      <!-- 进程 -->
      <div class="block">
        <div class="block-title">{{ t('monitor.topProcs') }}</div>
        <div v-if="topProcs.length === 0" class="proc-empty">{{ t('monitor.noData') }}</div>
        <div v-else class="procs">
          <div class="proc-head">
            <span class="pc-pid">PID</span>
            <span class="pc-name">{{ t('monitor.procName') }}</span>
            <span class="pc-num">CPU</span>
            <span class="pc-num">MEM</span>
          </div>
          <div v-for="p in topProcs" :key="p.pid" class="proc-row">
            <span class="pc-pid">{{ p.pid }}</span>
            <span class="pc-name" :title="p.name">{{ p.name }}</span>
            <span class="pc-num">
              <i class="mini-bar" :style="{ width: Math.min(100, p.cpu * 2) + '%' }" />
              <em>{{ p.cpu.toFixed(1) }}</em>
            </span>
            <span class="pc-num">
              <i class="mini-bar mem" :style="{ width: Math.min(100, p.mem * 2) + '%' }" />
              <em>{{ p.mem.toFixed(1) }}</em>
            </span>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.monitor {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 10px;
  height: 100%;
  overflow: auto;
}
.empty {
  margin-top: 40px;
}

/* ---------- 状态条 ---------- */
.status {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  opacity: 0.85;
}
.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 6px var(--accent);
  animation: pulse 2s ease-in-out infinite;
}
.dot.off {
  background: #888;
  box-shadow: none;
  animation: none;
}
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}
.status-text {
  color: rgba(255, 255, 255, 0.7);
}
.spacer {
  flex: 1;
}
.status-meta {
  color: rgba(255, 255, 255, 0.4);
  font-variant-numeric: tabular-nums;
}
.stopped {
  padding: 6px 10px;
  border-radius: 6px;
  background: rgba(240, 198, 116, 0.1);
  color: var(--warning);
  font-size: 11px;
}

/* ---------- 2×2 指标卡 ---------- */
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.card {
  background: rgba(255, 255, 255, 0.025);
  border: 1px solid var(--bg-hover);
  border-radius: 8px;
  padding: 8px 9px 6px;
  overflow: hidden;
  transition: border-color 0.2s;
}
.card:hover {
  border-color: rgba(255, 255, 255, 0.14);
}
.card-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 2px;
}
.card-label {
  font-size: 10px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: rgba(255, 255, 255, 0.45);
}
.card-value {
  font-size: 19px;
  font-weight: 650;
  line-height: 1;
  font-variant-numeric: tabular-nums;
}
.card-value small {
  font-size: 10px;
  font-weight: 400;
  margin-left: 1px;
  opacity: 0.7;
}
.card-sub {
  font-size: 10px;
  color: rgba(255, 255, 255, 0.4);
  margin-top: 3px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.legend {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 10px;
  color: rgba(255, 255, 255, 0.5);
}
.legend i {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  margin-left: 4px;
}

/* ---------- 区块 ---------- */
.block {
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid var(--bg-hover);
  border-radius: 8px;
  padding: 8px 10px;
}
.block-title {
  font-size: 10px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: rgba(255, 255, 255, 0.45);
  margin-bottom: 6px;
}

/* ---------- 磁盘 ---------- */
.disk {
  margin-bottom: 8px;
}
.disk:last-child {
  margin-bottom: 0;
}
.disk-head {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  margin-bottom: 3px;
}
.disk-mount {
  color: rgba(255, 255, 255, 0.8);
  font-variant-numeric: tabular-nums;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.disk-pct {
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.disk-bar {
  height: 5px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.07);
  overflow: hidden;
}
.disk-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.5s ease, background 0.5s ease;
}
.disk-detail {
  font-size: 10px;
  color: rgba(255, 255, 255, 0.35);
  margin-top: 2px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}

/* ---------- GPU ---------- */
.gpu {
  margin-bottom: 8px;
}
.gpu:last-child {
  margin-bottom: 0;
}
.gpu-line1 {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  margin-bottom: 3px;
}
.gpu-name {
  color: rgba(255, 255, 255, 0.85);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.gpu-temp {
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.gpu-bar {
  height: 5px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.07);
  overflow: hidden;
}
.gpu-fill {
  height: 100%;
  border-radius: 3px;
  background: linear-gradient(90deg, #722ed188, #722ed1);
  transition: width 0.5s ease;
}
.gpu-detail {
  font-size: 10px;
  color: rgba(255, 255, 255, 0.35);
  margin-top: 2px;
  font-variant-numeric: tabular-nums;
}

/* ---------- 进程 ---------- */
.procs {
  display: flex;
  flex-direction: column;
}
.proc-head,
.proc-row {
  display: grid;
  grid-template-columns: 52px 1fr 66px 66px;
  gap: 4px;
  align-items: center;
  font-size: 11px;
  padding: 2px 2px;
}
.proc-head {
  color: rgba(255, 255, 255, 0.35);
  font-size: 9px;
  letter-spacing: 0.06em;
  padding-bottom: 3px;
}
.proc-row {
  border-radius: 4px;
  transition: background 0.15s;
}
.proc-row:hover {
  background: rgba(255, 255, 255, 0.05);
}
.pc-pid {
  color: rgba(255, 255, 255, 0.4);
  font-variant-numeric: tabular-nums;
}
.pc-name {
  color: rgba(255, 255, 255, 0.85);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pc-num {
  position: relative;
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 4px;
  height: 16px;
  padding: 0 3px;
  border-radius: 3px;
  overflow: hidden;
  font-variant-numeric: tabular-nums;
}
.pc-num em {
  font-style: normal;
  color: rgba(255, 255, 255, 0.7);
  position: relative;
  z-index: 1;
}
.mini-bar {
  position: absolute;
  left: 0;
  height: 100%;
  border-radius: 3px;
  background: linear-gradient(90deg, rgba(99, 226, 183, 0.16), rgba(99, 226, 183, 0.03));
  transition: width 0.5s ease;
  z-index: 0;
}
.mini-bar.mem {
  background: linear-gradient(90deg, rgba(74, 159, 253, 0.15), rgba(74, 159, 253, 0.03));
}
.proc-empty {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.35);
}
</style>
