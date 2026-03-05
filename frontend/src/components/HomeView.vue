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

const copyCommand = async (torrent: TorrentWithStats, group: RepoGroup) => {
  try {
    let cmd = `llmpt download ${torrent.repo_id}`
    // If it's NOT the verified main branch, we must append --revision
    if (torrent.revision !== group.main_revision) {
      cmd += ` --revision ${torrent.revision}`
    }
    await navigator.clipboard.writeText(cmd)
    // Minimal visual feedback logic could go here
    alert(`Copied to clipboard:\n${cmd}`)
  } catch (err) {
    console.error('Failed to copy text: ', err)
  }
}

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
</script>

<template>
  <div class="models-view">
    <div class="page-header">
      <div>
        <h1>LLM Weights & Checkpoints</h1>
        <p class="subtitle">Distributed via optimized P2P tracker network</p>
      </div>
      
      <div class="search-box glass-panel">
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="11" cy="11" r="8"></circle>
          <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
        </svg>
        <input 
          type="text" 
          v-model="searchQuery" 
          placeholder="Search models... (e.g. Llama-3)"
        >
      </div>
    </div>

    <!-- Loading State -->
    <div v-if="loading" class="state-container glass-panel">
      <div class="loader"></div>
      <p>Synchronizing with Tracker node...</p>
    </div>

    <!-- Error State -->
    <div v-else-if="error" class="state-container error-state glass-panel">
      <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="var(--danger)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10"></circle>
        <line x1="12" y1="8" x2="12" y2="12"></line>
        <line x1="12" y1="16" x2="12.01" y2="16"></line>
      </svg>
      <p>{{ error }}</p>
      <button class="btn btn-primary" @click="fetchTorrents" style="margin-top: 1rem;">Retry connection</button>
    </div>

    <!-- Empty State -->
    <div v-else-if="filteredGroups.length === 0" class="state-container glass-panel">
      <p v-if="searchQuery">No models found matching "{{ searchQuery }}".</p>
      <p v-else>No models have been published to this tracker yet.</p>
    </div>

    <!-- Grouped Repo List -->
    <div v-else class="repo-list">
      <div v-for="group in filteredGroups" :key="group.repo_id" :class="['repo-card, glass-panel', { 'is-expanded': group.expanded }]">
        
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
              <span class="repo-meta">{{ formatBytes(group.total_size_latest) }} • {{ group.torrents.length }} revision(s)</span>
            </div>
          </div>
          <div class="repo-header-right">
            <!-- Loading indicator for HF status -->
            <div class="hf-status" v-if="group.resolving_main" title="Checking HuggingFace for latest commit...">
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
                <a :href="'https://huggingface.co/' + group.repo_id + '/commit/' + torrent.revision" target="_blank" class="revision-hash tooltip" title="View commit on HuggingFace">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="18" cy="18" r="3"></circle><circle cx="6" cy="6" r="3"></circle><path d="M13 6h3a2 2 0 0 1 2 2v7"></path><line x1="6" y1="9" x2="6" y2="21"></line></svg>
                  {{ torrent.revision.substring(0, 7) }}
                </a>
                
                <span class="badge badge-success hf-verified" v-if="torrent.revision === group.main_revision" title="This revision matches the current official 'main' branch">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></svg>
                  HF Sync / main
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
                <span class="btn-text">{{ torrent.file_count }} Files</span>
              </button>
              
              <button class="action-btn primary-outline" @click="copyCommand(torrent, group)" title="Copy CLI Command">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"></path>
                  <rect x="8" y="8" width="14" height="14" rx="2" ry="2"></rect>
                </svg>
                <span class="btn-text">CLI Download</span>
              </button>
            </div>
            
            <!-- Files Drawer -->
            <div class="files-drawer" v-if="torrent.filesExpanded">
              <div class="drawer-header">
                <h4>Packaged Files</h4>
                <span>{{ formatBytes(torrent.total_size) }} Total</span>
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
  </div>
</template>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  margin-bottom: 2.5rem;
  flex-wrap: wrap;
  gap: 1rem;
}

.search-box {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 1rem;
  min-width: 300px;
}

.search-box input {
  background: transparent;
  border: none;
  color: var(--text-primary);
  font-size: 0.95rem;
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
  min-height: 300px;
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
  gap: 1.25rem;
}

.repo-card {
  overflow: hidden;
  padding: 0; /* Override glass-panel padding to allow full-width headers */
  display: flex;
  flex-direction: column;
}

/* Repo Header */
.repo-header {
  padding: 1.25rem 1.5rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
  cursor: pointer;
  transition: background 0.2s ease;
}

.repo-header:hover {
  background: rgba(255,255,255,0.02);
}

.repo-title {
  display: flex;
  align-items: center;
  gap: 1.25rem;
}

.repo-title svg {
  color: var(--accent-primary);
  opacity: 0.9;
}

.repo-names h3 {
  font-size: 1.2rem;
  font-weight: 600;
  margin: 0;
  color: var(--text-primary);
  letter-spacing: -0.01em;
}

.repo-meta {
  font-size: 0.85rem;
  color: var(--text-tertiary);
  margin-top: 0.2rem;
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
  background: rgba(0, 0, 0, 0.15); /* Slightly darker inner bg */
}

.revision-row {
  padding: 1.25rem 1.5rem;
  border-bottom: 1px solid rgba(255,255,255,0.03);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
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
  background: rgba(255,255,255,0.06);
  padding: 0.2rem 0.5rem;
  border-radius: 6px;
  color: var(--text-secondary);
  text-decoration: none;
  transition: all 0.2s ease;
  border: 1px solid transparent;
}

.revision-hash:hover {
  background: rgba(255,255,255,0.1);
  color: var(--text-primary);
  border-color: rgba(255,255,255,0.1);
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
  background: rgba(255,255,255,0.05);
  border: 1px solid rgba(255,255,255,0.05);
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
  padding: 0.5rem 0.875rem;
  background: rgba(255,255,255,0.05);
  border: 1px solid var(--surface-border);
  border-radius: 6px;
  color: var(--text-secondary);
  cursor: pointer;
  transition: all 0.2s ease;
  font-size: 0.85rem;
  font-family: inherit;
}

.action-btn:hover {
  background: rgba(255,255,255,0.1);
  color: var(--text-primary);
  border-color: rgba(255,255,255,0.15);
}

.primary-outline {
  border-color: rgba(59, 130, 246, 0.4);
  color: var(--accent-primary);
}

.primary-outline:hover {
  background: rgba(59, 130, 246, 0.1);
  border-color: var(--accent-primary);
  color: var(--accent-primary);
}

/* Files Drawer component */
.files-drawer {
  flex-basis: 100%;
  margin-top: 1rem;
  background: rgba(0,0,0,0.25);
  border-radius: 8px;
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
  padding: 0.75rem 1.25rem;
  background: rgba(255,255,255,0.03);
  border-bottom: 1px solid var(--surface-border);
}

.drawer-header h4 {
  font-size: 0.85rem;
  font-weight: 500;
  color: var(--text-secondary);
  margin: 0;
}

.drawer-header span {
  font-size: 0.8rem;
  color: var(--text-tertiary);
  font-family: monospace;
}

.file-list {
  list-style: none;
  margin: 0;
  padding: 0.5rem 0;
  max-height: 250px;
  overflow-y: auto;
}

.file-list li {
  display: flex;
  justify-content: space-between;
  padding: 0.35rem 1.25rem;
  font-size: 0.85rem;
  font-family: 'ui-monospace', 'SFMono-Regular', Monaco, monospace;
}

.file-list li:hover {
  background: rgba(255,255,255,0.02);
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
