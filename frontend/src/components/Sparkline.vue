<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { EChartsCoreOption } from 'echarts/core'

// ECharts 迷你实时图：平滑曲线 + 渐变面积 + 悬浮十字提示 + 流式过渡动画
echarts.use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])

const props = defineProps<{
  labels: number[] // x 轴（毫秒时间戳）
  series: { label: string; values: (number | null)[]; color: string; fill?: string }[]
  height?: number
  yMax?: number
  unit?: string
}>()

const container = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null

function axisLabel(t: number): string {
  const d = new Date(t)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function mkSeries(s: (typeof props.series)[number]) {
  return {
    name: s.label,
    type: 'line' as const,
    showSymbol: false,
    smooth: 0.3,
    data: props.labels.map((t, i) => [t, s.values[i] ?? null]),
    lineStyle: { width: 1.6, color: s.color },
    itemStyle: { color: s.color },
    emphasis: { focus: 'series' as const },
    areaStyle: s.fill
      ? {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: `${s.color}55` },
            { offset: 1, color: `${s.color}05` },
          ]),
        }
      : undefined,
  }
}

function buildOption(): EChartsCoreOption {
  const dark = 'rgba(255,255,255,.45)'
  const gridLine = 'rgba(255,255,255,.06)'

  return {
    animationDuration: 300,
    animationDurationUpdate: 300,
    animationEasingUpdate: 'linear',
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(30,30,38,.96)',
      borderColor: 'rgba(255,255,255,.15)',
      textStyle: { color: 'rgba(255,255,255,.9)', fontSize: 11 },
      axisPointer: {
        type: 'cross',
        lineStyle: { color: 'rgba(255,255,255,.3)' },
        crossStyle: { color: 'rgba(255,255,255,.3)' },
        label: { backgroundColor: '#2a2a34' },
      },
      valueFormatter: (v: unknown) =>
        v == null ? '--' : `${Number(v).toFixed(1)}${props.unit ?? ''}`,
    },
    grid: { left: 6, right: 40, top: 8, bottom: 4, containLabel: false },
    xAxis: {
      type: 'time',
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { color: dark, fontSize: 10, hideOverlap: true, formatter: axisLabel },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'value',
      min: 0,
      max: props.yMax ?? undefined,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        color: dark,
        fontSize: 10,
        formatter: (v: number) => `${v}${props.unit ?? ''}`,
      },
      splitLine: { lineStyle: { color: gridLine } },
    },
    series: props.series.map(mkSeries),
  }
}

onMounted(() => {
  if (!container.value) return
  chart = echarts.init(container.value, undefined, {
    height: props.height ?? 110,
    renderer: 'canvas',
  })
  chart.setOption(buildOption())
})

// 数据更新 → 增量 setOption（ECharts 自动做状态过渡动画）
watch(
  () => [props.labels, props.series],
  () => chart?.setOption({ series: props.series.map(mkSeries) }),
  { flush: 'post' },
)

const ro = new ResizeObserver(() => chart?.resize())
onMounted(() => container.value && ro.observe(container.value))
onBeforeUnmount(() => {
  ro.disconnect()
  chart?.dispose()
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
