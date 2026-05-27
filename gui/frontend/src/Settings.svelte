<script lang="ts">
  import { fade, scale } from 'svelte/transition'
  import { GetKillSwitchEnabled, SetKillSwitchEnabled } from '../wailsjs/go/main/App'

  export let show: boolean
  export let serverIP: string
  export let port: number
  export let shortID: number
  export let secretKey: string
  export let routingSalt: string
  export let internalIP: string
  export let gatewayIP: string
  export let dns: string

  export let onImport: () => void
  export let onClose: () => void

  const tabs = ['Connection', 'Kill Switch', 'Advanced']
  let activeTab = 'Connection'
  let killSwitchOn = true

  $: {
    if (show) {
      GetKillSwitchEnabled().then(v => killSwitchOn = v)
    }
  }

  async function toggleKillSwitch() {
    killSwitchOn = !killSwitchOn
    await SetKillSwitchEnabled(killSwitchOn)
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

      {:else if activeTab === 'Advanced'}
      <h3>Advanced</h3>
      <div class="section">
        <div class="placeholder">
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" opacity="0.3">
            <circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/>
          </svg>
          <p>Additional settings coming soon</p>
        </div>
      </div>
      {/if}
    </div>
  </div>
</div>
{/if}

<style>
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

  .placeholder {
    display: flex; flex-direction: column; align-items: center;
    gap: 12px; padding: 40px 0;
    color: var(--text-dim);
  }
  .placeholder p { font-size: 14px; }
</style>
