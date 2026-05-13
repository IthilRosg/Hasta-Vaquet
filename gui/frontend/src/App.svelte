<script lang="ts">
  import { DoConnect, DoDisconnect, ImportConfig, LoadDefaultConfig } from '../wailsjs/go/main/App'
  import { EventsOn } from '../wailsjs/runtime/runtime'

  let connected = false
  let statusText = 'Disconnected'
  let txSpeed = '0 B/s'
  let rxSpeed = '0 B/s'
  let showSettings = false
  let animating = false

  let serverIP = '31.42.120.154'
  let port = 9999
  let shortID = 1
  let secretKey = ''
  let routingSalt = 'HastaVaquetGlobal'
  let internalIP = '10.0.0.10'
  let gatewayIP = '192.168.100.1'
  let dns = '1.1.1.1'

  async function loadConfig() {
    const cfg = await LoadDefaultConfig()
    if (cfg) {
      serverIP = cfg.server_ip || serverIP
      port = cfg.port || port
      shortID = cfg.short_id || shortID
      secretKey = cfg.secret_key || secretKey
      routingSalt = cfg.routing_salt || routingSalt
      internalIP = cfg.internal_ip || internalIP
      gatewayIP = cfg.gateway_ip || gatewayIP
      dns = cfg.dns || dns
    }
  }
  loadConfig()

  EventsOn('status', (s: string) => {
    statusText = s === 'connected' ? 'Connected' : 'Disconnected'
    connected = s === 'connected'
    if (!connected) { txSpeed = '0 B/s'; rxSpeed = '0 B/s' }
  })

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
    if (cfg.error) {
      statusText = cfg.error
      return
    }
    serverIP = cfg.server_ip
    port = cfg.port
    shortID = cfg.short_id
    secretKey = cfg.secret_key
    routingSalt = cfg.routing_salt
    internalIP = cfg.internal_ip
    gatewayIP = cfg.gateway_ip
    dns = cfg.dns || '1.1.1.1'
  }

  function toggleSettings() {
    showSettings = !showSettings
  }
</script>

<div class="container">
  <!-- Gear button -->
  <button class="gear" on:click={toggleSettings}>
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <circle cx="12" cy="12" r="3"/><path d="M12 1v2m0 18v2M4.22 4.22l1.42 1.42m12.72 12.72l1.42 1.42M1 12h2m18 0h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/>
    </svg>
  </button>

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
</div>

<!-- Settings panel -->
{#if showSettings}
<div class="overlay" on:click={toggleSettings}></div>
<div class="settings">
  <h2>Settings</h2>
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
  <div class="field-row">
    <div class="field">
      <label>Gateway IP</label>
      <input bind:value={gatewayIP} placeholder="192.168.100.1"/>
    </div>
    <div class="field">
      <label>DNS</label>
      <input bind:value={dns} placeholder="1.1.1.1"/>
    </div>
  </div>
  </div>
  <div class="field">
    <label>Routing Salt</label>
    <input bind:value={routingSalt} placeholder="Routing salt"/>
  </div>
  <button class="import-btn" on:click={importProfile}>Import Profile (.json)</button>
</div>
{/if}

<style>
  .container {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 24px;
    width: 100%;
    max-width: 360px;
  }

  .gear {
    position: fixed;
    top: 16px;
    right: 16px;
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    padding: 8px;
    border-radius: var(--radius-sm);
    transition: all 0.2s;
    z-index: 10;
  }

  .gear:hover { color: var(--text); background: var(--surface); }

  .button-wrapper {
    position: relative;
    border-radius: 50%;
    padding: 4px;
  }

  .button-wrapper.connected {
    animation: pulse 2s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { box-shadow: 0 0 0 0 var(--green-glow); }
    50% { box-shadow: 0 0 0 20px transparent; }
  }

  .big-btn {
    width: 140px;
    height: 140px;
    border-radius: 50%;
    border: 2px solid var(--border);
    background: var(--surface);
    color: var(--text);
    cursor: pointer;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  }

  .big-btn:hover {
    border-color: var(--accent);
    background: var(--surface-hover);
    transform: scale(1.05);
  }

  .big-btn:active {
    transform: scale(0.95);
  }

  .big-btn:disabled { opacity: 0.5; cursor: not-allowed; transform: none; }

  .button-wrapper.connected .big-btn {
    border-color: var(--green);
    background: rgba(63, 185, 80, 0.08);
  }

  .icon { display: flex; align-items: center; justify-content: center; }

  .label {
    font-size: 13px;
    font-weight: 600;
    letter-spacing: 0.5px;
    text-transform: uppercase;
  }

  .status {
    font-size: 14px;
    color: var(--text-dim);
    text-align: center;
    min-height: 20px;
  }

  .speeds {
    display: flex;
    align-items: center;
    gap: 20px;
    padding: 12px 24px;
    background: var(--surface);
    border-radius: var(--radius);
    border: 1px solid var(--border);
  }

  .speed-item {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 100px;
  }

  .speed-arrow { font-size: 18px; font-weight: 700; }
  .speed-arrow.up { color: var(--green); }
  .speed-arrow.down { color: var(--accent); }

  .speed-val {
    font-size: 14px;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
  }

  .speed-divider {
    width: 1px;
    height: 24px;
    background: var(--border);
  }

  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.5);
    z-index: 100;
  }

  .settings {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 320px;
    background: var(--surface);
    border-left: 1px solid var(--border);
    padding: 24px;
    overflow-y: auto;
    z-index: 101;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .settings h2 {
    font-size: 18px;
    font-weight: 600;
    margin-bottom: 8px;
  }

  .field { display: flex; flex-direction: column; gap: 4px; flex: 1; }

  .field-row {
    display: flex;
    gap: 12px;
  }

  .field label {
    font-size: 12px;
    color: var(--text-dim);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .field input {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 8px 12px;
    border-radius: var(--radius-sm);
    font-size: 14px;
    width: 100%;
    transition: border 0.2s;
  }

  .field input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .import-btn {
    background: none;
    border: 1px dashed var(--border);
    color: var(--accent);
    padding: 10px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: 14px;
    transition: all 0.2s;
    margin-top: 8px;
  }

  .import-btn:hover {
    border-color: var(--accent);
    background: var(--accent-glow);
  }
</style>
