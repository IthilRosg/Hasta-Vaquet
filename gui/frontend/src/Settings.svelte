<script lang="ts">
  import { fade, scale } from 'svelte/transition'
  import { GetKillSwitchEnabled, SetKillSwitchEnabled, GetBypassMode, SetBypassMode, GetBypassCIDRs, SetBypassCIDRs, GetRussianBankPreset, GetVPNCIDRs, SetVPNCIDRs, GetGameAntiVPNPreset, DownloadAntiFilter } from '../wailsjs/go/main/App'

  export let show: boolean
  export let serverIP: string
  export let port: number
  export let shortID: number
  export let secretKey: string
  export let routingSalt: string
  export let internalIP: string
  export let gatewayIP: string
  export let dns: string
  export let transport: string
  export let cdnDomain: string

  export let onImport: () => void
  export let onClose: () => void

  const tabs = ['Connection', 'Kill Switch', 'Transport', 'Bypass']
  let activeTab = 'Connection'
  let killSwitchOn = true
  let bypassMode = ''
  let bypassCIDRs: string[] = []
  let vpnCIDRs: string[] = []
  let newCIDR = ''
  let bypassError = ''
  let antiFilterStatus = ''
  let activeList: 'bypass' | 'vpn' = 'bypass'

  $: {
    if (show) {
      GetKillSwitchEnabled().then(v => killSwitchOn = v)
      GetBypassMode().then(v => bypassMode = v || 'off')
      GetBypassCIDRs().then(v => bypassCIDRs = v || [])
      GetVPNCIDRs().then(v => vpnCIDRs = v || [])
    }
  }

  async function toggleKillSwitch() {
    killSwitchOn = !killSwitchOn
    await SetKillSwitchEnabled(killSwitchOn)
  }

  async function setBypassMode(mode: string) {
    bypassMode = mode
    await SetBypassMode(mode === 'off' ? '' : mode)
  }

  async function addCIDR() {
    bypassError = ''
    const cidr = newCIDR.trim()
    if (!cidr) return
    if (!/^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/.test(cidr)) {
      bypassError = 'Invalid CIDR format. Example: 5.45.192.0/24'
      return
    }
    const list = bypassMode === 'vpn_only' ? vpnCIDRs : bypassCIDRs
    if (list.includes(cidr)) {
      bypassError = 'CIDR already in list'
      return
    }
    if (bypassMode === 'vpn_only') {
      vpnCIDRs = [...vpnCIDRs, cidr]
      await SetVPNCIDRs(vpnCIDRs)
    } else {
      bypassCIDRs = [...bypassCIDRs, cidr]
      await SetBypassCIDRs(bypassCIDRs)
    }
    newCIDR = ''
  }

  async function removeCIDR(cidr: string) {
    if (bypassMode === 'vpn_only') {
      vpnCIDRs = vpnCIDRs.filter(c => c !== cidr)
      await SetVPNCIDRs(vpnCIDRs)
    } else {
      bypassCIDRs = bypassCIDRs.filter(c => c !== cidr)
      await SetBypassCIDRs(bypassCIDRs)
    }
  }

  async function loadRussianBanks() {
    const preset = await GetRussianBankPreset()
    bypassCIDRs = [...new Set([...bypassCIDRs, ...preset])]
    await SetBypassCIDRs(bypassCIDRs)
  }

  async function loadGamePreset() {
    const preset = await GetGameAntiVPNPreset()
    if (bypassMode === 'vpn_only') {
      vpnCIDRs = [...new Set([...vpnCIDRs, ...preset])]
      await SetVPNCIDRs(vpnCIDRs)
    } else {
      bypassCIDRs = [...new Set([...bypassCIDRs, ...preset])]
      await SetBypassCIDRs(bypassCIDRs)
    }
  }

  async function loadAntiFilter() {
    antiFilterStatus = 'Downloading...'
    try {
      const result = await DownloadAntiFilter()
      if (result && result.length > 0) {
        vpnCIDRs = [...new Set([...vpnCIDRs, ...result])]
        await SetVPNCIDRs(vpnCIDRs)
        bypassMode = 'vpn_only'
        await SetBypassMode('vpn_only')
        antiFilterStatus = `Loaded ${result.length} aggregated CIDRs`
      } else {
        antiFilterStatus = 'No CIDRs loaded'
      }
    } catch (e) {
      antiFilterStatus = 'Error: ' + e
    }
  }

  async function clearAll() {
    bypassCIDRs = []
    vpnCIDRs = []
    await SetBypassCIDRs([])
    await SetVPNCIDRs([])
  }
</script>

