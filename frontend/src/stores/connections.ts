import { defineStore } from 'pinia'
import * as models from '../../wailsjs/go/models'
import {
  DeleteGroup,
  DeleteHost,
  ListGroups,
  ListHosts,
  SaveGroup,
  SaveHost,
} from '../../wailsjs/go/app/Service'

// 树节点（UI 专用结构，与后端模型解耦）
export type TreeNodeKind = 'group' | 'host'

export interface TreeNode {
  key: string // `${kind}:${id}`
  kind: TreeNodeKind
  label: string
  group?: models.store.Group
  host?: models.store.Host
  children?: TreeNode[]
}

// Go 零值 time 序列化为 "0001-01-01T..."，视为“从未连接”
export function isZeroTime(v: unknown): boolean {
  return typeof v !== 'string' || v === '' || v.startsWith('0001-01-01')
}

function bySortThenName(
  a: { sort: number; name: string },
  b: { sort: number; name: string },
): number {
  return a.sort !== b.sort ? a.sort - b.sort : a.name.localeCompare(b.name, 'zh-CN')
}

export const useConnectionsStore = defineStore('connections', {
  state: () => ({
    groups: [] as models.store.Group[],
    hosts: [] as models.store.Host[],
    loading: false,
  }),

  getters: {
    // 组装树：分组按 parentId 层级嵌套，主机挂在其直属分组（groupId 为空则挂根）
    treeData(state): TreeNode[] {
      const hostsOf = (groupId: string): TreeNode[] =>
        state.hosts
          .filter((h) => h.groupId === groupId)
          .sort(bySortThenName)
          .map((h) => ({ key: `host:${h.id}`, kind: 'host' as const, label: h.name, host: h }))
      const groupsOf = (parentId: string): TreeNode[] =>
        state.groups
          .filter((g) => g.parentId === parentId)
          .sort(bySortThenName)
          .map((g) => ({
            key: `group:${g.id}`,
            kind: 'group' as const,
            label: g.name,
            group: g,
            children: [...groupsOf(g.id), ...hostsOf(g.id)],
          }))
      return [...groupsOf(''), ...hostsOf('')]
    },

    // 最近连接：仅统计真正连过的（M2 接入后生效），取前 5
    recentHosts(state): models.store.Host[] {
      return state.hosts
        .filter((h) => !isZeroTime(h.lastConnectedAt))
        .sort((a, b) =>
          String(b.lastConnectedAt).localeCompare(String(a.lastConnectedAt)),
        )
        .slice(0, 5)
    },
  },

  actions: {
    // 每次变更后整体拉取：数据量小，以服务端为唯一事实源，避免前端增量同步出错
    async load() {
      this.loading = true
      try {
        const [groups, hosts] = await Promise.all([ListGroups(), ListHosts()])
        this.groups = groups ?? []
        this.hosts = hosts ?? []
      } finally {
        this.loading = false
      }
    },

    async saveGroup(g: models.store.Group) {
      await SaveGroup(g)
      await this.load()
    },

    async deleteGroup(id: string) {
      await DeleteGroup(id)
      await this.load()
    },

    async saveHost(input: models.app.HostInput) {
      await SaveHost(input)
      await this.load()
    },

    async deleteHost(id: string) {
      await DeleteHost(id)
      await this.load()
    },

    // 兄弟节点上移/下移：整体重编 sort（0..n），只回存受影响的两条，
    // 同时兜底修复历史脏数据（sort 全 0 的场景）
    async moveSibling(kind: TreeNodeKind, id: string, dir: -1 | 1) {
      const all = kind === 'group' ? [...this.groups] : [...this.hosts]
      all.sort(bySortThenName)
      const self = all.find((x) => x.id === id)
      if (!self) return
      const parentId =
        kind === 'group'
          ? (self as models.store.Group).parentId
          : (self as models.store.Host).groupId
      const siblings = all.filter((x) =>
        kind === 'group'
          ? (x as models.store.Group).parentId === parentId
          : (x as models.store.Host).groupId === parentId,
      )
      const idx = siblings.findIndex((x) => x.id === id)
      const target = idx + dir
      if (idx < 0 || target < 0 || target >= siblings.length) return
      ;[siblings[idx], siblings[target]] = [siblings[target], siblings[idx]]
      siblings.forEach((s, i) => {
        s.sort = i
      })
      const moved = [siblings[idx], siblings[target]]
      await Promise.all(
        moved.map((s) =>
          kind === 'group'
            ? SaveGroup(s as models.store.Group)
            : SaveHost(new models.app.HostInput(s)),
        ),
      )
      await this.load()
    },
  },
})
