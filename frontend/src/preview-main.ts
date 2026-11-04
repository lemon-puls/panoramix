// 临时视觉预览入口（仅本地调试用，验证后删除）
// mock Wails runtime → 注入假样本 → 渲染 MonitorPanel
import { createApp, h } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { NConfigProvider, NDialogProvider, NMessageProvider, darkTheme } from 'naive-ui'
import { zhCN, dateZhCN } from 'naive-ui'

type Handler = (...args: any[]) => void
const handlers = new Map<string, Handler[]>()
let lastAIRequestId = 'preview-ai-req'

// 必须在任何模块 import monitor.ts 之前挂载 mock
;(window as any).runtime = {
  EventsOn(name: string, cb: Handler) {
    const list = handlers.get(name) ?? []
    list.push(cb)
    handlers.set(name, list)
    return () => {}
  },
  EventsOnMultiple(name: string, cb: Handler) {
    return (window as any).runtime.EventsOn(name, cb)
  },
  EventsOff() {},
  EventsEmit() {},
  LogInfo() {},
  LogWarning() {},
  LogError() {},
  OnFileDrop() {},
  OnFileDropOff() {},
  ClipboardGetText() { return '' },
  ClipboardSetText() { return true },
}

// mock go 绑定：仅 preview 用（FilePanel 依赖本地/远程列目录与传输列表）
const mockService: Record<string, (...args: any[]) => Promise<any>> = {
  LocalHome: async () => '/home/demo',
  LocalRoots: async () => ['C:\\', 'D:\\'],
  LocalParent: async (p: string) => p.split('/').slice(0, -1).join('/') || '/',
  LocalListDir: async () =>
    Array.from({ length: 14 }, (_, i) => ({
      name: i % 4 === 0 ? `folder-${i}` : `local-file-${i}.tar.gz`,
      path: `/home/demo/${i}`,
      isDir: i % 4 === 0,
      size: (i + 1) * 1024 * 512,
      mtime: new Date().toISOString(),
    })),
  SftpHome: async () => '/root',
  SftpListDir: async () =>
    Array.from({ length: 18 }, (_, i) => ({
      name: i % 5 === 0 ? `remote-dir-${i}` : `app.log.${i}`,
      path: `/root/${i}`,
      isDir: i % 5 === 0,
      size: (i + 1) * 1024 * 300,
      mode: '-rw-r--r--',
      mtime: new Date().toISOString(),
    })),
  ListTransfers: async () => [],
  Ping: async () => 'pong',
  IsSessionLogEnabled: async () => false,
  SetSessionLogEnabled: async () => undefined,
  IsCwdSyncEnabled: async () => false,
  SetCwdSyncEnabled: async () => undefined,
  GetTransferSpeedLimit: async () => 0,
  SetTransferSpeedLimit: async () => undefined,
  ListGroups: async () => [],
  ListHosts: async () => [],
  ListSnippets: async () => [],
  ListDBConns: async () => (new URLSearchParams(location.search).get('redis') === '1'
    ? [
        { id: 'redis-1', name: '本地 Redis', kind: 'redis', addr: '127.0.0.1', port: 6379, username: '', dbName: '0', hasSecret: true, sort: 0 },
        { id: 'mysql-1', name: '本地 MySQL', kind: 'mysql', addr: '127.0.0.1', port: 3306, username: 'root', dbName: 'demo', hasSecret: true, sort: 1 },
      ]
    : [
        { id: 'mysql-1', name: '本地 MySQL', kind: 'mysql', addr: '127.0.0.1', port: 3306, username: 'root', dbName: 'demo', hasSecret: true, sort: 0 },
        { id: 'redis-1', name: '本地 Redis', kind: 'redis', addr: '127.0.0.1', port: 6379, username: '', dbName: '0', hasSecret: true, sort: 1 },
      ]),
  DBListDatabases: async () => ['demo', 'information_schema', 'mysql'],
  DBListTables: async () => [
    { name: 'users', isView: false, rows: 1280, comment: '用户表' },
    { name: 'orders', isView: false, rows: 45210, comment: '' },
    { name: 'v_user_summary', isView: true, rows: 0, comment: '视图' },
  ],
  DBDescribeTable: async () => [
    { field: 'id', type: 'bigint unsigned', null: 'NO', key: 'PRI', default: undefined, extra: 'auto_increment', comment: '' },
    { field: 'name', type: 'varchar(64)', null: 'NO', key: 'UNI', default: '', extra: '', comment: '用户名' },
    { field: 'created_at', type: 'datetime', null: 'YES', key: '', default: 'CURRENT_TIMESTAMP', extra: '', comment: '' },
  ],
  DBShowCreateTable: async () => 'CREATE TABLE `users` (\n  `id` bigint unsigned NOT NULL AUTO_INCREMENT,\n  `name` varchar(64) NOT NULL,\n  PRIMARY KEY (`id`)\n) ENGINE=InnoDB;',
  // AI 预览 mock：不依赖真实模型，用于验证侧栏布局与交互
  AIGetConfig: async () => {
    const params = new URLSearchParams(location.search)
    if (params.get('ai_setup') === '1') {
      return {
        enabled: false,
        provider: 'ollama',
        baseUrl: 'http://127.0.0.1:11434/v1',
        model: '',
        temperature: 0.1,
        timeoutSec: 120,
        allowWrite: false,
        allowInsecureTls: false,
        concurrency: 1,
        hasKey: false,
        endpoint: 'http://127.0.0.1:11434/v1',
        endpointHash: 'ab12cd34ef56ab78',
        scope: 'local',
        cloudConsent: false,
        warnings: [],
      }
    }
    const cloud = params.get('ai_cloud') === '1'
    const lan = params.get('ai_lan') === '1'
    const endpoint = cloud
      ? 'https://api.example.com/v1'
      : lan
        ? 'http://192.168.1.10:11434/v1'
        : 'http://127.0.0.1:11434/v1'
    return {
      enabled: true,
      provider: cloud ? 'openai-compatible' : 'ollama',
      baseUrl: endpoint,
      model: 'qwen2.5-coder:7b',
      temperature: 0.1,
      timeoutSec: 120,
      allowWrite: false,
      allowInsecureTls: false,
      concurrency: 1,
      hasKey: cloud,
      endpoint,
      endpointHash: cloud ? 'ff00ff00ff00ff00' : lan ? '1122334455667788' : 'ab12cd34ef56ab78',
      scope: cloud ? 'cloud' : lan ? 'lan' : 'local',
      cloudConsent: params.get('ai_consent') === '1',
      warnings: [],
    }
  },
  AIPrepare: async (req: any) => {
    const params = new URLSearchParams(location.search)
    const cloud = params.get('ai_cloud') === '1'
    const lan = params.get('ai_lan') === '1'
    const endpoint = cloud
      ? 'https://api.example.com/v1'
      : lan
        ? 'http://192.168.1.10:11434/v1'
        : 'http://127.0.0.1:11434/v1'
    lastAIRequestId = req?.requestId ?? 'preview-ai-req'
    return {
      prepareId: 'prep-preview',
      requestId: lastAIRequestId,
      payload:
        '[system]\nYou are a MySQL 8.0 SQL assistant embedded in a database client.\n\n[user]\nMode: ' +
        (req?.mode ?? 'generate') +
        '\nDatabase: demo\nMySQL version: 8.0.36\nSelected tables: users, orders\nQuestion:\n' +
        (req?.question ?? '') +
        '\n\n<schema>\n### TABLE `users`\nCREATE TABLE `users` (\n  `id` bigint unsigned NOT NULL,\n  `email` varchar(128) NOT NULL,\n  PRIMARY KEY (`id`)\n) ENGINE=InnoDB\n\n### TABLE `orders`\nCREATE TABLE `orders` (\n  `id` bigint unsigned NOT NULL,\n  `user_id` bigint unsigned NOT NULL,\n  KEY `idx_user` (`user_id`)\n) ENGINE=InnoDB\n</schema>\n',
      payloadHash: 'ab12cd34ef56ab78',
      promptChars: 1860,
      schemaChars: 612,
      tables: [
        { name: 'orders', chars: 302, excluded: false },
        { name: 'users', chars: 310, excluded: false },
        { name: 'v_user_summary', chars: 0, excluded: true, reason: 'view' },
      ],
      warnings: [{ code: 'foreign_key_unselected', detail: 'payments' }],
      endpoint,
      endpointHash: cloud ? 'ff00ff00ff00ff00' : lan ? '1122334455667788' : 'ab12cd34ef56ab78',
      scope: cloud ? 'cloud' : lan ? 'lan' : 'local',
      provider: cloud ? 'openai-compatible' : 'ollama',
      model: 'qwen2.5-coder:7b',
      needsConsent: (cloud || lan) && params.get('ai_consent') !== '1',
      expiresInSec: 300,
      allowWrite: false,
      mode: req?.mode ?? 'generate',
      sensitiveHints:
        params.get('ai_sensitive') === '1'
          ? [
              { field: 'question', kind: 'password_assignment' },
              { field: 'question', kind: 'email_address' },
            ]
          : [],
    }
  },
  AIGenerate: async () => {
    emit('ai:status', { requestId: lastAIRequestId, phase: 'request' })
    emit('ai:delta', { requestId: lastAIRequestId, text: 'SELECT u.id, u.email\n' })
    emit('ai:delta', { requestId: lastAIRequestId, text: "FROM users u\nWHERE u.last_login < NOW() - INTERVAL 7 DAY\nORDER BY u.id;" })
    emit('ai:status', { requestId: lastAIRequestId, phase: 'done' })
    return {
      sql: 'SELECT u.id, u.email\nFROM users u\nWHERE u.last_login < NOW() - INTERVAL 7 DAY\nORDER BY u.id;',
      explanation: '按最近登录时间筛选 7 天内未登录的用户；结果行需人工复核后再执行。',
      warnings: [],
      provider: 'ollama',
      model: 'qwen2.5-coder:7b',
      durationMs: 860,
      promptChars: 1860,
      responseChars: 210,
      isWrite: false,
      multiStatement: false,
      dangerous: false,
      uncertain: false,
      insertAllowed: true,
      insertBlockReason: '',
    }
  },
  AICancel: async () => undefined,
  AISaveConfig: async () => undefined,
  AITestConnection: async () => undefined,
  AIDeleteKey: async () => undefined,
  AIClearConsents: async () => undefined,
  DBQuery: async () => ({ columns: ['id', 'name', 'created_at'], rows: [['1', 'alice', '2026-01-01 10:00:00'], ['2', null, '2026-01-02 11:30:00']], affected: 0, durationMs: 3, truncated: false }),
  RedisScanKeys: async () => ({ keys: [
    { key: 'user:1:name', ttl: 120, type: 'string' },
    { key: 'user:1:email', ttl: 120, type: 'string' },
    { key: 'user:2:name', ttl: -1, type: 'string' },
    { key: 'session:abc', ttl: 3600, type: 'hash' },
    { key: 'session:def', ttl: 3600, type: 'hash' },
    { key: 'queue:jobs', ttl: -1, type: 'list' },
    { key: 'stats:visits', ttl: -1, type: 'string' },
  ], cursor: 0 }),
  RedisGetValue: async () => ({ key: 'user:1', type: 'string', ttl: 120, str: 'alice', total: 1, truncated: false }),
  RedisSetTTL: async () => undefined,
  RedisDeleteKey: async () => undefined,
  RedisSetString: async () => undefined,
  SaveDBConn: async () => ({}),
  DeleteDBConn: async () => undefined,
  TestDBConn: async () => undefined,
  ListRemoteEdits: async () => [],
  SftpMkdir: async () => undefined,
  SftpRemove: async () => undefined,
  SftpRename: async () => undefined,
  SftpDownload: async () => 'batch',
  SftpUpload: async () => 'batch',
  PickUploadFiles: async () => [],
  PickUploadFile: async () => '',
  PickDir: async () => '',
  PickSavePath: async () => '',
  TransferCancel: async () => undefined,
  TransferRetry: async () => undefined,
  SetTransferPolicy: async () => undefined,
  PauseTransfer: async () => undefined,
  ResumeTransfer: async () => undefined,
}
;(window as any).go = { app: { Service: mockService } }

