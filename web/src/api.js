const BASE = ''

function authHeaders (extra = {}) {
  const headers = { ...extra }
  const token = sessionStorage.getItem('gbsim_token')
  if (token) headers.Authorization = `Bearer ${token}`
  return headers
}

function request (url, options = {}) {
  const opts = {
    credentials: 'include',
    ...options,
    headers: authHeaders(options.headers || {})
  }
  return fetch(`${BASE}${url}`, opts).then(async (resp) => {
    if (resp.status === 401 && !url.startsWith('/v1/auth/')) {
      sessionStorage.removeItem('gbsim_token')
      if (!location.hash.includes('/login')) {
        const redirect = encodeURIComponent(location.hash.replace(/^#/, '') || '/dashboard')
        location.hash = `#/login?redirect=${redirect}`
      }
    }
    return resp
  })
}

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
  authStatus () {
    return request('/v1/auth/status').then(ok)
  },
  login (username, password) {
    return request('/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password })
    }).then(ok)
  },
  logout () {
    return request('/v1/auth/logout', { method: 'POST' }).then(ok)
  },


  // ─── Channels ────────────────────────────────────────────────────────────────

  listChannels (nodeId) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels`).then(ok)
  },
  addChannel (nodeId, { id, name, parentId, status }) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, name, parent_id: parentId || '', status: status || 'ON' })
    }).then(ok)
  },
  removeChannel (nodeId, ch) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}`, {
      method: 'DELETE'
    }).then(ok)
  },
  getChannelMedia (nodeId, ch) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/media`).then(ok)
  },
  putChannelMedia (nodeId, ch, cfg) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/media`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg)
    }).then(ok)
  },
  deleteChannelMedia (nodeId, ch) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/media`, { method: 'DELETE' }).then(ok)
  },

  // ─── PTZ ────────────────────────────────────────────────────────────────────

  ptzControl (nodeId, ch, cmd) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/ptz`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cmd)
    }).then(ok)
  },

  // ─── Records (device RecordInfo) ────────────────────────────────────────────

  listRecords (nodeId, ch, start, end) {
    const u = new URL(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/records`, location.href)
    if (start) u.searchParams.set('start', start)
    if (end) u.searchParams.set('end', end)
    return request(u.pathname + u.search).then(ok)
  },

  // ─── Platform recordings library (#42 / #47) ────────────────────────────────

  listRecordings (nodeId, ch, start, end) {
    const u = new URL(`${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/recordings`, location.href)
    if (start) u.searchParams.set('start', start)
    if (end) u.searchParams.set('end', end)
    return request(u.pathname + u.search).then(ok)
  },
  recordingFlvUrl (nodeId, ch, recordingId) {
    let url = `${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/recordings/${encodeURIComponent(recordingId)}/flv`
    const token = sessionStorage.getItem('gbsim_token')
    if (token) url += `?access_token=${encodeURIComponent(token)}`
    return url
  },

  // ─── Platform live pull (#41 / #47) ─────────────────────────────────────────

  startLive (nodeId, ch, transport = 'udp') {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/live`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ transport })
    }).then(ok)
  },
  stopLive (nodeId, ch) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/live/stop`, {
      method: 'POST'
    }).then(ok)
  },
  getLive (nodeId, ch) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/live`).then(ok)
  },

  listCascadeSessions (nodeId) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/cascade/sessions`).then(ok)
  },

  // ─── Talk ───────────────────────────────────────────────────────────────────

  startTalk (nodeId, ch) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/talk/start`, { method: 'POST' }).then(ok)
  },
  stopTalk (nodeId, ch) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/talk/stop`, { method: 'POST' }).then(ok)
  },

  // ─── Snapshot ───────────────────────────────────────────────────────────────

  getSnapshot (nodeId, ch) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/snapshot`)
  },

  // ─── Flv stream URL ─────────────────────────────────────────────────────────

  flvUrl (nodeId, ch, opts = {}) {
    // 相对路径：自动跟随当前页面的 host + port 与后端主 HTTP 监听地址，
    // 避免与后端实际端口/路径不匹配导致 404。
    // opts.source: 'live' | 'local' | 'auto'（缺省由后端 live-优先）
    let url = `${BASE}/v1/flv/${encodeURIComponent(nodeId)}/${encodeURIComponent(ch)}`
    const q = []
    if (opts.source) q.push(`source=${encodeURIComponent(opts.source)}`)
    const token = sessionStorage.getItem('gbsim_token')
    if (token) q.push(`access_token=${encodeURIComponent(token)}`)
    if (q.length) url += `?${q.join('&')}`
    return url
  },

  // ─── Media file URL (native <video src>) ────────────────────────────────────
  //
  // 浏览器原生 video 播放 MP4 使用的直出端点（支持 HTTP Range）。仅 file
  // 源且 .mp4 时使用，由 ChannelDetailView 播放分派决定调用与否。
  mediaFileUrl (nodeId, ch) {
    let url = `${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/media-file`
    const token = sessionStorage.getItem('gbsim_token')
    if (token) url += `?access_token=${encodeURIComponent(token)}`
    return url
  },

  // ─── Node definition CRUD (runtime-node-configuration) ─────────────────────

  createNode (payload) {
    return request(`/v1/nodes`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    }).then(ok)
  },
  updateNode (id, payload) {
    return request(`/v1/nodes/${encodeURIComponent(id)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    }).then(ok)
  },
  deleteNode (id) {
    return request(`/v1/nodes/${encodeURIComponent(id)}`, { method: 'DELETE' }).then(ok)
  },
  updateNodeCascade (id, { parent, children }) {
    return request(`/v1/nodes/${encodeURIComponent(id)}/cascade`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ parent, children })
    }).then(ok)
  },

  // 对讲音频 WebSocket：上下行都是二进制 PCMU 帧（每帧一个 RTP 载荷）。
  talkAudioUrl (nodeId, ch, session) {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    let url = `${proto}://${location.host}${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/talk/audio?session=${encodeURIComponent(session)}`
    const token = sessionStorage.getItem('gbsim_token')
    if (token) url += `&access_token=${encodeURIComponent(token)}`
    return url
  },

  // 回放会话：play 带时间窗，成功后用 playbackFlvUrl 播放；teardown 结束。
  startPlayback (nodeId, ch, { sn, start, end }) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/playback`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action: 'play', sn, start, end })
    }).then(ok)
  },
  stopPlayback (nodeId, ch, sn) {
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/playback`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action: 'teardown', sn })
    }).then(ok)
  },
  playbackFlvUrl (nodeId, ch, sn) {
    let url = `${BASE}/v1/nodes/${encodeURIComponent(nodeId)}/channels/${encodeURIComponent(ch)}/playback/flv?sn=${encodeURIComponent(sn)}`
    const token = sessionStorage.getItem('gbsim_token')
    if (token) url += `&access_token=${encodeURIComponent(token)}`
    return url
  },

  // 节点状态推送（/v1/nodes/stream）。帧是 events.Event 的 JSON：
  // {type, id, status?, at}。返回 socket 本身，由调用方管理重连与关闭。
  nodesStream (onMessage, onClose) {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/v1/nodes/stream`)
    ws.onmessage = (ev) => {
      try { onMessage(JSON.parse(ev.data)) } catch (_) {}
    }
    ws.onclose = () => { if (onClose) onClose() }
    return ws
  },

  listNodes () {
    return request(`/v1/nodes`).then(ok)
  },
  getNode (id) {
    return request(`/v1/nodes/${encodeURIComponent(id)}`).then(ok)
  },
  startNode (id) {
    return request(`/v1/nodes/${encodeURIComponent(id)}/start`, { method: 'POST' }).then(ok)
  },
  stopNode (id) {
    return request(`/v1/nodes/${encodeURIComponent(id)}/stop`, { method: 'POST' }).then(ok)
  },
  unregisterNode (id) {
    return request(`/v1/nodes/${encodeURIComponent(id)}/unregister`, { method: 'POST' }).then(ok)
  },
  getFault (id) {
    return request(`/v1/nodes/${encodeURIComponent(id)}/faults`).then(ok)
  },
  installFault (id, profile) {
    return request(`/v1/nodes/${encodeURIComponent(id)}/faults`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(profile)
    }).then(ok)
  },
  clearFault (id) {
    return request(`/v1/nodes/${encodeURIComponent(id)}/faults`, { method: 'DELETE' }).then(ok)
  },
  getMedia (id) {
    return request(`/v1/nodes/${encodeURIComponent(id)}/media`).then(ok)
  },
  putMedia (id, cfg) {
    return request(`/v1/nodes/${encodeURIComponent(id)}/media`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg)
    }).then(ok)
  },
  deleteMedia (id) {
    return request(`/v1/nodes/${encodeURIComponent(id)}/media`, { method: 'DELETE' }).then(ok)
  },
  listScenarios () {
    return request(`/v1/scenarios`).then(ok)
  },
  runScenario (name) {
    return request(`/v1/scenarios/run`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    }).then(ok)
  },
  getLastRun () {
    return request(`/v1/scenarios/last-run`).then(ok)
  },
  queryCapture (id, limit = 50) {
    const u = new URL(`${BASE}/v1/nodes/${encodeURIComponent(id)}/capture`, location.href)
    u.searchParams.set('limit', String(limit))
    return request(u.pathname + u.search).then(ok)
  },
  downloadPCAP (id) {
    window.open(`${BASE}/v1/nodes/${encodeURIComponent(id)}/capture.pcap`, '_blank')
  },
  version () {
    return request(`/v1/version`).then(ok)
  },
  health () {
    return request(`/v1/health`).then(ok)
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
    return request(`/v1/platforms/${encodeURIComponent(nodeId)}/accounts`).then(ok)
  },
  addAccount (nodeId, username, password) {
    return request(`/v1/platforms/${encodeURIComponent(nodeId)}/accounts`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password })
    }).then(ok)
  },
  removeAccount (nodeId, username) {
    return request(`/v1/platforms/${encodeURIComponent(nodeId)}/accounts/${encodeURIComponent(username)}`, {
      method: 'DELETE'
    }).then(ok)
  },
  setAccountPassword (nodeId, username, password) {
    return request(`/v1/platforms/${encodeURIComponent(nodeId)}/accounts/${encodeURIComponent(username)}/password`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password })
    }).then(ok)
  },

  // ─── Media upload ───────────────────────────────────────────────────────────

  uploadMedia (nodeId, file) {
    const fd = new FormData()
    fd.append('file', file)
    return request(`/v1/nodes/${encodeURIComponent(nodeId)}/media/upload`, {
      method: 'POST',
      body: fd
    }).then(ok)
  }
}
