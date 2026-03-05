<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'

interface TorrentStats {
  seeders: number
  leechers: number
  completed: number
}

interface TorrentFile {
  path: string
  size: number
}

interface Torrent {
  id: string
  repo_id: string
  revision: string
  repo_type: string
  name: string
  info_hash: string
  total_size: number
  file_count: number
  piece_length: number
  created_at: string
  files: TorrentFile[]
}

interface TorrentWithStats extends Torrent {
  stats: TorrentStats
  // UI states
  filesExpanded?: boolean
}

interface RepoGroup {
  repo_id: string
  name: string
  repo_type: string
  latest_created_at: string
  total_size_latest: number
  main_revision: string | null
  resolving_main: boolean
  expanded: boolean
  torrents: TorrentWithStats[]
}

const loading = ref(true)
const error = ref('')
const searchQuery = ref('')
const repoGroups = ref<RepoGroup[]>([])

// Helper to format bytes
const formatBytes = (bytes: number, decimals = 2) => {
  if (!+bytes) return '0 Bytes'
  const k = 1024
  const dm = decimals < 0 ? 0 : decimals
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`
}

// Format date relative or absolute
const formatDate = (dateStr: string) => {
  const date = new Date(dateStr)
  return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', year: 'numeric' }).format(date)
}

// Fetch main branch SHA from HuggingFace with fallback to HF-Mirror
const resolveMainBranch = async (repo_id: string): Promise<string | null> => {
  const endpoints = [
    `https://huggingface.co/api/models/${repo_id}`,
    `https://hf-mirror.com/api/models/${repo_id}`
  ]

  for (const endpoint of endpoints) {
    try {
      const controller = new AbortController()
      const id = setTimeout(() => controller.abort(), 3500)
      const res = await fetch(endpoint, { signal: controller.signal })
      clearTimeout(id)

      if (res.ok) {
        const data = await res.json()
        if (data && data.sha) return data.sha
      }
    } catch (err) {
      console.warn(`Failed to fetch from ${endpoint}`, err)
    }
  }
  return null
}

const fetchTorrents = async () => {
  loading.value = true
  error.value = ''
  try {
    const res = await fetch('/api/v1/torrents')
    if (!res.ok) throw new Error('Failed to fetch data from tracker node.')
    const data = await res.json()
    const torrents: TorrentWithStats[] = data.data || []

    // Group by repo_id
    const map = new Map<string, RepoGroup>()
    for (const t of torrents) {
      if (!map.has(t.repo_id)) {
        map.set(t.repo_id, {
          repo_id: t.repo_id,
          name: t.name,
          repo_type: t.repo_type,
          latest_created_at: t.created_at,
          total_size_latest: t.total_size,
          main_revision: null,
          resolving_main: true,
          expanded: false,
          torrents: []
        })
      }
      
      const group = map.get(t.repo_id)!
      // Update latest if newer
      if (new Date(t.created_at) > new Date(group.latest_created_at)) {
        group.latest_created_at = t.created_at
        group.total_size_latest = t.total_size
      }
      group.torrents.push(t)
    }

    // Sort groups by latest activity
    const groupsArray = Array.from(map.values()).sort((a, b) => {
      return new Date(b.latest_created_at).getTime() - new Date(a.latest_created_at).getTime()
    })

    // Sort torrents inside each group by date descending
    groupsArray.forEach(g => {
      g.torrents.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    })

    repoGroups.value = groupsArray
    
    // Asynchronously resolve main branch for each group
    groupsArray.forEach(async (group) => {
      group.main_revision = await resolveMainBranch(group.repo_id)
      group.resolving_main = false
    })

  } catch (err: any) {
    error.value = err.message || 'An error occurred while fetching models.'
    console.error(err)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchTorrents()
})

const toggleFiles = (torrent: TorrentWithStats) => {
  torrent.filesExpanded = !torrent.filesExpanded
}

const toggleGroup = (group: RepoGroup) => {
  group.expanded = !group.expanded
}

