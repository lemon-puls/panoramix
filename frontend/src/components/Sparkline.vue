<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'

// uPlot 迷你图包装：命令式 setData（数据来自监控总线，非响应式）
const props = defineProps<{
  labels: number[] // x 轴（毫秒时间戳）
  series: { label: string; values: (number | null)[]; color: string; fill?: string }[]
  height?: number
  yMax?: number
  unit?: string
}>()

const container = ref<HTMLDivElement>()
let up: uPlot | null = null

function buildData(): uPlot.AlignedData {
  const n = props.labels.length
  const cols: unknown[] = [props.labels]
  for (const s of props.series) {
    while (s.values.length < n) s.values.unshift(null)
    cols.push(s.values)
  }
  return cols as uPlot.AlignedData
}

onMounted(() => {
  if (!container.value) return
  const h = props.height ?? 110
  const opts: uPlot.Options = {
    width: container.value.clientWidth,
    height: h,
    cursor: { show: false },
    legend: { show: true, live: false },
    scales: {
      y: props.yMax != null ? { range: [0, props.yMax] } : { auto: true },
    },
    axes: [
      { stroke: 'rgba(255,255,255,.35)', grid: { stroke: 'rgba(255,255,255,.07)' }, ticks: { show: false } },
      { stroke: 'rgba(255,255,255,.35)', grid: { stroke: 'rgba(255,255,255,.07)' }, side: 1, size: 42 },
    ],
    series: [
      {},
      ...props.series.map((s) => ({
        label: s.label,
        stroke: s.color,
        width: 1.5,
        fill: s.fill ? `${s.color}33` : undefined,
        points: { show: false },
        value: (_u: unknown, v: number | null) =>
          v == null ? '--' : `${v.toFixed(1)}${props.unit ?? ''}`,
      })),
    ],
  }
  up = new uPlot(opts, buildData(), container.value)
})

// 数据或尺寸变化 → 重设
watch(
  () => props.labels.length,
  () => up?.setData(buildData()),
)

const ro = new ResizeObserver(() => {
  if (up && container.value) {
    up.setSize({ width: container.value.clientWidth, height: props.height ?? 110 })
  }
})
onMounted(() => container.value && ro.observe(container.value))
onBeforeUnmount(() => {
  ro.disconnect()
  up?.destroy()
})
</script>

<template>
  <div ref="container" class="spark" :style="{ height: (height ?? 110) + 'px' }" />
</template>

<style scoped>
.spark {
  width: 100%;
}
</style>
