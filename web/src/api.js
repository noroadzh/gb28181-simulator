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
  }
}