const filteredGroups = computed(() => {
  if (!searchQuery.value) return repoGroups.value
  const query = searchQuery.value.toLowerCase()
  return repoGroups.value.filter(g => 
    g.repo_id.toLowerCase().includes(query) || 
    g.name.toLowerCase().includes(query)
  )
})

const isDrawerOpen = ref(false)
const selectedTorrent = ref<TorrentWithStats | null>(null)
const selectedGroup = ref<RepoGroup | null>(null)

const openDownloadDrawer = (torrent: TorrentWithStats, group: RepoGroup) => {
  selectedTorrent.value = torrent
  selectedGroup.value = group
  isDrawerOpen.value = true
}

const closeDrawer = () => {
  isDrawerOpen.value = false
  setTimeout(() => {
    selectedTorrent.value = null
    selectedGroup.value = null
  }, 300)
}

const getRepoTypeName = (repoType: string) => {
  if (repoType === 'dataset') return '数据集'
  if (repoType === 'space') return '空间'
  return '模型'
}

const copiedState = ref<Record<string, boolean>>({})

const doCopy = async (text: string, id: string) => {
  try {
    await navigator.clipboard.writeText(text)
    copiedState.value[id] = true
    setTimeout(() => {
      copiedState.value[id] = false
    }, 2000)
  } catch (err) {
    console.error('Failed to copy text: ', err)
  }
}

const cliCommand = computed(() => {
  if (!selectedTorrent.value || !selectedGroup.value) return ''
  let cmd = `llmpt-cli download ${selectedGroup.value.repo_id}`
  if (selectedTorrent.value.revision !== selectedGroup.value.main_revision) {
    cmd += ` --revision ${selectedTorrent.value.revision}`
  }
  return cmd
})

const pyCommand = computed(() => {
  if (!selectedTorrent.value || !selectedGroup.value) return ''
  const isDataset = selectedGroup.value.repo_type === 'dataset'
  let code = `import llmpt\nfrom huggingface_hub import snapshot_download\n\n# 自动使用 P2P 加速下载\nsnapshot_download(\n    repo_id="${selectedGroup.value.repo_id}",\n    revision="${selectedTorrent.value.revision}"`
  if (isDataset) {
    code += `,\n    repo_type="dataset"`
  }
  code += `\n)`
  return code
})
</script>

