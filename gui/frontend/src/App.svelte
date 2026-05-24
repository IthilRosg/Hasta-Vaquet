<script lang="ts">
  import { fade } from 'svelte/transition'
  import { DoConnect, DoDisconnect, ImportConfigFromDialog, LoadDefaultConfig, LoadProfile, ListProfileItems, SaveLastProfile, LoadLastProfile, DeleteProfile } from '../wailsjs/go/main/App'
  import { EventsOn } from '../wailsjs/runtime/runtime'

  let connected = false
  let reconnecting = false
  let statusText = 'Disconnected'
  let txSpeed = '0 B/s'
  let rxSpeed = '0 B/s'
  let rtt = 0
  let loss = 0
  let uptime = '00:00'
  let showSettings = false
  let animating = false
  let profileName = ''
  let profiles: {name: string; server_ip: string; port: number; short_id: number; internal_ip: string; dns: string}[] = []
  let showProfileDropdown = false

  let pingTimer: number
  let uptimeTimer: number
  let startTime: number
  let totalTX = 0
  let totalRX = 0

  async function loadConfig() {
    profiles = (await ListProfileItems()) || []
    const last = await LoadLastProfile()
    if (last) {
      const cfg = await LoadProfile(last)
      if (cfg) { applyCfg(cfg); profileName = last; return }
    }
    const cfg = await LoadDefaultConfig()
    if (cfg) { applyCfg(cfg) }
  }
  loadConfig()

  function applyCfg(cfg: any) {
    profileName = cfg.profile_name || ''
    serverIP = cfg.server_ip || serverIP
    port = cfg.port || port
    shortID = cfg.short_id || shortID
    secretKey = cfg.secret_key || secretKey
    routingSalt = cfg.routing_salt || routingSalt
    internalIP = cfg.internal_ip || internalIP
    gatewayIP = cfg.gateway_ip || gatewayIP
    dns = cfg.dns || dns
  }

  let serverIP = '31.42.120.154'
  let port = 9999
  let shortID = 1
  let secretKey = ''
  let routingSalt = 'HastaVaquetGlobal'
  let internalIP = '10.0.0.10'
  let gatewayIP = '192.168.100.1'
  let dns = '1.1.1.1'

  function formatSpeed(bps: number): string {
    if (bps >= 1_000_000) return (bps / 1_000_000).toFixed(1) + ' MB/s'
    if (bps >= 1_000) return (bps / 1_000).toFixed(0) + ' KB/s'
    return bps + ' B/s'
  }

  function formatBytes(total: number): string {
    if (total >= 1_000_000_000) return (total / 1_000_000_000).toFixed(2) + ' GB'
    if (total >= 1_000_000) return (total / 1_000_000).toFixed(1) + ' MB'
    if (total >= 1_000) return (total / 1_000).toFixed(0) + ' KB'
    return total + ' B'
  }

  function formatTime(sec: number): string {
    const m = Math.floor(sec / 60)
    const s = sec % 60
    return String(m).padStart(2, '0') + ':' + String(s).padStart(2, '0')
  }

  EventsOn('status', (data: any) => {
    const s = typeof data === 'string' ? data : data.status
    const attempt = typeof data === 'object' ? data.attempt || 0 : 0
    if (s === 'connected') {
      connected = true; reconnecting = false
      statusText = 'Connected'
      startTime = Date.now()
      clearInterval(uptimeTimer)
      uptimeTimer = setInterval(() => {
        const sec = Math.floor((Date.now() - startTime) / 1000)
        uptime = formatTime(sec)
      }, 1000)
    } else if (s === 'disconnected') {
      connected = false; reconnecting = false
      statusText = 'Disconnected'
      txSpeed = '0 B/s'; rxSpeed = '0 B/s'; rtt = 0; loss = 0; uptime = '00:00'
      clearInterval(uptimeTimer)
    } else if (s === 'connecting') {
      statusText = 'Connecting...'
    } else if (s === 'reconnecting') {
      reconnecting = true
      statusText = 'Reconnecting… #' + attempt
    }
  })

  EventsOn('connection_status', (s: string) => {
    if (s === 'connected') {
      reconnecting = false
      statusText = 'Connected'
    } else if (s === 'reconnecting') {
      reconnecting = true
      statusText = 'Network lost — reconnecting…'
    } else if (s === 'disconnected') {
      reconnecting = false; connected = false
      statusText = 'Disconnected'
    }
  })

  EventsOn('traffic', (data: {tx_speed: number, rx_speed: number, total_tx: number, total_rx: number}) => {
    txSpeed = formatSpeed(data.tx_speed)
    rxSpeed = formatSpeed(data.rx_speed)
    totalTX = data.total_tx
    totalRX = data.total_rx
    const sec = Math.floor((Date.now() - startTime) / 1000)
    uptime = formatTime(sec)
  })

  EventsOn('ping', (data: {rtt: number, loss: number}) => {
    rtt = data.rtt
    loss = data.loss
  })

  function toggle() {
    if (animating || !serverIP) return
    if (connected) { disconnect() } else { connect() }
  }

  async function connect() {
    if (!serverIP) { statusText = 'No profile selected'; return }
    animating = true; statusText = 'Connecting...'
    const res = await DoConnect(serverIP, secretKey, routingSalt, internalIP, gatewayIP, dns, port, shortID)
    if (res !== 'connected') { statusText = res }
    animating = false
  }

  async function disconnect() {
    animating = true; await DoDisconnect(); animating = false
  }

  async function selectProfile(name: string) {
    const cfg = await LoadProfile(name)
    if (cfg) { applyCfg(cfg); profileName = name; SaveLastProfile(name) }
  }

  async function importProfile() {
    const cfg = await ImportConfigFromDialog()
    if (!cfg) return
    if (cfg.profile_name && !cfg.profile_name.startsWith('error')) {
      applyCfg(cfg)
      loadConfig()
    } else {
      statusText = cfg?.profile_name || 'Import failed'
    }
  }

  async function deleteCurrentProfile() {
    if (!profileName) return
    if (!confirm(`Удалить профиль "${profileName}"?`)) return
    const res = await DeleteProfile(profileName)
    if (res === 'deleted') {
      profileName = ''
      statusText = 'Profile deleted'
      // Сбросить настройки подключения
      serverIP = ''; port = 9999; shortID = 0
      secretKey = ''; internalIP = ''; gatewayIP = ''; dns = ''
      // Очистить last_profile.txt чтобы loadDefault не сработал
      await SaveLastProfile('')
      // Обновить только список профилей, не загружать никакие конфиги
      profiles = (await ListProfileItems()) || []
    } else {
      statusText = res
    }
  }

  function toggleSettings() { showSettings = !showSettings }