{#if show}
<div class="settings-overlay" on:click={onClose} transition:fade={{ duration: 150 }}>
  <div class="settings-dialog" on:click|stopPropagation transition:scale={{ start: 0.95, duration: 150 }}>
    <div class="dialog-sidebar">
      <div class="dialog-title">Settings</div>
      {#each tabs as tab}
      <button
        class="tab-btn"
        class:active={activeTab === tab}
        on:click={() => activeTab = tab}
      >
        {tab}
      </button>
      {/each}
    </div>

    <div class="dialog-content">
      <button class="close-btn" on:click={onClose}>
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
        </svg>
      </button>

      {#if activeTab === 'Connection'}
      <h3>Connection</h3>
      <div class="section">
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
        <button class="action-btn" on:click={onImport}>Import Profile (.json)</button>
      </div>

      {:else if activeTab === 'Kill Switch'}
      <h3>Kill Switch</h3>
      <div class="section">
        <div class="toggle-row">
          <div class="toggle-info">
            <div class="toggle-label">Enable Kill Switch</div>
            <div class="toggle-desc">When active, blocks all network traffic if the VPN connection drops — prevents IP leaks</div>
          </div>
          <label class="toggle">
            <input type="checkbox" checked={killSwitchOn} on:change={toggleKillSwitch}/>
            <span class="slider"></span>
          </label>
        </div>
        <div class="note">
          Windows: removes default route on disconnect, restores on reconnect.
          Android: handled by VpnService.Builder.setBlocking(true).
        </div>
      </div>

      {:else if activeTab === 'Transport'}
      <h3>Transport</h3>
      <div class="section">
        <div class="toggle-group">
          {#each [
            {val: 'auto', label: 'Auto', desc: 'WSS → WS → QUIC → UDP, best effort'},
            {val: 'wss', label: 'WSS', desc: '443, TLS + uTLS Chrome, CDN-ready'},
            {val: 'ws', label: 'WS', desc: '19998, plain WebSocket, no TLS'},
            {val: 'quic', label: 'QUIC', desc: '19999, QUIC Short Header, masked'},
            {val: 'udp', label: 'UDP', desc: '19999, raw format, legacy'},
          ] as t}
          <button
            class="transport-toggle"
            class:active={transport === t.val}
            on:click={() => transport = t.val}
          >
            <div class="toggle-name">{t.label}</div>
            <div class="toggle-desc">{t.desc}</div>
          </button>
          {/each}
        </div>
        <div class="field">
          <label>CDN Domain (for WSS)</label>
          <input bind:value={cdnDomain} placeholder="your-vpn.domain.com"/>
          <div class="field-hint">Set when using Cloudflare CDN. WSS will connect via this domain.</div>
        </div>
        <div class="note">Reality (Xray) — SOCKS5 proxy on 127.0.0.1:1080, connect via WS transport through it.</div>
      </div>

      {:else if activeTab === 'Bypass'}
      <h3>Smart Bypass</h3>
      <div class="section">
        <div class="toggle-group">
          {#each [
            {val: 'off', label: 'Off', desc: 'All traffic through VPN'},
            {val: 'bypass', label: 'Bypass Mode', desc: 'Traffic to listed CIDRs goes direct, rest through VPN'},
            {val: 'vpn_only', label: 'VPN Only Mode', desc: 'Only traffic to listed CIDRs goes through VPN'},
          ] as m}
          <button
            class="transport-toggle"
            class:active={bypassMode === m.val}
            on:click={() => setBypassMode(m.val)}
          >
            <div class="toggle-name">{m.label}</div>
            <div class="toggle-desc">{m.desc}</div>
          </button>
          {/each}
        </div>

        {#if bypassMode !== 'off'}
        <div class="cidr-section">
          <div class="field-row">
            <div class="field" style="flex: 1;">
              <label>{bypassMode === 'vpn_only' ? 'VPN CIDRs (through tunnel)' : 'Bypass CIDRs (direct)'}</label>
              <input
                bind:value={newCIDR}
                placeholder="5.45.192.0/24"
                on:keydown={(e) => e.key === 'Enter' && addCIDR()}
              />
            </div>
            <button class="action-btn" style="margin-top: 18px; width: auto; padding: 8px 20px;" on:click={addCIDR}>Add</button>
          </div>
          {#if bypassError}
          <div class="error">{bypassError}</div>
          {/if}

          <div class="preset-row">
            {#if bypassMode !== 'vpn_only'}
            <button class="preset-btn" on:click={loadRussianBanks}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/></svg>
              Russian Banks
            </button>
            {/if}
            <button class="preset-btn" on:click={loadGamePreset}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 11h-5V6a1 1 0 0 0-2 0v5H6a1 1 0 0 0 0 2h5v5a1 1 0 0 0 2 0v-5h5a1 1 0 0 0 0-2z"/></svg>
              {bypassMode === 'vpn_only' ? 'Game VPN' : 'Game Anti-VPN'}
            </button>
            {#if bypassMode === 'vpn_only'}
            <button class="preset-btn" on:click={loadAntiFilter}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/></svg>
              AntiFilter
            </button>
            {/if}
            {#if (bypassMode === 'vpn_only' ? vpnCIDRs : bypassCIDRs).length > 0}
            <button class="preset-btn danger" on:click={clearAll}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
              Clear All
            </button>
            {/if}
          </div>

          {#if antiFilterStatus}
          <div class="note">{antiFilterStatus}</div>
          {/if}

          <div class="cidr-list">
            {#if bypassMode === 'vpn_only'}
              {#if vpnCIDRs.length === 0}
              <div class="empty">No VPN CIDRs. Traffic will not be tunneled.</div>
              {:else}
              {#each vpnCIDRs as cidr}
              <div class="cidr-item">
                <code>{cidr}</code>
                <button class="remove-btn" on:click={() => removeCIDR(cidr)}>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                </button>
              </div>
              {/each}
              {/if}
            {:else}
              {#if bypassCIDRs.length === 0}
              <div class="empty">No CIDRs added. Traffic will not be bypassed.</div>
              {:else}
              {#each bypassCIDRs as cidr}
              <div class="cidr-item">
                <code>{cidr}</code>
                <button class="remove-btn" on:click={() => removeCIDR(cidr)}>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                </button>
              </div>
              {/each}
              {/if}
            {/if}
          </div>

          <div class="note">
            {#if bypassMode === 'bypass'}
            <strong>Bypass Mode:</strong> Traffic to listed CIDRs goes DIRECT (bypasses VPN).
            Use for Russian banks, game servers that block VPN, government services.
            {:else}
            <strong>VPN Only Mode:</strong> Only traffic to listed CIDRs goes through the VPN tunnel.
            Everything else is direct. Use with AntiFilter preset to only tunnel blocked sites.
            {/if}
          </div>
        </div>
        {/if}
      </div>
      {/if}
    </div>
  </div>
</div>
{/if}

<style>
  .toggle-group {
    display: flex; flex-direction: column; gap: 6px;
  }
  .transport-toggle {
    display: flex; flex-direction: column; gap: 2px;
    width: 100%; padding: 10px 14px;
    background: rgba(255,255,255,0.03);
    border: 1px solid var(--border);
    border-radius: 10px;
    color: var(--text); cursor: pointer;
    text-align: left;
    transition: all 0.15s;
  }
  .transport-toggle:hover { background: rgba(255,255,255,0.06); border-color: var(--accent); }
  .transport-toggle.active {
    background: rgba(63,185,80,0.08);
    border-color: var(--accent);
  }
  .transport-toggle .toggle-name {
    font-size: 14px; font-weight: 600;
  }
  .transport-toggle.active .toggle-name { color: var(--accent); }
  .transport-toggle .toggle-desc {
    font-size: 11px; color: var(--text-dim);
  }

  .field-hint {
    font-size: 11px; color: var(--text-dim);
    margin-top: 4px; line-height: 1.4;
  }

  .settings-overlay {
    position: fixed; inset: 0;
    background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(4px);
    -webkit-backdrop-filter: blur(4px);
    z-index: 100;
    display: flex; align-items: center; justify-content: center;
  }

  .settings-dialog {
    display: flex;
    width: 640px; height: 460px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 16px;
    overflow: hidden;
    box-shadow: 0 16px 64px rgba(0,0,0,0.5);
  }

  /* Sidebar */
  .dialog-sidebar {
    width: 170px; flex-shrink: 0;
    background: rgba(255,255,255,0.03);
    border-right: 1px solid var(--border);
    padding: 20px 0;
    display: flex; flex-direction: column; gap: 2px;
  }

  .dialog-title {
    font-size: 14px; font-weight: 700; color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 1px;
    padding: 0 16px 16px; border-bottom: 1px solid var(--border);
    margin-bottom: 8px;
  }

  .tab-btn {
    display: block; width: 100%; text-align: left;
    padding: 10px 16px;
    background: none; border: none; border-right: 2px solid transparent;
    color: var(--text-dim); cursor: pointer;
    font-size: 14px; font-weight: 500;
    transition: all 0.15s;
  }
  .tab-btn:hover { color: var(--text); background: rgba(255,255,255,0.04); }
  .tab-btn.active { color: var(--accent); border-right-color: var(--accent); background: rgba(63,185,80,0.06); }

  /* Content */
  .dialog-content {
    flex: 1; padding: 24px;
    overflow-y: auto; position: relative;
  }

  .close-btn {
    position: absolute; top: 12px; right: 12px;
    background: none; border: none; color: var(--text-dim);
    cursor: pointer; padding: 4px; border-radius: 6px;
    transition: all 0.15s;
  }
  .close-btn:hover { color: var(--text); background: rgba(255,255,255,0.06); }

  h3 {
    font-size: 18px; font-weight: 600;
    margin-bottom: 20px;
  }

  .section {
    display: flex; flex-direction: column; gap: 14px;
  }

  .field { display: flex; flex-direction: column; gap: 4px; flex: 1; }
  .field-row { display: flex; gap: 12px; }
  .field label {
    font-size: 11px; color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.5px;
  }
  .field input {
    background: var(--bg); border: 1px solid var(--border);
    color: var(--text); padding: 8px 12px;
    border-radius: 8px; font-size: 14px; width: 100%;
    transition: border 0.2s;
  }
  .field input:focus { outline: none; border-color: var(--accent); }

  .action-btn {
    background: none; border: 1px dashed var(--border);
    color: var(--accent); padding: 10px;
    border-radius: 8px; cursor: pointer;
    font-size: 14px; transition: all 0.2s; margin-top: 4px;
  }
  .action-btn:hover { border-color: var(--accent); background: var(--accent-glow); }

  /* Kill Switch toggle */
  .toggle-row {
    display: flex; align-items: flex-start; justify-content: space-between;
    gap: 24px; padding: 16px;
    background: rgba(255,255,255,0.03);
    border: 1px solid var(--border);
    border-radius: 12px;
  }

  .toggle-info { flex: 1; }
  .toggle-label { font-size: 15px; font-weight: 600; margin-bottom: 6px; }
  .toggle-desc { font-size: 12px; color: var(--text-dim); line-height: 1.5; }

  .toggle {
    position: relative; display: inline-block;
    width: 44px; height: 24px; flex-shrink: 0; margin-top: 2px;
  }
  .toggle input { opacity: 0; width: 0; height: 0; }
  .slider {
    position: absolute; cursor: pointer;
    inset: 0; background: rgba(255,255,255,0.12);
    border-radius: 24px; transition: 0.25s;
  }
  .slider::before {
    content: '';
    position: absolute; width: 18px; height: 18px;
    left: 3px; bottom: 3px;
    background: white; border-radius: 50%;
    transition: 0.25s;
  }
  .toggle input:checked + .slider { background: var(--accent); }
  .toggle input:checked + .slider::before { transform: translateX(20px); }

  .note {
    font-size: 12px; color: var(--text-dim);
    padding: 12px; background: rgba(255,255,255,0.03);
    border-radius: 8px; line-height: 1.5;
  }

  .note strong { color: var(--text); }

  /* Bypass UI */
  .cidr-section {
    display: flex; flex-direction: column; gap: 12px;
    padding: 16px; background: rgba(255,255,255,0.02);
    border: 1px solid var(--border); border-radius: 12px;
  }

  .error {
    font-size: 12px; color: #ff4444;
    padding: 6px 10px; background: rgba(255,68,68,0.1);
    border-radius: 6px;
  }

  .preset-row {
    display: flex; gap: 10px; flex-wrap: wrap;
  }

  .preset-btn {
    display: inline-flex; align-items: center; gap: 8px;
    padding: 8px 16px;
    background: rgba(255,255,255,0.04);
    border: 1px solid var(--border); border-radius: 8px;
    color: var(--text); cursor: pointer;
    font-size: 13px; font-weight: 500;
    transition: all 0.15s;
  }
  .preset-btn:hover { background: rgba(63,185,80,0.08); border-color: var(--accent); }
  .preset-btn.danger:hover { background: rgba(255,68,68,0.1); border-color: #ff4444; }

  .cidr-list {
    display: flex; flex-direction: column; gap: 4px;
    max-height: 180px; overflow-y: auto;
  }

  .cidr-item {
    display: flex; align-items: center; justify-content: space-between;
    padding: 8px 12px;
    background: rgba(255,255,255,0.03);
    border: 1px solid var(--border); border-radius: 8px;
    transition: background 0.15s;
  }
  .cidr-item:hover { background: rgba(255,255,255,0.06); }
  .cidr-item code {
    font-size: 13px; font-family: 'JetBrains Mono', 'Fira Code', monospace;
    color: var(--accent);
  }

  .remove-btn {
    background: none; border: none;
    color: var(--text-dim); cursor: pointer;
    padding: 4px; border-radius: 4px;
    transition: all 0.15s;
  }
  .remove-btn:hover { color: #ff4444; background: rgba(255,68,68,0.1); }

  .empty {
    font-size: 13px; color: var(--text-dim);
    text-align: center; padding: 24px;
  }

  .placeholder {
    display: flex; flex-direction: column; align-items: center;
    gap: 12px; padding: 40px 0;
    color: var(--text-dim);
  }
  .placeholder p { font-size: 14px; }
</style>