<template>
  <div class="models-view">
    <div class="page-header">
      <div>
        <h1>基于 P2P 网络的大模型下载</h1>
        <p class="subtitle">通过去中心化技术，更快速、免费地获取 Hugging Face 上的开源模型</p>
      </div>
      
      <div class="search-box panel">
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="11" cy="11" r="8"></circle>
          <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
        </svg>
        <input 
          type="text" 
          v-model="searchQuery" 
          placeholder="搜索模型... (例如 Llama-3)"
        >
      </div>
    </div>

    <!-- Loading State -->
    <div v-if="loading" class="state-container panel">
      <div class="loader"></div>
      <p>正在同步网络数据...</p>
    </div>

    <!-- Error State -->
    <div v-else-if="error" class="state-container error-state panel">
      <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="var(--danger)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10"></circle>
        <line x1="12" y1="8" x2="12" y2="12"></line>
        <line x1="12" y1="16" x2="12.01" y2="16"></line>
      </svg>
      <p>{{ error }}</p>
      <button class="btn btn-primary" @click="fetchTorrents" style="margin-top: 1rem;">重试连接</button>
    </div>

    <!-- Empty State -->
    <div v-else-if="filteredGroups.length === 0" class="state-container panel">
      <p v-if="searchQuery">没有找到与 "{{ searchQuery }}" 相关的模型。</p>
      <p v-else>当前网络中还没有发布任何模型。</p>
    </div>

    <!-- Grouped Repo List -->
    <div v-else class="repo-list">
      <div v-for="group in filteredGroups" :key="group.repo_id" :class="['repo-card panel', { 'is-expanded': group.expanded }]">
        
        <!-- Repo Header (Click to expand) -->
        <div class="repo-header" @click="toggleGroup(group)">
          <div class="repo-title">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"></path>
              <polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline>
              <line x1="12" y1="22.08" x2="12" y2="12"></line>
            </svg>
            <div class="repo-names">
              <h3>{{ group.repo_id }}</h3>
              <span class="repo-meta">{{ formatBytes(group.total_size_latest) }} • {{ group.torrents.length }} 个版本</span>
            </div>
          </div>
          <div class="repo-header-right">
            <div class="hf-status" v-if="group.resolving_main" title="正在与 HuggingFace 同步最新版本...">
              <div class="pulse-dot"></div>
            </div>
            <div class="chevron">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" :style="{ transform: group.expanded ? 'rotate(180deg)' : 'rotate(0)' }">
                <polyline points="6 9 12 15 18 9"></polyline>
              </svg>
            </div>
          </div>
        </div>

        <!-- Revisions Body -->
        <div class="repo-body" v-if="group.expanded">
          <div v-for="torrent in group.torrents" :key="torrent.id" class="revision-row">
            
            <div class="revision-main-info">
              <div class="revision-header">
                <a :href="'https://huggingface.co/' + group.repo_id + '/commit/' + torrent.revision" target="_blank" class="revision-hash tooltip" title="在 Hugging Face 网站查看此提交记录">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="18" cy="18" r="3"></circle><circle cx="6" cy="6" r="3"></circle><path d="M13 6h3a2 2 0 0 1 2 2v7"></path><line x1="6" y1="9" x2="6" y2="21"></line></svg>
                  {{ torrent.revision.substring(0, 7) }}
                </a>
                
                <span class="badge badge-success hf-verified" v-if="torrent.revision === group.main_revision" title="此版本与 Hugging Face 官方的 main 分支一致">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></svg>
                  main
                </span>
                <span class="date-label">{{ formatDate(torrent.created_at) }}</span>
              </div>
              
              <div class="stats-group">
                <span class="badge" title="Seeders">S: {{ torrent.stats.seeders }}</span>
                <span class="badge" title="Leechers">L: {{ torrent.stats.leechers }}</span>
                <span class="completed" v-if="torrent.stats.completed > 0">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="20 6 9 17 4 12"></polyline>
                  </svg>
                  {{ torrent.stats.completed }}
                </span>
              </div>
            </div>

            <div class="revision-actions">
              <button class="action-btn" @click="toggleFiles(torrent)">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                  <polyline points="17 8 12 3 7 8"></polyline>
                  <line x1="12" y1="3" x2="12" y2="15"></line>
                </svg>
                <span class="btn-text">{{ torrent.file_count }} 个文件</span>
              </button>
              
              <button class="action-btn primary-outline" @click="openDownloadDrawer(torrent, group)" title="配置并下载">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                  <polyline points="7 10 12 15 17 10"></polyline>
                  <line x1="12" y1="15" x2="12" y2="3"></line>
                </svg>
                <span class="btn-text">下载{{ getRepoTypeName(group.repo_type) }}</span>
              </button>
            </div>
            
            <!-- Files Drawer -->
            <div class="files-drawer" v-if="torrent.filesExpanded">
              <div class="drawer-header">
                <h4>打包文件列表</h4>
                <span>共计 {{ formatBytes(torrent.total_size) }}</span>
              </div>
              <ul class="file-list">
                <li v-for="(file, idx) in torrent.files" :key="idx">
                  <span class="file-path">{{ file.path }}</span>
                  <span class="file-size">{{ formatBytes(file.size) }}</span>
                </li>
              </ul>
            </div>
          </div>
        </div>
        
      </div>
    </div>

    <!-- Slide Drawer for Download Instructions -->
    <div class="drawer-overlay" :class="{ 'is-open': isDrawerOpen }" @click="closeDrawer"></div>
    <div class="side-drawer panel" :class="{ 'is-open': isDrawerOpen }">
      <div class="drawer-header-main">
        <h2>下载指引</h2>
        <button class="close-btn" @click="closeDrawer">&times;</button>
      </div>
      
      <div class="drawer-content" v-if="selectedTorrent && selectedGroup">
        <p class="drawer-subtitle">
          即将通过 P2P 协议为您加速下载 <strong>{{ selectedGroup.repo_id }}</strong>。
        </p>

        <div class="doc-section">
          <h3>1. 客户端库安装</h3>
          <p>请确保您的 Python 环境中已安装轻量级的 <code>llmpt-client</code>（依赖极简）：</p>
          <div class="code-block">
            <code>pip install llmpt-client</code>
            <button @click="doCopy('pip install llmpt-client', 'install')" class="copy-btn" :class="{ 'is-copied': copiedState['install'] }">
              {{ copiedState['install'] ? '已复制 ✓' : '复制' }}
            </button>
          </div>
        </div>

        <div class="doc-section">
          <h3>2. 方式一：CLI 命令行 (推荐)</h3>
          <p>无需修改任何代码，直接通过命令行高效下载文件（支持断点续传）：</p>
          <div class="code-block">
            <code>{{ cliCommand }}</code>
            <button @click="doCopy(cliCommand, 'cli')" class="copy-btn" :class="{ 'is-copied': copiedState['cli'] }">
              {{ copiedState['cli'] ? '已复制 ✓' : '复制' }}
            </button>
          </div>
        </div>

        <div class="doc-section">
          <h3>3. 方式二：Python 脚本无缝集成</h3>
          <p>在现有代码的最上面<strong>增加一行 <code>import llmpt</code></strong>，即可零配置拦截官方 <code>huggingface_hub</code>，自动实施资源 P2P 加速与回退降级：</p>
          <div class="code-block multi-line">
            <pre><code>{{ pyCommand }}</code></pre>
            <button @click="doCopy(pyCommand, 'py')" class="copy-btn" :class="{ 'is-copied': copiedState['py'] }">
              {{ copiedState['py'] ? '已复制 ✓' : '复制' }}
            </button>
          </div>
          <p class="footnote">注：在环境变量中设置 <code>HF_USE_P2P=1</code> 以默认启用拦截，如果 P2P 失败会自动降维回 Huggingface 官网源。</p>
        </div>

        <div class="doc-section more-info">
          <h3>延伸阅读</h3>
          <p>查阅详细的高级配置（如指定 tracker 端口、静默下载模式等），欢迎访问我们开源客户端 <a href="https://github.com/gogky/llmpt-client" target="_blank">llmpt-client GitHub 库</a> 阅读完整使用文档。</p>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  margin-bottom: 1.5rem;
  flex-wrap: wrap;
  gap: 1rem;
}

