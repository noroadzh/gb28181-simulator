const BASE = ''

async function ok (resp) {
  if (!resp.ok) {
    const text = await resp.text().catch(() => '')
    let err = { message: text || resp.statusText }
    try { err = JSON.parse(text) } catch (_) {}
    throw new Error(err.error || err.message || String(resp.status))
  }
  if (resp.status === 204) return null
  return resp.json()
}

export const api = {

  // ─── Channels ────────────────────────────────────────────────────────────────

  listChannels (nodeId) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels`).then(ok)
  },
  addChannel (nodeId, { id, name, parentId, status }) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, name, parent_id: parentId || '', status: status || 'ON' })
    }).then(ok)
  },
  removeChannel (nodeId, ch) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}`, {
      method: 'DELETE'
    }).then(ok)
  },
  getChannelMedia (nodeId, ch) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/media`).then(ok)
  },
  putChannelMedia (nodeId, ch, cfg) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/media`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg)
    }).then(ok)
  },
  deleteChannelMedia (nodeId, ch) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/media`, { method: 'DELETE' }).then(ok)
  },

  // ─── PTZ ────────────────────────────────────────────────────────────────────

  ptzControl (nodeId, ch, cmd) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/ptz`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cmd)
    }).then(ok)
  },

  // ─── Records ────────────────────────────────────────────────────────────────

  listRecords (nodeId, ch, start, end) {
    const u = new URL(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/records`, location.href)
    if (start) u.searchParams.set('start', start)
    if (end) u.searchParams.set('end', end)
    return fetch(u).then(ok)
  },

  // ─── Talk ───────────────────────────────────────────────────────────────────

  startTalk (nodeId, ch) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/talk/start`, { method: 'POST' }).then(ok)
  },
  stopTalk (nodeId, ch) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/talk/stop`, { method: 'POST' }).then(ok)
  },

  // ─── Snapshot ───────────────────────────────────────────────────────────────

  getSnapshot (nodeId, ch) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/snapshot`)
  },

  // ─── Flv stream URL ─────────────────────────────────────────────────────────

  flvUrl (nodeId, ch) {
    // 相对路径：自动跟随当前页面的 host + port 与后端主 HTTP 监听地址，
    // 避免与后端实际端口/路径不匹配导致 404。
    return `${BASE}/v1/flv/${encodeURIComponent(nodeId)}/${encodeURIComponent(ch)}`
  },

  // ─── Existing API ───────────────────────────────────────────────────────────

  listNodes () {
    return fetch(`${BASE}/v1/nodes`).then(ok)
  },
  getNode (id) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(id)}`).then(ok)
  },
  startNode (id) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(id)}/start`, { method: 'POST' }).then(ok)
  },
  stopNode (id) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(id)}/stop`, { method: 'POST' }).then(ok)
  },
  unregisterNode (id) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(id)}/unregister`, { method: 'POST' }).then(ok)
  },
  getFault (id) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(id)}/faults`).then(ok)
  },
  installFault (id, profile) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(id)}/faults`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(profile)
    }).then(ok)
  },
  clearFault (id) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(id)}/faults`, { method: 'DELETE' }).then(ok)
  },
  getMedia (id) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(id)}/media`).then(ok)
  },
  putMedia (id, cfg) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(id)}/media`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg)
    }).then(ok)
  },
  deleteMedia (id) {
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(id)}/media`, { method: 'DELETE' }).then(ok)
  },
  listScenarios () {
    return fetch(`${BASE}/v1/scenarios`).then(ok)
  },
  runScenario (name) {
    return fetch(`${BASE}/v1/scenarios/run`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    }).then(ok)
  },
  getLastRun () {
    return fetch(`${BASE}/v1/scenarios/last-run`).then(ok)
  },
  queryCapture (id, limit = 50) {
    const u = new URL(`${BASE}/v1/nodes/${encodeURIComponent(id)}/capture`, location.href)
    u.searchParams.set('limit', String(limit))
    return fetch(u).then(ok)
  },
  downloadPCAP (id) {
    window.open(`${BASE}/v1/nodes/${encodeURIComponent(id)}/capture.pcap`, '_blank')
  },
  version () {
    return fetch(`${BASE}/v1/version`).then(ok)
  },
  health () {
    return fetch(`${BASE}/v1/health`).then(ok)
  },
  logsStream (onMessage, onClose) {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/v1/logs/stream`)
    ws.onmessage = (ev) => {
      try { onMessage(JSON.parse(ev.data)) } catch (_) {}
    }
    ws.onclose = () => { if (onClose) onClose() }
    return ws
  },

  // ─── Accounts (platform SIP registration) ───────────────────────────────────

  listAccounts (nodeId) {
    return fetch(`${BASE}/v1/platforms/${encodeURIComponent(nodeId)}/accounts`).then(ok)
  },
  addAccount (nodeId, username, password) {
    return fetch(`${BASE}/v1/platforms/${encodeURIComponent(nodeId)}/accounts`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password })
    }).then(ok)
  },
  removeAccount (nodeId, username) {
    return fetch(`${BASE}/v1/platforms/${encodeURIComponent(nodeId)}/accounts/${encodeURIComponent(username)}`, {
      method: 'DELETE'
    }).then(ok)
  },
  setAccountPassword (nodeId, username, password) {
    return fetch(`${BASE}/v1/platforms/${encodeURIComponent(nodeId)}/accounts/${encodeURIComponent(username)}/password`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password })
    }).then(ok)
  },

  // ─── Media upload ───────────────────────────────────────────────────────────

  uploadMedia (nodeId, file) {
    const fd = new FormData()
    fd.append('file', file)
    return fetch(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/media/upload`, {
      method: 'POST',
      body: fd
    }).then(ok)
  }
}
