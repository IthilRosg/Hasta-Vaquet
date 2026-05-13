<script lang="ts">
  import { DoConnect, DoDisconnect, ImportConfig, LoadDefaultConfig, ListProfiles, LoadProfile, ListProfileItems, DoPing } from '../wailsjs/go/main/App'
  import { EventsOn } from '../wailsjs/runtime/runtime'

  let connected = false
  let statusText = 'Disconnected'
  let txSpeed = '0 B/s'
  let rxSpeed = '0 B/s'
  let rtt = 0
  let loss = 0
  let showSettings = false
  let animating = false
  let profileName = 'Default'
  let profiles: {name: string; server_ip: string; port: number; short_id: number; internal_ip: string; dns: string}[] = []
  let showProfileMenu = false

  let serverIP = '31.42.120.154'
  let port = 9999
  let shortID = 1
  let secretKey = ''
  let routingSalt = 'HastaVaquetGlobal'
  let internalIP = '10.0.0.10'
  let gatewayIP = '192.168.100.1'
  let dns = '1.1.1.1'

  async function loadConfig() {
    const list = await ListProfileItems()
    profiles = list || []
    const cfg = await LoadDefaultConfig()
    if (cfg) { applyCfg(cfg) }
  }
  loadConfig()

  function applyCfg(cfg: any) {
    profileName = cfg.profile_name || 'Default'
    serverIP = cfg.server_ip || serverIP
    port = cfg.port || port
    shortID = cfg.short_id || shortID
    secretKey = cfg.secret_key || secretKey
    routingSalt = cfg.routing_salt || routingSalt
    internalIP = cfg.internal_ip || internalIP
    gatewayIP = cfg.gateway_ip || gatewayIP
    dns = cfg.dns || dns
  }

  async function selectProfile(name: string) {
    showProfileMenu = false
    const cfg = await LoadProfile(name)
    if (cfg) {
      applyCfg(cfg)
      profileName = name
    }
  }

  function formatSpeed(bps: number): string {
    if (bps >= 1_000_000) return (bps / 1_000_000).toFixed(1) + ' MB/s'
    if (bps >= 1_000) return (bps / 1_000).toFixed(0) + ' KB/s'
    return bps + ' B/s'
  }

  let pingTimer: number

  EventsOn('status', (s: string) => {
    statusText = s === 'connected' ? 'Connected' : 'Disconnected'
    connected = s === 'connected'
    if (!connected) {
      txSpeed = '0 B/s'; rxSpeed = '0 B/s'; rtt = 0; loss = 0
      if (pingTimer) { clearInterval(pingTimer); pingTimer = 0 }
    } else {
      if (!pingTimer) startPinging()
    }
  })

  async function startPinging() {
    pingTimer = window.setInterval(async () => {
      if (!connected) return
      const res = await DoPing()
      rtt = res.rtt || 0
      loss = res.loss || 0
    }, 3000)
  }

  EventsOn('traffic', (data: {tx: number, rx: number}) => {
    txSpeed = formatSpeed(data.tx)
    rxSpeed = formatSpeed(data.rx)
  })

  function toggle() {
    if (animating) return
    if (connected) { disconnect() } else { connect() }
  }

  async function connect() {
    animating = true
    statusText = 'Connecting...'
    const res = await DoConnect(serverIP, secretKey, routingSalt, internalIP, gatewayIP, dns, port, shortID)
    if (res !== 'connected') { statusText = res }
    animating = false
  }

  async function disconnect() {
    animating = true
    await DoDisconnect()
    animating = false
  }

  async function importProfile() {
    const path = prompt('Enter path to config.json:')
    if (!path) return
    const cfg = await ImportConfig(path)
    if (cfg && cfg.profile_name !== 'error') {
      applyCfg(cfg)
    } else {
      statusText = cfg?.profile_name || 'Import failed'
    }
  }

  function toggleSettings() { showSettings = !showSettings }
</script>

