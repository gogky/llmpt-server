<script setup lang="ts">
import { ref, onMounted } from 'vue'

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080'

interface TorrentData {
  id: string;
  repo_id: string;
  revision: string;
  info_hash: string;
  total_size: number;
  file_count: number;
  status: string;
  created_at: string;
}

interface TorrentPeer {
  ip: string;
  port: number;
  address: string;
  last_seen: string;
}

interface TorrentPeersPayload {
  torrent_id: string;
  repo_id: string;
  revision: string;
  swarm_key: string;
  seeder_count: number;
  leecher_count: number;
  seeders: TorrentPeer[];
  leechers: TorrentPeer[];
}

const torrents = ref<TorrentData[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
const expandedPeerPanels = ref<Record<string, boolean>>({})
const loadingPeers = ref<Record<string, boolean>>({})
const peerErrors = ref<Record<string, string | null>>({})
const peerDetails = ref<Record<string, TorrentPeersPayload>>({})

const isAuthenticated = ref(false)
const adminToken = ref('')

onMounted(() => {
  const savedToken = localStorage.getItem('adminToken')
  if (savedToken) {
    adminToken.value = savedToken
    isAuthenticated.value = true
    loadTorrents()
  }
})

const login = () => {
  if (adminToken.value.trim()) {
    localStorage.setItem('adminToken', adminToken.value.trim())
    isAuthenticated.value = true
    loadTorrents()
  }
}

const logout = () => {
  localStorage.removeItem('adminToken')
  adminToken.value = ''
  isAuthenticated.value = false
  torrents.value = []
  expandedPeerPanels.value = {}
  loadingPeers.value = {}
  peerErrors.value = {}
  peerDetails.value = {}
}

const getAuthHeaders = () => ({
  'Authorization': `Bearer ${adminToken.value}`
})

const loadTorrents = async () => {
  if (!isAuthenticated.value) return
  
  loading.value = true
  error.value = null
  try {
    const res = await fetch(`${API_BASE_URL}/api/v1/admin/torrents`, {
      headers: getAuthHeaders()
    })
    
    if (res.status === 401 || res.status === 403) {
      logout()
      error.value = '授权失败，请重新登录'
      return
    }
    
    if (!res.ok) throw new Error('加载失败')
    
    const json = await res.json()
    torrents.value = json.data || []
  } catch (err: any) {
    error.value = err.message || '网络连接错误'
  } finally {
    loading.value = false
  }
}

const approveTorrent = async (id: string) => {
  try {
    const res = await fetch(`${API_BASE_URL}/api/v1/admin/torrents/${id}/approve`, {
      method: 'POST',
      headers: getAuthHeaders()
    })
    
    if (!res.ok) {
      const e = await res.json()
      alert('审批失败: ' + (e.error || res.statusText))
      return
    }
    
    alert('已通过')
    loadTorrents()
  } catch(e) {
    alert('请求错误')
  }
}

const deleteTorrent = async (id: string) => {
  if (!confirm('确定要彻底删除该种子的所有数据和 Tracker 记录吗？')) return
  
  try {
    const res = await fetch(`${API_BASE_URL}/api/v1/admin/torrents/${id}`, {
      method: 'DELETE',
      headers: getAuthHeaders()
    })
    
    if (!res.ok) {
      const e = await res.json()
      alert('删除失败: ' + (e.error || res.statusText))
      return
    }
    
    alert('已删除')
    delete expandedPeerPanels.value[id]
    delete loadingPeers.value[id]
    delete peerErrors.value[id]
    delete peerDetails.value[id]
    loadTorrents()
  } catch(e) {
    alert('请求错误')
  }
}

const togglePeers = async (id: string) => {
  const isOpen = expandedPeerPanels.value[id]
  expandedPeerPanels.value[id] = !isOpen

  if (!isOpen && !peerDetails.value[id] && !loadingPeers.value[id]) {
    await loadPeers(id)
  }
}

const loadPeers = async (id: string) => {
  loadingPeers.value[id] = true
  peerErrors.value[id] = null

  try {
    const res = await fetch(`${API_BASE_URL}/api/v1/admin/torrents/${id}/peers`, {
      headers: getAuthHeaders()
    })

    if (res.status === 401 || res.status === 403) {
      logout()
      error.value = '授权失败，请重新登录'
      return
    }

    if (!res.ok) {
      const payload = await res.json().catch(() => null)
      throw new Error(payload?.error || 'Peer 列表加载失败')
    }

    const json = await res.json()
    peerDetails.value[id] = json as TorrentPeersPayload
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : 'Peer 列表加载失败'
    peerErrors.value[id] = message
  } finally {
    loadingPeers.value[id] = false
  }
}

const formatBytes = (bytes: number) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

const formatDate = (dateString: string) => {
  return new Date(dateString).toLocaleString()
}

const formatPeerEndpoint = (peer: TorrentPeer) => {
  if (peer.address) return peer.address
  if (peer.port > 0) return `${peer.ip}:${peer.port}`
  return peer.ip
}

const getPeerPayload = (id: string) => peerDetails.value[id] ?? null

const getSeederCount = (id: string) => getPeerPayload(id)?.seeder_count ?? 0

const getLeecherCount = (id: string) => getPeerPayload(id)?.leecher_count ?? 0

const getSeederPeers = (id: string) => getPeerPayload(id)?.seeders ?? []

const getLeecherPeers = (id: string) => getPeerPayload(id)?.leechers ?? []
</script>

<template>
  <div class="admin-container">
    <div class="admin-header">
      <h2>管理后台</h2>
      <button v-if="isAuthenticated" @click="logout" class="action-btn delete-btn">退出登录</button>
    </div>

    <!-- Login Form -->
    <div v-if="!isAuthenticated" class="login-card panel">
      <h3>小站管理登录</h3>
      <div class="input-group">
        <label>Admin Token</label>
        <input type="password" v-model="adminToken" @keyup.enter="login" placeholder="请输入超级管理员 Token..." />
      </div>
      <button class="primary-btn" @click="login">登录</button>
    </div>

    <!-- Admin Panel -->
    <div v-else class="admin-panel">
      <div class="table-actions">
        <button class="action-btn" @click="loadTorrents">🔄 刷新列表</button>
      </div>
      
      <div v-if="error" class="error-msg">
        {{ error }}
      </div>
      
      <div v-else-if="loading" class="loading-state">
        <div class="loader"></div>
        <p>加载中...</p>
      </div>
      
      <div v-else class="table-container panel">
        <table class="data-table">
          <thead>
            <tr>
              <th>仓库 (Repo ID)</th>
              <th>Revision</th>
              <th>大小</th>
              <th>文件数</th>
              <th>状态</th>
              <th>时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="torrents.length === 0">
              <td colspan="7" class="empty-state">暂无数据</td>
            </tr>
            <template v-for="t in torrents" :key="t.id">
              <tr>
                <td><span class="repo-id">{{ t.repo_id }}</span></td>
                <td><span class="revision-badge">{{ t.revision.substring(0, 7) }}</span></td>
                <td>{{ formatBytes(t.total_size) }}</td>
                <td>{{ t.file_count }}</td>
                <td>
                  <span class="status-badge" :class="t.status">{{ t.status === 'pending' ? '待审核' : '生效中' }}</span>
                </td>
                <td>{{ formatDate(t.created_at) }}</td>
                <td class="row-actions">
                  <button @click="togglePeers(t.id)" class="action-btn sm">
                    {{ expandedPeerPanels[t.id] ? '收起 Peer' : '查看 Peer IP' }}
                  </button>
                  <button v-if="t.status === 'pending'" @click="approveTorrent(t.id)" class="action-btn primary-btn sm">
                    通过
                  </button>
                  <button @click="deleteTorrent(t.id)" class="action-btn delete-btn sm">
                    删除
                  </button>
                </td>
              </tr>
              <tr v-if="expandedPeerPanels[t.id]" class="peer-row">
                <td colspan="7" class="peer-panel-cell">
                  <div class="peer-panel">
                    <div v-if="loadingPeers[t.id]" class="peer-status">正在加载 Peer 列表...</div>
                    <div v-else-if="peerErrors[t.id]" class="peer-status peer-error">{{ peerErrors[t.id] }}</div>
                    <div v-else-if="peerDetails[t.id]" class="peer-groups">
                      <div class="peer-group">
                        <div class="peer-group-header">
                          <h4>Seeders</h4>
                          <span>{{ getSeederCount(t.id) }}</span>
                        </div>
                        <div v-if="getSeederPeers(t.id).length === 0" class="peer-empty">
                          当前没有 Seeder
                        </div>
                        <ul v-else class="peer-list">
                          <li v-for="peer in getSeederPeers(t.id)" :key="`seeder-${peer.address}`" class="peer-item">
                            <div class="peer-main">{{ peer.ip }}</div>
                            <div class="peer-meta">
                              <span>{{ formatPeerEndpoint(peer) }}</span>
                              <span>最近心跳：{{ formatDate(peer.last_seen) }}</span>
                            </div>
                          </li>
                        </ul>
                      </div>

                      <div class="peer-group">
                        <div class="peer-group-header">
                          <h4>Leechers</h4>
                          <span>{{ getLeecherCount(t.id) }}</span>
                        </div>
                        <div v-if="getLeecherPeers(t.id).length === 0" class="peer-empty">
                          当前没有 Leecher
                        </div>
                        <ul v-else class="peer-list">
                          <li v-for="peer in getLeecherPeers(t.id)" :key="`leecher-${peer.address}`" class="peer-item">
                            <div class="peer-main">{{ peer.ip }}</div>
                            <div class="peer-meta">
                              <span>{{ formatPeerEndpoint(peer) }}</span>
                              <span>最近心跳：{{ formatDate(peer.last_seen) }}</span>
                            </div>
                          </li>
                        </ul>
                      </div>
                    </div>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<style scoped>
.admin-container {
  max-width: 1200px;
  margin: 0 auto;
}

.admin-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 2rem;
}