</script>

<div class="app-root">
  <!-- Top bar -->
  <div class="top-bar">
    <button class="icon-btn" title="Settings" on:click={toggleSettings}>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="12" cy="12" r="3"/><path d="M12 1v2m0 18v2M4.22 4.22l1.42 1.42m12.72 12.72l1.42 1.42M1 12h2m18 0h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/>
      </svg>
    </button>
  </div>

  <!-- Main action: button + status -->
  <div class="main-block">
    <div class="button-wrapper" class:connected>
      <button class="big-btn" on:click={toggle} disabled={animating || !serverIP}>
        <div class="icon">
          {#if connected}
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M5 12h14M12 5l7 7-7 7"/>
          </svg>
          {:else}
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M12 5v14M5 12l7-7 7 7"/>
          </svg>
          {/if}
        </div>
        <span class="label">{connected ? 'Disconnect' : 'Connect'}</span>
      </button>
    </div>
    <div class="status" class:reconnecting>{statusText}</div>
  </div>

  <!-- Stats panel (visible during reconnect too — no reset) -->
  {#if connected || reconnecting}
  <div class="stats-grid">
    <div class="card">
      <div class="card-title">SPEED</div>
      <div class="card-body">↑ {txSpeed}  ↓ {rxSpeed}</div>
    </div>
    <div class="card">
      <div class="card-title">DATA</div>
      <div class="card-body">↑ {formatBytes(totalTX)}  ↓ {formatBytes(totalRX)}</div>
    </div>
    <div class="card">
      <div class="card-title">NETWORK</div>
      <div class="card-body">Ping: {rtt > 0 ? rtt + 'ms' : '—'}  Loss: {rtt > 0 ? loss.toFixed(1) + '%' : '—'}</div>
    </div>
    <div class="card">
      <div class="card-title">UPTIME</div>
      <div class="card-body card-uptime">{uptime}</div>
    </div>
  </div>
  {/if}

  <!-- Reconnect overlay with blur -->
  {#if reconnecting}
  <div class="reconnect-overlay" transition:fade={{ duration: 300 }}>
    <div class="reconnect-spinner">
      <div class="pulsing-circle"></div>
      <p class="reconnect-text">Восстановление сети...</p>
      <p class="reconnect-hint">Кнопка отключения активна</p>
    </div>
  </div>
  {/if}

  <!-- Profile selector -->
  <div class="profile-section">
    <div class="profile-label">Current profile</div>
    <div class="profile-row">
      <div class="dropdown-wrap">
        <button class="dropdown-trigger" on:click={() => showProfileDropdown = !showProfileDropdown}>
          <span>{profileName || 'Select Profile'}</span>
          <span class="arrow">▼</span>
        </button>
        {#if showProfileDropdown}
        <div class="dropdown-menu">
          {#if profiles.length === 0}
          <div class="dropdown-empty">No profiles</div>
          {:else}
          {#each profiles as p}
          <button class="dropdown-item" class:selected={p.name === profileName} on:click={() => { selectProfile(p.name); showProfileDropdown = false; }}>
            <div>
              <div class="dropdown-item-name">{p.name}</div>
              <div class="dropdown-item-detail">{p.server_ip}:{p.port}</div>
            </div>
            <span class="dropdown-item-ip">{p.internal_ip}</span>
          </button>
          {/each}
          {/if}
        </div>
        {/if}
      </div>
      <button class="add-btn" on:click={importProfile} title="Импорт профиля">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
          <circle cx="12" cy="12" r="10"/>
        </svg>
      </button>
      {#if profileName}
      <button class="add-btn" on:click={deleteCurrentProfile} title="Удалить профиль" style="border-color: rgba(248,81,73,0.3);">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#f85149" stroke-width="2" stroke-linecap="round">
          <polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
        </svg>
      </button>
      {/if}
    </div>
  </div>
</div>

<!-- Settings panel -->
{#if showSettings}
<div class="overlay" on:click={toggleSettings}></div>
<div class="settings">
  <h2>Settings — {profileName}</h2>
  <div class="field"><label>Server IP</label><input bind:value={serverIP}/></div>
  <div class="field-row">
    <div class="field"><label>Port</label><input bind:value={port} type="number"/></div>
    <div class="field"><label>Short ID</label><input bind:value={shortID} type="number"/></div>
  </div>
  <div class="field"><label>Secret Key</label><input bind:value={secretKey} type="password"/></div>
  <div class="field-row">
    <div class="field"><label>Internal IP</label><input bind:value={internalIP}/></div>
    <div class="field"><label>Gateway IP</label><input bind:value={gatewayIP}/></div>
  </div>
  <div class="field-row">
    <div class="field"><label>DNS</label><input bind:value={dns}/></div>
    <div class="field"><label>Routing Salt</label><input bind:value={routingSalt}/></div>
  </div>
  <button class="import-btn" on:click={importProfile}>Import Profile (.json)</button>
</div>
{/if}

<div class="version">v0.2.3</div>

<style>
  .app-root {
    display: flex; flex-direction: column; align-items: center;
    width: 100%; height: 100vh; padding: 0 16px;
  }

  .top-bar {
    position: fixed; top: 0; right: 0; padding: 12px 16px; z-index: 10;
  }

  .icon-btn {
    background: none; border: none; color: var(--text-dim); cursor: pointer;
    padding: 8px; border-radius: var(--radius-sm); transition: all 0.2s;
  }
  .icon-btn:hover { color: var(--accent); background: var(--surface); }

  /* ----- Main action ----- */
  .main-block {
    display: flex; flex-direction: column; align-items: center;
    padding-top: 10vh; gap: 16px;
  }

  .button-wrapper { border-radius: 50%; padding: 4px; }
  .button-wrapper.connected { animation: pulse 2s ease-in-out infinite; }

  @keyframes pulse {
    0%, 100% { box-shadow: 0 0 0 0 var(--green-glow); }
    50% { box-shadow: 0 0 0 20px transparent; }
  }

  .big-btn {
    width: 140px; height: 140px; border-radius: 50%; border: 2px solid var(--border);
    background: var(--surface); color: var(--text); cursor: pointer;
    display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px;
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  }
  .big-btn:hover { border-color: var(--accent); background: var(--surface-hover); transform: scale(1.05); }
  .big-btn:active { transform: scale(0.95); }
  .big-btn:disabled { opacity: 0.5; cursor: not-allowed; transform: none; }
  .button-wrapper.connected .big-btn { border-color: var(--green); background: rgba(63, 185, 80, 0.08); }
  .icon { display: flex; }
  .label { font-size: 13px; font-weight: 600; letter-spacing: 0.5px; text-transform: uppercase; }
  .status { font-size: 14px; color: var(--text-dim); text-align: center; min-height: 20px; }

  /* ----- Stats grid 2x2 ----- */
  .stats-grid {
    display: grid; grid-template-columns: 1fr 1fr;
    gap: 12px; width: 100%; max-width: 360px; margin-top: 24px;
  }

  .card {
    background: rgba(255,255,255,0.04);
    border: 1px solid rgba(255,255,255,0.08);
    border-radius: 12px; padding: 16px;
    display: flex; flex-direction: column; gap: 8px;
  }

  .card-title {
    font-size: 11px; color: #888; letter-spacing: 1px;
    text-transform: uppercase;
  }

  .card-body {
    font-size: 13px; font-weight: 600; color: var(--text);
    font-variant-numeric: tabular-nums; line-height: 1.4;
  }

  .card-uptime {
    font-size: 28px; font-weight: 700; letter-spacing: 1px;
    color: var(--accent);
  }

  /* ----- Profile section ----- */
  .profile-section {
    width: 100%; max-width: 360px; margin-top: 24px;
  }

  .profile-label {
    font-size: 11px; color: #888; letter-spacing: 1px;
    text-transform: uppercase; margin-bottom: 8px; text-align: left;
  }

  .profile-row {
    display: flex; align-items: stretch; gap: 10px;
  }

  .dropdown-wrap { position: relative; flex: 1; }

  .dropdown-trigger {
    display: flex; align-items: center; justify-content: space-between;
    width: 100%; height: 44px; padding: 0 14px;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); cursor: pointer;
    color: var(--text); font-size: 14px; font-weight: 600;
    transition: border 0.2s;
  }
  .dropdown-trigger:hover { border-color: var(--accent); }
  .arrow { color: var(--text-dim); font-size: 10px; }

  .dropdown-menu {
    position: absolute; bottom: 100%; left: 0; width: 100%;
    margin-bottom: 8px; z-index: 50;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); overflow: hidden;
    box-shadow: 0 -8px 32px rgba(0,0,0,0.5);
    max-height: 200px; overflow-y: auto;
  }

  .dropdown-item {
    display: flex; align-items: center; justify-content: space-between;
    width: 100%; padding: 10px 14px; text-align: left;
    background: none; border: none; border-bottom: 1px solid var(--border);
    color: var(--text); cursor: pointer; transition: background 0.15s;
  }
  .dropdown-item:last-child { border-bottom: none; }
  .dropdown-item:hover { background: var(--surface-hover); }
  .dropdown-item.selected { background: rgba(63, 185, 80, 0.06); }
  .dropdown-item-name { font-size: 14px; font-weight: 600; }
  .dropdown-item-detail { font-size: 11px; color: var(--text-dim); margin-top: 2px; }
  .dropdown-item-ip { font-size: 12px; color: var(--accent); }
  .dropdown-empty { padding: 16px; text-align: center; color: var(--text-dim); font-size: 13px; }

  .add-btn {
    width: 44px; height: 44px; flex-shrink: 0;
    background: var(--surface); border: 2px dashed var(--border);
    color: var(--text-dim); cursor: pointer;
    border-radius: 50%; display: flex; align-items: center; justify-content: center;
    transition: all 0.2s;
  }
  .add-btn:hover { border-color: var(--accent); color: var(--accent); background: var(--accent-glow); }

  /* ----- Settings ----- */
  .overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); z-index: 100; }
  .settings { position: fixed; top: 0; right: 0; bottom: 0; width: 320px; background: var(--surface); border-left: 1px solid var(--border); padding: 24px; overflow-y: auto; z-index: 101; display: flex; flex-direction: column; gap: 16px; }
  .settings h2 { font-size: 18px; font-weight: 600; }
  .field { display: flex; flex-direction: column; gap: 4px; flex: 1; }
  .field-row { display: flex; gap: 12px; }
  .field label { font-size: 12px; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.5px; }
  .field input { background: var(--bg); border: 1px solid var(--border); color: var(--text); padding: 8px 12px; border-radius: var(--radius-sm); font-size: 14px; width: 100%; transition: border 0.2s; }
  .field input:focus { outline: none; border-color: var(--accent); }
  .import-btn { background: none; border: 1px dashed var(--border); color: var(--accent); padding: 10px; border-radius: var(--radius-sm); cursor: pointer; font-size: 14px; transition: all 0.2s; }
  .import-btn:hover { border-color: var(--accent); background: var(--accent-glow); }
  .version { position: fixed; bottom: 8px; right: 12px; font-size: 11px; color: var(--text-dim); opacity: 0.5; }

  .status.reconnecting { color: var(--yellow); }

  /* ----- Reconnect overlay ----- */
  .reconnect-overlay {
    position: fixed; inset: 0;
    background: rgba(0, 0, 0, 0.35);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
    z-index: 40;
    display: flex; align-items: center; justify-content: center;
    pointer-events: none;
  }
  .reconnect-spinner {
    pointer-events: none;
    text-align: center;
    display: flex; flex-direction: column; align-items: center; gap: 12px;
  }
  .pulsing-circle {
    width: 56px; height: 56px;
    border-radius: 50%;
    border: 3px solid var(--yellow);
    border-top-color: transparent;
    animation: reconnect-spin 0.9s linear infinite;
    box-shadow: 0 0 20px rgba(250, 197, 28, 0.15), 0 0 40px rgba(250, 197, 28, 0.05);
  }
  @keyframes reconnect-spin {
    to { transform: rotate(360deg); }
  }
  .reconnect-text {
    font-size: 16px; font-weight: 600; color: var(--text);
    letter-spacing: 0.3px;
  }
  .reconnect-hint {
    font-size: 12px; color: var(--text-dim); opacity: 0.7;
  }
</style>