<div class="container">
  <!-- Profile selector -->
  <div class="profile-bar">
    <button class="profile-btn" on:click={() => showProfileMenu = !showProfileMenu}>
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M16 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/><circle cx="8.5" cy="7" r="4"/><path d="M20 8v6m-3-3h6"/>
      </svg>
      <span class="profile-name">{profileName}</span>
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M6 9l6 6 6-6"/>
      </svg>
    </button>
    <button class="gear" on:click={toggleSettings}>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="12" cy="12" r="3"/><path d="M12 1v2m0 18v2M4.22 4.22l1.42 1.42m12.72 12.72l1.42 1.42M1 12h2m18 0h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/>
      </svg>
    </button>
  </div>

  <!-- Profile dropdown -->
  {#if showProfileMenu}
  <div class="profile-dropdown">
    {#each profiles as p}
    <button class="profile-item" on:click={() => selectProfile(p.name)}>
      <div class="profile-item-name">{p.name}</div>
      <div class="profile-item-detail">{p.server_ip}:{p.port} | {p.internal_ip}</div>
    </button>
    {/each}
    {#if profiles.length === 0}
    <div class="profile-empty">No profiles. Add .json files to profiles/</div>
    {/if}
  </div>
  {/if}

  <!-- Main button -->
  <div class="button-wrapper" class:connected>
    <button class="big-btn" on:click={toggle} disabled={animating}>
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

  <!-- Status -->
  <div class="status">{statusText}</div>

  <!-- Speed counters -->
  <div class="speeds">
    <div class="speed-item">
      <span class="speed-arrow up">↑</span>
      <span class="speed-val">{txSpeed}</span>
    </div>
    <div class="speed-divider"></div>
    <div class="speed-item">
      <span class="speed-arrow down">↓</span>
      <span class="speed-val">{rxSpeed}</span>
    </div>
  </div>

  <!-- Ping & Loss -->
  {#if connected}
  <div class="ping-row">
    <div class="ping-item">
      <span class="ping-label">Ping</span>
      <span class="ping-value" class:ping-ok={rtt > 0 && rtt < 100} class:ping-warn={rtt >= 100}>{rtt > 0 ? rtt + ' ms' : '—'}</span>
    </div>
    <div class="speed-divider"></div>
    <div class="ping-item">
      <span class="ping-label">Loss</span>
      <span class="ping-value" class:ping-ok={loss === 0} class:ping-warn={loss > 0}>{loss}%</span>
    </div>
  </div>
  {/if}
</div>

<!-- Settings panel -->
{#if showSettings}
<div class="overlay" on:click={toggleSettings}></div>
<div class="settings">
  <h2>Settings — {profileName}</h2>
  <div class="field">
    <label>Server IP</label>
    <input bind:value={serverIP} placeholder="Server IP"/>
  </div>
  <div class="field-row">
    <div class="field">
      <label>Port</label>
      <input bind:value={port} type="number" placeholder="9999"/>
    </div>
    <div class="field">
      <label>Short ID</label>
      <input bind:value={shortID} type="number" placeholder="1"/>
    </div>
  </div>
  <div class="field">
    <label>Secret Key</label>
    <input bind:value={secretKey} type="password" placeholder="Secret key"/>
  </div>
  <div class="field-row">
    <div class="field">
      <label>Internal IP</label>
      <input bind:value={internalIP} placeholder="10.0.0.10"/>
    </div>
    <div class="field">
      <label>Gateway IP</label>
      <input bind:value={gatewayIP} placeholder="192.168.100.1"/>
    </div>
  </div>
  <div class="field-row">
    <div class="field">
      <label>DNS</label>
      <input bind:value={dns} placeholder="1.1.1.1"/>
    </div>
    <div class="field">
      <label>Routing Salt</label>
      <input bind:value={routingSalt} placeholder="Routing salt"/>
    </div>
  </div>
  <button class="import-btn" on:click={importProfile}>Import Profile (.json)</button>
</div>
{/if}

<style>
  .container {
    display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 24px;
    width: 100%; max-width: 360px;
  }

  .profile-bar {
    position: fixed; top: 0; left: 0; right: 0;
    display: flex; align-items: center; justify-content: space-between;
    padding: 12px 16px; gap: 8px; z-index: 10;
  }

  .profile-btn {
    display: flex; align-items: center; gap: 6px;
    background: var(--surface); border: 1px solid var(--border);
    color: var(--text); padding: 6px 12px; border-radius: var(--radius);
    cursor: pointer; font-size: 13px; transition: all 0.2s;
  }

  .profile-btn:hover { border-color: var(--accent); background: var(--surface-hover); }

  .profile-name { font-weight: 600; max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .gear {
    background: none; border: none; color: var(--text-dim); cursor: pointer;
    padding: 6px; border-radius: var(--radius-sm); transition: all 0.2s;
  }

  .gear:hover { color: var(--text); background: var(--surface); }

  .profile-dropdown {
    position: fixed; top: 48px; left: 16px; right: 16px; max-width: 320px;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); z-index: 20; overflow: hidden;
    box-shadow: 0 8px 32px rgba(0,0,0,0.4);
  }

  .profile-item {
    width: 100%; background: none; border: none; border-bottom: 1px solid var(--border);
    color: var(--text); padding: 10px 14px; text-align: left; cursor: pointer;
    transition: background 0.15s;
  }

  .profile-item:last-child { border-bottom: none; }
  .profile-item:hover { background: var(--surface-hover); }

  .profile-item-name { font-weight: 600; font-size: 14px; }
  .profile-item-detail { font-size: 11px; color: var(--text-dim); margin-top: 2px; }

  .profile-empty { padding: 16px; text-align: center; color: var(--text-dim); font-size: 13px; }

  .button-wrapper { position: relative; border-radius: 50%; padding: 4px; margin-top: 48px; }
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

  .speeds {
    display: flex; align-items: center; gap: 20px; padding: 12px 24px;
    background: var(--surface); border-radius: var(--radius); border: 1px solid var(--border);
  }

  .speed-item { display: flex; align-items: center; gap: 8px; min-width: 100px; }
  .speed-arrow { font-size: 18px; font-weight: 700; }
  .speed-arrow.up { color: var(--green); }
  .speed-arrow.down { color: var(--accent); }
  .speed-val { font-size: 14px; font-weight: 500; font-variant-numeric: tabular-nums; }
  .speed-divider { width: 1px; height: 24px; background: var(--border); }

  .ping-row {
    display: flex; align-items: center; gap: 20px;
    padding: 8px 24px; border-radius: var(--radius);
    border: 1px solid var(--border); background: var(--surface);
  }

  .ping-item { display: flex; align-items: center; gap: 8px; min-width: 80px; }
  .ping-label { font-size: 11px; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.5px; }
  .ping-value { font-size: 14px; font-weight: 600; font-variant-numeric: tabular-nums; }
  .ping-ok { color: var(--green); }
  .ping-warn { color: var(--red); }

  .overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); z-index: 100; }

  .settings {
    position: fixed; top: 0; right: 0; bottom: 0; width: 320px;
    background: var(--surface); border-left: 1px solid var(--border);
    padding: 24px; overflow-y: auto; z-index: 101;
    display: flex; flex-direction: column; gap: 16px;
  }

  .settings h2 { font-size: 18px; font-weight: 600; margin-bottom: 8px; }

  .field { display: flex; flex-direction: column; gap: 4px; flex: 1; }
  .field-row { display: flex; gap: 12px; }

  .field label { font-size: 12px; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.5px; }

  .field input {
    background: var(--bg); border: 1px solid var(--border); color: var(--text);
    padding: 8px 12px; border-radius: var(--radius-sm); font-size: 14px; width: 100%;
    transition: border 0.2s;
  }

  .field input:focus { outline: none; border-color: var(--accent); }

  .import-btn {
    background: none; border: 1px dashed var(--border); color: var(--accent);
    padding: 10px; border-radius: var(--radius-sm); cursor: pointer; font-size: 14px;
    transition: all 0.2s; margin-top: 8px;
  }

  .import-btn:hover { border-color: var(--accent); background: var(--accent-glow); }
</style>
