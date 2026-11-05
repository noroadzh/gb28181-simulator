/**
 * Centralized unauthenticated / session-expired handling.
 * Wired from main.js to avoid circular imports with api / pinia.
 */

let onNeedLogin = null
let busy = false

export function installAuthGuard (handler) {
  onNeedLogin = handler
}

/**
 * @param {'expired'|'required'} reason
 * @param {{ navigate?: boolean }} [opts] navigate=false when caller already redirects (e.g. router guard)
 */
export function handleUnauthorized (reason = 'expired', opts = {}) {
  const navigate = opts.navigate !== false
  sessionStorage.removeItem('gbsim_token')
  if (busy) return
  const hash = location.hash || ''
  if (hash.includes('/login')) return
  busy = true
  Promise.resolve()
    .then(() => onNeedLogin?.({ reason, navigate }))
    .catch(() => {})
    .finally(() => {
      setTimeout(() => { busy = false }, 1200)
    })
}