.admin-header h2 {
  font-size: 1.5rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.panel {
  background: var(--surface-card);
  border: 1px solid var(--surface-border);
  border-radius: 12px;
  padding: 2rem;
}

/* Login */
.login-card {
  max-width: 400px;
  margin: 4rem auto;
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}

.login-card h3 {
  margin: 0;
  text-align: center;
}

.input-group {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.input-group label {
  font-size: 0.875rem;
  color: var(--text-secondary);
}

.input-group input {
  padding: 0.75rem 1rem;
  background: var(--surface-bg);
  border: 1px solid var(--surface-border);
  border-radius: 8px;
  color: var(--text-primary);
  font-size: 1rem;
  outline: none;
  transition: border-color 0.2s;
}

.input-group input:focus {
  border-color: var(--accent-primary);
}

.primary-btn {
  background: var(--accent-primary);
  color: white;
  border: none;
  border-radius: 8px;
  padding: 0.75rem 1.5rem;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.2s;
}

.primary-btn:hover {
  opacity: 0.9;
}

.primary-btn.sm {
  padding: 0.5rem 1rem;
  font-size: 0.875rem;
}

.action-btn {
  background: var(--surface-bg);
  color: var(--text-primary);
  border: 1px solid var(--surface-border);
  border-radius: 8px;
  padding: 0.75rem 1.5rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.action-btn:hover {
  background: var(--surface-border);
}

.action-btn.sm {
  padding: 0.5rem 1rem;
  font-size: 0.875rem;
}

.delete-btn {
  color: #ef4444;
  border-color: #fca5a5;
  background: transparent;
}
.delete-btn:hover {
  background: #fee2e2;
  color: #b91c1c;
}

.table-actions {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 1rem;
}

.table-container {
  padding: 0;
  overflow: hidden;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
  text-align: left;
}

.data-table th, .data-table td {
  padding: 1rem 1.5rem;
  border-bottom: 1px solid var(--surface-border);
  font-size: 0.9rem;
}

.data-table th {
  background: var(--surface-bg);
  font-weight: 600;
  color: var(--text-secondary);
}

.data-table tr:last-child td {
  border-bottom: none;
}

.repo-id {
  font-family: var(--font-mono);
  color: var(--text-primary);
  font-weight: 500;
}

.revision-badge {
  font-family: var(--font-mono);
  background: var(--surface-bg);
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
  border: 1px solid var(--surface-border);
  color: var(--text-secondary);
  font-size: 0.85rem;
}

.status-badge {
  display: inline-block;
  padding: 0.25rem 0.75rem;
  border-radius: 99px;
  font-size: 0.8rem;
  font-weight: 600;
  text-transform: uppercase;
}

.status-badge.active {
  background: rgba(16, 185, 129, 0.1);
  color: #10b981;
  border: 1px solid rgba(16, 185, 129, 0.2);
}

.status-badge.pending {
  background: rgba(245, 158, 11, 0.1);
  color: #f59e0b;
  border: 1px solid rgba(245, 158, 11, 0.2);
}

.row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.peer-row td {
  background: color-mix(in srgb, var(--surface-bg) 70%, transparent);
}

.peer-panel-cell {
  padding: 0 !important;
}

.peer-panel {
  padding: 1.25rem 1.5rem 1.5rem;
}

.peer-status {
  color: var(--text-secondary);
}

.peer-error {
  color: #b91c1c;
}

.peer-groups {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}

.peer-group {
  border: 1px solid var(--surface-border);
  border-radius: 12px;
  background: var(--surface-card);
  overflow: hidden;
}

.peer-group-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1rem 1rem 0.75rem;
  border-bottom: 1px solid var(--surface-border);
}

.peer-group-header h4 {
  margin: 0;
  font-size: 0.95rem;
}

.peer-group-header span {
  font-family: var(--font-mono);
  color: var(--text-secondary);
}

.peer-empty {
  padding: 1rem;
  color: var(--text-secondary);
}

.peer-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.peer-item {
  padding: 0.875rem 1rem;
  border-bottom: 1px solid var(--surface-border);
}

.peer-item:last-child {
  border-bottom: none;
}

.peer-main {
  font-family: var(--font-mono);
  font-weight: 600;
  color: var(--text-primary);
}

.peer-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  margin-top: 0.4rem;
  font-size: 0.8rem;
  color: var(--text-secondary);
}

.error-msg {
  padding: 1rem;
  background: #fee2e2;
  color: #b91c1c;
  border-radius: 8px;
  text-align: center;
}

.empty-state {
  text-align: center;
  color: var(--text-secondary);
  padding: 3rem !important;
}

.loading-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1rem;
  padding: 4rem;
  color: var(--text-secondary);
}

.loader {
  width: 32px;
  height: 32px;
  border: 3px solid var(--surface-border);
  border-top-color: var(--accent-primary);
  border-radius: 50%;
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

@media (prefers-color-scheme: dark) {
  .delete-btn:hover {
    background: rgba(239, 68, 68, 0.1);
  }
}

@media (max-width: 900px) {
  .peer-groups {
    grid-template-columns: 1fr;
  }
}
</style>