.search-box {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.4rem 0.8rem;
  min-width: 300px;
}

.search-box input {
  background: transparent;
  border: none;
  color: var(--text-primary);
  font-size: 0.9rem;
  width: 100%;
  outline: none;
}

.search-box input::placeholder {
  color: var(--text-tertiary);
}

.search-box svg {
  color: var(--text-secondary);
}

/* State Containers */
.state-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 200px;
  text-align: center;
  color: var(--text-secondary);
}

.loader {
  border: 3px solid var(--surface-border);
  border-top-color: var(--accent-primary);
  border-radius: 50%;
  width: 40px;
  height: 40px;
  animation: spin 1s linear infinite;
  margin-bottom: 1rem;
}

@keyframes spin {
  0% { transform: rotate(0deg); }
  100% { transform: rotate(360deg); }
}

/* Repositories List Grid/Stack */
.repo-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.repo-card {
  overflow: hidden;
  padding: 0; /* Override panel padding to allow full-width headers */
  display: flex;
  flex-direction: column;
}

/* Repo Header */
.repo-header {
  padding: 0.75rem 1rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
  cursor: pointer;
  transition: background 0.2s ease;
}

.repo-header:hover {
  background: var(--surface-hover);
}

.repo-title {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.repo-title svg {
  color: var(--accent-primary);
  opacity: 0.9;
}

.repo-names h3 {
  font-size: 1rem;
  font-weight: 600;
  margin: 0;
  color: var(--text-primary);
  letter-spacing: -0.01em;
}

.repo-meta {
  font-size: 0.75rem;
  color: var(--text-tertiary);
  margin-top: 0.1rem;
  display: block;
}

.repo-header-right {
  display: flex;
  align-items: center;
  gap: 1rem;
}

.chevron svg {
  transition: transform 0.3s cubic-bezier(0.175, 0.885, 0.32, 1.275);
  color: var(--text-tertiary);
}

.pulse-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--warning);
  box-shadow: 0 0 0 0 rgba(245, 158, 11, 0.7);
  animation: pulse 1.5s infinite;
}

