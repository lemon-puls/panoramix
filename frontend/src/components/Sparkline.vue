<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { EChartsCoreOption } from 'echarts/core'

// 迷你趋势图：无轴无图例的纯形状表达（紧凑卡片专用）。
// 数值与系列名由卡片头部展示，图内只留曲线与网格，悬浮给精确值。
echarts.use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])

const props = defineProps<{
  labels: number[] // x 轴（毫秒时间戳）
  series: { label: string; values: (number | null)[]; color: string }[]
  height?: number
  yMax?: number
  unit?: string
}>()

const container = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null

function mkSeries(s: (typeof props.series)[number]) {
  return {
    name: s.label,
    type: 'line' as const,
    showSymbol: false,
    symbolSize: 0,
    smooth: 0.25,
    data: props.labels.map((t, i) => [t, s.values[i] ?? null]),
    lineStyle: { width: 1.5, color: s.color },
    itemStyle: { color: s.color },
    emphasis: { focus: 'series' as const, lineStyle: { width: 2 } },
    areaStyle: {
      color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
        { offset: 0, color: `${s.color}40` },
        { offset: 1, color: `${s.color}00` },
      ]),
    },
  }
}

function buildOption(): EChartsCoreOption {
  return {
    animation: false, // 高频刷新下图内动画反而抖动；数值变化由外部数字动画承担
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(24,24,32,.94)',
      borderColor: 'rgba(255,255,255,.12)',
      borderWidth: 1,
      padding: [6, 10],
      textStyle: { color: 'rgba(255,255,255,.9)', fontSize: 11 },
      axisPointer: {
        type: 'line',
        lineStyle: { color: 'rgba(255,255,255,.25)', type: 'dashed' },
      },
      formatter: (params: unknown) => {
        const arr = params as { value: [number, number]; seriesName: string; color: string }[]
        if (!arr?.length) return ''
        const t = new Date(arr[0].value[0])
        const hh = String(t.getHours()).padStart(2, '0')
        const mm = String(t.getMinutes()).padStart(2, '0')
        const ss = String(t.getSeconds()).padStart(2, '0')
        const rows = arr
          .map(
            (p) =>
              `<div style="display:flex;justify-content:space-between;gap:16px">
                 <span><i style="display:inline-block;width:6px;height:6px;border-radius:50%;background:${p.color};margin-right:5px"></i>${p.seriesName}</span>
                 <b>${p.value[1] == null ? '--' : Number(p.value[1]).toFixed(1)}${props.unit ?? ''}</b>
               </div>`,
          )
          .join('')
        return `<div style="font-size:10px;opacity:.55;margin-bottom:3px">${hh}:${mm}:${ss}</div>${rows}`
      },
    },
    grid: { left: 0, right: 0, top: 0, bottom: 0 },
    xAxis: {
      type: 'time',
      show: false,
      boundaryGap: ['0%', '0%'],
    },
    yAxis: {
      type: 'value',
      show: false,
      min: 0,
      max: props.yMax ?? undefined,
    },
    series: props.series.map(mkSeries),
  }
}

onMounted(() => {
  if (!container.value) return
  chart = echarts.init(container.value, undefined, {
    height: props.height ?? 56,
    renderer: 'canvas',
  })
  chart.setOption(buildOption())
})

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
  <div ref="container" class="spark" :style="{ height: (height ?? 56) + 'px' }" />
</template>

<style scoped>
.spark {
  width: 100%;
  cursor: crosshair;
}
</style>