function emit(name: string, payload: unknown) {
  for (const cb of handlers.get(name) ?? []) cb(payload)
}

async function main() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const { i18n, setLocale } = await import('./i18n')
  const lang = new URLSearchParams(location.search).get('locale')
  if (lang === 'en-US') setLocale('en-US')
  const { useTerminalsStore } = await import('./stores/terminals')
  const { useFilesStore } = await import('./stores/files')
  const params = new URLSearchParams(location.search)
  if (params.get('app') === '1') {
    const { default: AppRoot } = await import('./App.vue')
    const { mainView } = await import('./stores/view')
    if (params.get('db') === '1') mainView.value = 'db'
    const appRoot = createApp({
      render: () =>
        h(NConfigProvider, { theme: darkTheme, locale: zhCN, dateLocale: dateZhCN }, {
          default: () =>
            h(NMessageProvider, null, {
              default: () => h(NDialogProvider, null, { default: () => h(AppRoot) }),
            }),
        }),
    })
    appRoot.use(pinia)
    appRoot.use(i18n)
    appRoot.config.errorHandler = (err) => {
      const el = document.createElement('pre')
      el.id = 'preview-error'
      el.style.cssText = 'color:#e88080;padding:16px;white-space:pre-wrap;font-size:12px'
      el.textContent = 'RENDER ERROR: ' + (err instanceof Error ? `${err.name}: ${err.message}\n${err.stack ?? ''}` : String(err))
      document.body.appendChild(el)
    }
    appRoot.mount('#app')
    return
  }
  const panel = params.get('panel')
  const { default: Component } = panel === 'files'
    ? await import('./components/FilePanel.vue')
    : panel === 'db'
      ? await import('./components/DBView.vue')
      : await import('./components/MonitorPanel.vue')
  const terminals = useTerminalsStore()
  terminals.tabs.push({
    id: 'preview-tab',
    panes: [{ sessionId: 'demo', hostId: 'h1', hostName: '阿里云', state: 'connected', exitReason: '' }],
    activePaneId: 'demo',
    direction: 'row',
  })
  terminals.activeTabId = 'preview-tab'

  const app = createApp({
    render: () =>
      h(NConfigProvider, { theme: darkTheme, locale: zhCN, dateLocale: dateZhCN }, {
        default: () =>
          h(NMessageProvider, null, {
            default: () =>
              h(NDialogProvider, null, {
                default: () =>
                  h('div', { style: 'width:640px;height:900px;background:#18181c' }, [
                    h(Component),
                  ]),
              }),
          }),
      }),
  })
  app.use(pinia)
  app.use(i18n)
  app.mount('#app')

  // 生成 300 个点的模拟数据（CPU 有起伏、内存缓慢上升、网络尖峰）
  const now = Date.now()
  const push = (i: number) => {
    const t = now - (300 - i) * 2000
    const cpu = 25 + 18 * Math.sin(i / 11) + 8 * Math.sin(i / 3.3) + Math.random() * 4
    const memUsed = (0.42 + i * 0.0006) * 16384 * 1024 * 1024
    emit('monitor:sample', {
      hostId: 'h1',
      sessionId: 'demo',
      ts: t,
      cpu: { percent: Math.max(1, cpu), cores: 2 },
      loadAvg: [0.31, 0.28, 0.21],
      mem: {
        total: 16384 * 1024 * 1024,
        used: memUsed,
        available: 16384 * 1024 * 1024 - memUsed,
        swapTotal: 0,
        swapUsed: 0,
      },
      disks: [
        { mount: '/', total: 39 * 1024 ** 3, used: 7.8 * 1024 ** 3, percent: 20 },
        { mount: '/boot/efi', total: 197 * 1024 ** 2, used: 6.1 * 1024 ** 2, percent: 3 },
        { mount: '/data', total: 500 * 1024 ** 3, used: 430 * 1024 ** 3, percent: 86 },
      ],
      diskIo: {
        readBps: 200000 + 900000 * Math.abs(Math.sin(i / 7)),
        writeBps: 300000 + 1500000 * Math.abs(Math.sin(i / 5)),
      },
      net: {
        recvBps: 1200000 + 3000000 * Math.abs(Math.sin(i / 9)) + Math.random() * 100000,
        sentBps: 90000 + 400000 * Math.abs(Math.sin(i / 4)),
      },
      processes: [
        { pid: 12539, name: 'argusagent', cpu: 12.5, mem: 3.2 },
        { pid: 206039, name: 'AliYunDunMonito', cpu: 8.4, mem: 5.1 },
        { pid: 16808, name: 'loongcollector', cpu: 6.1, mem: 2.8 },
        { pid: 279708, name: 'sshd', cpu: 3.2, mem: 0.5 },
        { pid: 267449, name: 'aliyun-service', cpu: 1.8, mem: 1.2 },
        { pid: 234256, name: 'tuned', cpu: 0.6, mem: 1.7 },
        { pid: 55813, name: 'AliYunDunUp...', cpu: 0.3, mem: 0.4 },
        { pid: 1, name: 'systemd', cpu: 0.1, mem: 0.8 },
      ],
      gpu: [
        { index: 0, name: 'NVIDIA A100-SXM4-40GB', util: 62, memUsedMB: 8192, memTotMB: 40960, tempC: 68 },
      ],
    })
  }
  for (let i = 0; i < 300; i++) push(i)
}

main().catch((e) => {
  document.body.innerHTML = `<pre style="color:#e88080;padding:20px;white-space:pre-wrap">${String(
    e && e.stack ? e.stack : e,
  )}</pre>`
})
