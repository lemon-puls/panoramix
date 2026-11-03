<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDialog, useMessage } from 'naive-ui'
import { NButton, NForm, NFormItem, NInput, NModal } from 'naive-ui'
import { useSnippetsStore } from '../stores/snippets'
import { useTerminalsStore } from '../stores/terminals'
import * as models from '../../wailsjs/go/models'

// 命令片段侧栏：CRUD + 点击发送到活跃终端（{host} 占位符替换）
const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const store = useSnippetsStore()
const terminals = useTerminalsStore()

const modal = reactive({ show: false, snippet: null as models.store.Snippet | null })
const form = reactive({ name: '', content: '' })
const saving = ref(false)

onMounted(() => {
  store.load().catch((e) => message.error(String(e)))
})

const canSend = computed(
  () => terminals.activeSessionId && terminals.activePane?.state === 'connected',
)

function openCreate() {
  form.name = ''
  form.content = ''
  modal.snippet = null
  modal.show = true
}

function openEdit(sn: models.store.Snippet) {
  form.name = sn.name
  form.content = sn.content
  modal.snippet = sn
  modal.show = true
}

async function save() {
  if (!form.name.trim() || !form.content.trim()) {
    message.warning(t('snippets.requiredTip'))
    return
  }
  saving.value = true
  try {
    const sn = modal.snippet
      ? new models.store.Snippet({
          id: modal.snippet.id,
          name: form.name.trim(),
          content: form.content.trim(),
          sort: modal.snippet.sort,
        })
      : new models.store.Snippet({ name: form.name.trim(), content: form.content.trim() })
    await store.save(sn)
    modal.show = false
    message.success(t('common.saved'))
  } catch (e) {
    message.error(String(e))
  } finally {
    saving.value = false
  }
}

function send(sn: models.store.Snippet) {
  const sessionId = terminals.activeSessionId
  if (!sessionId || terminals.activePane?.state !== 'connected') {
    message.warning(t('snippets.noActive'))
    return
  }
  const content = sn.content.replace('{host}', terminals.activePane?.hostName ?? '')
  void terminals.write(sessionId, content + '\r')
}

function askDelete(sn: models.store.Snippet) {
  dialog.warning({
    title: t('tree.deleteTitle'),
    content: t('snippets.deleteConfirm', { name: sn.name }),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await store.remove(sn.id)
        message.success(t('common.saved'))
      } catch (e) {
        message.error(String(e))
      }
    },
  })
}
</script>

<template>
  <div class="snippets">
    <div class="head">
      <span class="head-title">{{ t('snippets.title') }}</span>
      <n-button size="tiny" dashed @click="openCreate">{{ t('snippets.new') }}</n-button>
    </div>
    <div v-if="store.snippets.length === 0" class="empty">{{ t('snippets.empty') }}</div>
    <div v-else class="list">
      <div
        v-for="sn in store.snippets"
        :key="sn.id"
        class="item"
        :class="{ disabled: !canSend }"
        :title="sn.content"
        @click="send(sn)"
      >
        <div class="item-line">
          <span class="item-name">{{ sn.name }}</span>
          <span class="item-ops">
            <span class="op" @click.stop="openEdit(sn)">{{ t('menu.edit') }}</span>
            <span class="op danger" @click.stop="askDelete(sn)">{{ t('common.delete') }}</span>
          </span>
        </div>
        <div class="item-content">{{ sn.content }}</div>
      </div>
    </div>

    <n-modal
      v-model:show="modal.show"
      preset="card"
      :title="modal.snippet ? t('snippets.editTitle') : t('snippets.createTitle')"
      style="width: 480px"
    >
      <n-form label-placement="top">
        <n-form-item :label="t('snippets.name')">
          <n-input v-model:value="form.name" :placeholder="t('snippets.namePlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('snippets.content')">
          <n-input
            v-model:value="form.content"
            type="textarea"
            :rows="4"
            :placeholder="t('snippets.contentPlaceholder')"
          />
        </n-form-item>
        <p class="tip">{{ t('snippets.contentTip') }}</p>
        <div class="actions">
          <n-button quaternary @click="modal.show = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="saving" @click="save">
            {{ t('common.save') }}
          </n-button>
        </div>
      </n-form>
    </n-modal>
  </div>
</template>

<style scoped>
.snippets {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px 12px;
  background: var(--bg-panel);
}
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.head-title {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--text-muted);
  font-weight: 600;
}
.empty {
  font-size: 11px;
  color: var(--text-muted);
  padding: 4px 2px;
}
.list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: 150px;
  overflow: auto;
}
.item {
  padding: 5px 8px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background 0.12s ease;
}
.item:hover {
  background: var(--bg-hover);
}
.item.disabled {
  opacity: 0.45;
}
.item-line {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.item-name {
  font-size: 12px;
  color: var(--text-primary);
}
.item-ops {
  display: none;
  gap: 8px;
  font-size: 12px;
}
.item:hover .item-ops {
  display: inline;
}
.op {
  opacity: 0.7;
}
.op:hover {
  opacity: 1;
}
.op.danger {
  color: var(--danger);
}
.item-content {
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tip {
  margin-top: -8px;
  font-size: 12px;
  opacity: 0.5;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
