<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NCheckbox, NEmpty, NModal, NSpin } from 'naive-ui'
import { PickDir, XshellImport, XshellScanDir } from '../../wailsjs/go/app/Service'
import type { xshell } from '../../wailsjs/go/models'

// Xshell 会话批量导入：选目录 → 预览勾选 → 导入为根分组下的主机。
// 密码/密钥不可迁移（Xshell 本机加密存储），首次连接时引导补录。
const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void; (e: 'imported', n: number): void }>()

const { t } = useI18n()
const message = useMessage()

const scanning = ref(false)
const importing = ref(false)
const dir = ref('')
const list = ref<xshell.Preview[]>([])
const checked = ref<Set<string>>(new Set())

const showModal = computed({
  get: () => props.show,
  set: (v) => emit('update:show', v),
})

async function pickAndScan() {
  const d = await PickDir()
  if (!d) return
  dir.value = d
  scanning.value = true
  try {
    const list0 = await XshellScanDir(d)
    list.value = list0 ?? []
    checked.value = new Set(list.value.filter((p) => p.valid).map((p) => p.file))
  } catch (e) {
    message.error(String(e))
  } finally {
    scanning.value = false
  }
}

function toggle(file: string, on: boolean) {
  if (on) checked.value.add(file)
  else checked.value.delete(file)
}

const validChecked = () => list.value.filter((p) => p.valid && checked.value.has(p.file))

async function doImport() {
  const sel = validChecked()
  if (sel.length === 0) {
    message.warning(t('import.noneSelected'))
    return
  }
  importing.value = true
  try {
    const n = await XshellImport(sel)
    message.success(t('import.done', { n }))
    emit('imported', n)
    emit('update:show', false)
  } catch (e) {
    message.error(String(e))
  } finally {
    importing.value = false
  }
}

function authLabel(p: xshell.Preview): string {
  switch (p.authType) {
    case 'key':
      return t('hostForm.authKey') + (p.keyName ? ` (${p.keyName})` : '')
    case 'keyboard_interactive':
      return t('hostForm.authKbd')
    default:
      return t('hostForm.authPassword')
  }
}
</script>

<template>
  <n-modal
    v-model:show="showModal"
    preset="card"
    :title="t('import.title')"
    style="width: 640px"
  >
    <p class="warning">{{ t('import.warning') }}</p>
    <div class="step">
      <n-button type="primary" :loading="scanning" @click="pickAndScan">
        {{ t('import.pickDir') }}
      </n-button>
      <span v-if="dir" class="dir">{{ dir }}</span>
    </div>

    <n-spin :show="scanning" size="small">
      <n-empty v-if="list.length === 0" size="small" :description="t('import.empty')" class="empty" />
      <div v-else class="list">
        <div v-for="p in list" :key="p.file" class="row" :class="{ invalid: !p.valid }">
          <n-checkbox
            :checked="checked.has(p.file)"
            :disabled="!p.valid"
            @update:checked="(v: boolean) => toggle(p.file, v)"
          />
          <span class="name">{{ p.name }}</span>
          <span class="host">{{ p.host }}:{{ p.port }}</span>
          <span class="user">{{ p.username }}</span>
          <span class="auth">{{ p.valid ? authLabel(p) : p.skipReason }}</span>
        </div>
      </div>
    </n-spin>

    <div class="actions">
      <n-button quaternary @click="emit('update:show', false)">{{ t('common.cancel') }}</n-button>
      <n-button type="primary" :loading="importing" :disabled="validChecked().length === 0" @click="doImport">
        {{ t('import.importBtn', { n: validChecked().length }) }}
      </n-button>
    </div>
  </n-modal>
</template>

<style scoped>
.warning {
  margin: 0 0 12px;
  padding: 8px 12px;
  border-radius: 4px;
  background: rgba(240, 198, 116, 0.12);
  color: #f0c674;
  font-size: 12px;
}
.step {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.dir {
  font-size: 12px;
  opacity: 0.6;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.empty {
  margin: 24px 0;
}
.list {
  max-height: 300px;
  overflow: auto;
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 6px;
  border-radius: 4px;
  font-size: 13px;
}
.row:hover {
  background: rgba(255, 255, 255, 0.06);
}
.row.invalid {
  opacity: 0.45;
}
.name {
  width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.host {
  flex: 1;
}
.user {
  width: 90px;
}
.auth {
  width: 140px;
  font-size: 12px;
  opacity: 0.7;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 16px;
}
</style>