@keyframes pulse {
  0% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(245, 158, 11, 0.7); }
  70% { transform: scale(1); box-shadow: 0 0 0 6px rgba(245, 158, 11, 0); }
  100% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(245, 158, 11, 0); }
}

/* Revisions Body */
.repo-body {
  border-top: 1px solid var(--surface-border);
  background: var(--bg-color); /* Contrast inside the card */
}

.revision-row {
  padding: 0.75rem 1rem;
  border-bottom: 1px solid var(--surface-border);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.revision-row:last-child {
  border-bottom: none;
}

.revision-main-info {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.revision-header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.revision-hash {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  font-family: 'ui-monospace', 'SFMono-Regular', Menlo, Monaco, Consolas, monospace;
  font-size: 0.85rem;
  background: var(--surface-hover);
  padding: 0.2rem 0.5rem;
  border-radius: 6px;
  color: var(--text-secondary);
  text-decoration: none;
  transition: all 0.2s ease;
  border: 1px solid var(--surface-border);
}

.revision-hash:hover {
  background: #e5e7eb;
  color: var(--text-primary);
  border-color: #d1d5db;
}

.date-label {
  font-size: 0.8rem;
  color: var(--text-tertiary);
}

.hf-verified {
  gap: 0.25rem;
  box-shadow: 0 0 10px rgba(16, 185, 129, 0.15);
}

.stats-group {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.stats-group .badge {
  background: var(--surface-hover);
  border: 1px solid var(--surface-border);
  color: var(--text-secondary);
}

.completed {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.8rem;
  color: var(--text-secondary);
}

.revision-actions {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.action-btn {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.35rem 0.6rem;
  background: #ffffff;
  border: 1px solid var(--surface-border);
  border-radius: 4px;
  color: var(--text-secondary);
  cursor: pointer;
  transition: all 0.2s ease;
  font-size: 0.75rem;
  font-family: inherit;
  box-shadow: 0 1px 2px 0 rgba(0, 0, 0, 0.05);
}

.action-btn:hover {
  background: var(--surface-hover);
  color: var(--text-primary);
  border-color: #d1d5db;
}

.primary-outline {
  border-color: #fcd34d;
  background: #fffbeb;
  color: #b45309;
}

.primary-outline:hover {
  background: #fef3c7;
  border-color: #f59e0b;
  color: #92400e;
}

.files-drawer {
  flex-basis: 100%;
  margin-top: 0.5rem;
  background: #f9fafb;
  border-radius: 6px;
  border: 1px solid var(--surface-border);
  overflow: hidden;
  animation: slideDown 0.3s ease-out forwards;
}

@keyframes slideDown {
  from { opacity: 0; transform: translateY(-5px); }
  to { opacity: 1; transform: translateY(0); }
}

.drawer-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.5rem 1rem;
  background: var(--surface-hover);
  border-bottom: 1px solid var(--surface-border);
}

.drawer-header h4 {
  font-size: 0.8rem;
  font-weight: 500;
  color: var(--text-secondary);
  margin: 0;
}

.drawer-header span {
  font-size: 0.75rem;
  color: var(--text-tertiary);
  font-family: monospace;
}

.file-list {
  list-style: none;
  margin: 0;
  padding: 0.25rem 0;
  max-height: 250px;
  overflow-y: auto;
}

.file-list li {
  display: flex;
  justify-content: space-between;
  padding: 0.25rem 1rem;
  font-size: 0.8rem;
  font-family: 'ui-monospace', 'SFMono-Regular', Monaco, monospace;
}

.file-list li:hover {
  background: #ffffff;
}

.file-path {
  color: var(--text-primary);
  word-break: break-all;
  padding-right: 1rem;
}

.file-size {
  color: var(--text-tertiary);
  white-space: nowrap;
}

/* Side Drawer Styles */
.drawer-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0,0,0,0.4);
  backdrop-filter: blur(2px);
  z-index: 100;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.3s ease;
}

.drawer-overlay.is-open {
  opacity: 1;
  pointer-events: auto;
}

.side-drawer {
  position: fixed;
  top: 0;
  right: -550px;
  bottom: 0;
  width: 100%;
  max-width: 500px;
  background: var(--surface-color);
  z-index: 101;
  box-shadow: -4px 0 15px rgba(0,0,0,0.1);
  transition: right 0.3s cubic-bezier(0.16, 1, 0.3, 1);
  display: flex;
  flex-direction: column;
  border-radius: 0;
  border-left: 1px solid var(--surface-border);
}

.side-drawer.is-open {
  right: 0;
}

.drawer-header-main {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1.25rem 1.5rem;
  border-bottom: 1px solid var(--surface-border);
}

.drawer-header-main h2 {
  font-size: 1.25rem;
  font-weight: 600;
  margin: 0;
  color: var(--text-primary);
}

.close-btn {
  background: none;
  border: none;
  font-size: 1.75rem;
  line-height: 1;
  cursor: pointer;
  color: var(--text-tertiary);
  transition: color 0.2s;
}

.close-btn:hover {
  color: var(--danger);
}

.drawer-content {
  flex: 1;
  overflow-y: auto;
  padding: 1.5rem;
}

.drawer-subtitle {
  color: var(--text-secondary);
  margin-bottom: 2rem;
  font-size: 0.95rem;
}

.doc-section {
  margin-bottom: 2rem;
}

.doc-section h3 {
  font-size: 1rem;
  font-weight: 600;
  margin-bottom: 0.75rem;
  color: var(--text-primary);
}

.doc-section p {
  font-size: 0.9rem;
  color: var(--text-secondary);
  line-height: 1.6;
  margin-bottom: 0.75rem;
}

.code-block {
  background: #f8fafc;
  border: 1px solid var(--surface-border);
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.5rem 0.75rem;
}

.code-block code {
  font-family: 'ui-monospace', 'SFMono-Regular', Menlo, Monaco, Consolas, monospace;
  font-size: 0.85rem;
  color: #0f172a;
  white-space: nowrap;
  overflow-x: auto;
}

.code-block.multi-line {
  align-items: flex-start;
  padding: 0.75rem;
}

.code-block.multi-line pre {
  margin: 0;
  overflow-x: auto;
}

.code-block.multi-line code {
  white-space: pre;
}

.copy-btn {
  background: white;
  border: 1px solid var(--surface-border);
  border-radius: 4px;
  padding: 0.25rem 0.5rem;
  font-size: 0.75rem;
  cursor: pointer;
  color: var(--text-secondary);
  margin-left: 1rem;
  flex-shrink: 0;
  transition: all 0.2s;
  font-weight: 500;
}

.copy-btn:hover {
  background: var(--surface-hover);
  color: var(--text-primary);
}

.copy-btn.is-copied {
  color: var(--success);
  border-color: var(--success);
  background: #ecfdf5;
}

.footnote {
  margin-top: 0.5rem;
  font-size: 0.8rem !important;
  color: var(--warning) !important;
}

.more-info a {
  color: #2563eb;
  text-decoration: none;
  font-weight: 500;
}
.more-info a:hover {
  text-decoration: underline;
}

@media (max-width: 768px) {
  .revision-row {
    flex-direction: column;
    align-items: flex-start;
  }
  .revision-actions {
    width: 100%;
  }
  .action-btn {
    flex: 1;
    justify-content: center;
  }
}
</style>
