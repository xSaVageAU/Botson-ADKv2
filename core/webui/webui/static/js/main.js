// Main SPA Controller & Shared Workspace State

// Shared state between views (attached to global window scope)
window.activeView = 'dashboard';

// Chat state
window.activeAgent = null;
window.activeSessionId = null;
window.isNewSession = false;
window.activeInspectorTab = 'state';
window.currentUser = 'user';

// Builder state
window.allAgents = [];
window.allTools = { standard: [], agents: [] };
window.currentAgent = null;

// Initial bootstrap
window.addEventListener('DOMContentLoaded', async () => {
  await window.loadUsers();
  await window.switchView('dashboard');
});

window.loadUsers = async function() {
  try {
    const res = await fetch('/botson/api/users');
    if (!res.ok) throw new Error('failed to load users');
    const users = await res.json();
    
    const select = document.getElementById('userSelect');
    if (select) {
      select.innerHTML = '';
      users.forEach(u => {
        const opt = document.createElement('option');
        opt.value = u;
        opt.textContent = u;
        if (u === window.currentUser) {
          opt.selected = true;
        }
        select.appendChild(opt);
      });
    }
  } catch (err) {
    console.error('Error fetching users:', err);
  }
};

window.changeCurrentUser = async function(newUser) {
  window.currentUser = newUser;
  window.showToast(`Switched context to user: ${newUser}`, 'success');
  
  if (window.activeView === 'dashboard') {
    if (typeof window.loadStats === 'function') {
      await window.loadStats();
    }
  } else if (window.activeView === 'chat') {
    window.activeSessionId = null;
    window.isNewSession = false;
    
    const pane = document.getElementById('chatDisplayPane');
    if (pane) {
      pane.innerHTML = '<div class="empty-state"><h3>No active session</h3><p>Select an agent and a session from the rail, or start a new one.</p></div>';
    }
    
    const inspector = document.getElementById('inspectorPanel');
    if (inspector) inspector.classList.remove('active');
    
    // Refresh sessions list
    if (window.activeAgent && typeof window.loadSessionsForAgent === 'function') {
      await window.loadSessionsForAgent(window.activeAgent);
    } else if (typeof window.loadAgentsForChat === 'function') {
      await window.loadAgentsForChat();
    }
  }
};

// View switching logic (SPA Tab Manager)
window.switchView = async function(viewName) {
  window.activeView = viewName;
  
  // Update nav buttons active state
  document.querySelectorAll('.nav-btn').forEach(btn => {
    btn.classList.toggle('active', btn.id === `tab-${viewName}`);
  });

  // Update view panel active state
  document.querySelectorAll('.view-panel').forEach(panel => {
    panel.classList.toggle('active', panel.id === `view-${viewName}`);
  });

  // Trigger view specific load actions
  if (viewName === 'dashboard') {
    if (typeof window.loadStats === 'function') {
      await window.loadStats();
    }
  } else if (viewName === 'chat') {
    if (typeof window.loadAgentsForChat === 'function') {
      await window.loadAgentsForChat();
    }
  } else if (viewName === 'builder') {
    if (typeof window.loadToolsForBuilder === 'function') {
      await window.loadToolsForBuilder();
    }
    if (typeof window.loadAgentsForBuilder === 'function') {
      await window.loadAgentsForBuilder();
    }
  } else if (viewName === 'settings') {
    if (typeof window.loadSettings === 'function') {
      await window.loadSettings();
    }
  }
};

// Global Helpers
window.escapeHtml = function(str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
};

window.showToast = function(message, type = 'success') {
  const rack = document.getElementById('toastContainer');
  if (!rack) return;
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.textContent = message;
  
  rack.appendChild(toast);
  
  setTimeout(() => {
    toast.style.opacity = '0';
    toast.style.transition = 'opacity 0.25s ease';
    setTimeout(() => toast.remove(), 250);
  }, 4000);
};

/* ==================== SETTINGS SCRIPTS ==================== */
window.togglePasswordVisibility = function(id) {
  const input = document.getElementById(id);
  if (!input) return;
  input.type = input.type === 'password' ? 'text' : 'password';
};

window.toggleDiscordFields = function(checked) {
  const container = document.getElementById('discordFieldsContainer');
  if (!container) return;
  
  if (checked) {
    container.classList.remove('disabled-fields');
  } else {
    container.classList.add('disabled-fields');
  }
  
  // Update disabled state of input fields
  container.querySelectorAll('input').forEach(inp => {
    inp.disabled = !checked;
  });
};

window.loadSettings = async function() {
  try {
    const res = await fetch('/botson/api/config');
    if (!res.ok) throw new Error('Failed to load settings');
    const cfg = await res.json();
    
    document.getElementById('geminiApiKeyInput').value = cfg.gemini_api_key || '';
    document.getElementById('discordEnabledInput').checked = !!(cfg.discord && cfg.discord.enabled);
    document.getElementById('discordTokenInput').value = (cfg.discord && cfg.discord.token) || '';
    document.getElementById('discordOwnerIdInput').value = (cfg.discord && cfg.discord.owner_id) || '';
    document.getElementById('discordGuildIdInput').value = (cfg.discord && cfg.discord.guild_id) || '';
    document.getElementById('discordLogChannelIdInput').value = (cfg.discord && cfg.discord.log_channel_id) || '';
    
    // Save whitelist locally to preserve on settings save
    window.currentWhitelist = (cfg.discord && cfg.discord.whitelist) || [];
    
    window.toggleDiscordFields(!!(cfg.discord && cfg.discord.enabled));
    
    // Populate Access Control lists
    await window.renderAccessControl(window.currentWhitelist);
  } catch (err) {
    window.showToast('Failed to load configuration settings', 'error');
  }
};

window.renderAccessControl = async function(whitelist) {
  // 1. Render Active Whitelist Table
  const wlBody = document.getElementById('activeWhitelistBody');
  if (wlBody) {
    wlBody.innerHTML = '';
    if (!whitelist || whitelist.length === 0) {
      wlBody.innerHTML = `<tr><td colspan="2" class="empty-table-state">No users whitelisted yet.</td></tr>`;
    } else {
      whitelist.forEach(uid => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
          <td style="font-family: var(--font-mono); font-size: 13px; color: var(--text);">${window.escapeHtml(uid)}</td>
          <td>
            <button type="button" class="btn-action-revoke" onclick="window.revokeUserAccess('${uid}')">Revoke Access</button>
          </td>
        `;
        wlBody.appendChild(tr);
      });
    }
  }
  
  // Hide Access Control Card if Discord bot is disabled entirely
  const enabled = document.getElementById('discordEnabledInput').checked;
  const acCard = document.getElementById('discordAccessControlCard');
  if (acCard) {
    if (enabled) {
      acCard.style.display = 'flex';
    } else {
      acCard.style.display = 'none';
    }
  }

  if (!enabled) return;

  // 2. Fetch and Render Pending Authorization Requests
  try {
    const res = await fetch('/botson/api/discord/pending');
    if (!res.ok) throw new Error('Failed to fetch pending requests');
    const pending = await res.json();
    
    const pendBody = document.getElementById('pendingAuthsBody');
    if (pendBody) {
      pendBody.innerHTML = '';
      if (!pending || pending.length === 0) {
        pendBody.innerHTML = `<tr><td colspan="4" class="empty-table-state">No pending authorization requests.</td></tr>`;
      } else {
        pending.forEach(req => {
          const tr = document.createElement('tr');
          tr.innerHTML = `
            <td style="font-weight: 500;">${window.escapeHtml(req.username)}</td>
            <td style="font-family: var(--font-mono); font-size: 12px; color: var(--text-muted);">${window.escapeHtml(req.user_id)}</td>
            <td style="font-family: var(--font-mono); font-weight: 600; color: var(--accent);">${window.escapeHtml(req.code)}</td>
            <td>
              <button type="button" class="btn-action-approve" onclick="window.approveUserAccess('${req.code}')">Approve</button>
            </td>
          `;
          pendBody.appendChild(tr);
        });
      }
    }
  } catch (err) {
    console.error('Error fetching pending authorizations:', err);
  }
};

window.approveUserAccess = async function(code) {
  try {
    const res = await fetch('/botson/api/discord/approve', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code: code })
    });
    if (!res.ok) {
      const errText = await res.text();
      throw new Error(errText || 'Failed to approve user');
    }
    
    window.showToast('User authorized and added to whitelist', 'success');
    await window.loadSettings();
  } catch (err) {
    window.showToast('Failed to approve user: ' + err.message, 'error');
  }
};

window.revokeUserAccess = async function(userID) {
  if (!confirm(`Are you sure you want to revoke whitelist access for user ID ${userID}?`)) return;
  
  try {
    const res = await fetch('/botson/api/discord/remove-whitelisted', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ user_id: userID })
    });
    if (!res.ok) {
      const errText = await res.text();
      throw new Error(errText || 'Failed to revoke access');
    }
    
    window.showToast('User access revoked successfully', 'success');
    await window.loadSettings();
  } catch (err) {
    window.showToast('Failed to revoke access: ' + err.message, 'error');
  }
};

window.saveSettings = async function(event) {
  if (event) event.preventDefault();
  
  const payload = {
    model_name: "gemini-3.1-flash-lite", // Retain standard model default
    gemini_api_key: document.getElementById('geminiApiKeyInput').value.trim(),
    discord: {
      enabled: document.getElementById('discordEnabledInput').checked,
      token: document.getElementById('discordTokenInput').value.trim(),
      owner_id: document.getElementById('discordOwnerIdInput').value.trim(),
      guild_id: document.getElementById('discordGuildIdInput').value.trim(),
      log_channel_id: document.getElementById('discordLogChannelIdInput').value.trim(),
      whitelist: window.currentWhitelist || []
    }
  };
  
  try {
    const res = await fetch('/botson/api/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    if (!res.ok) {
      const errText = await res.text();
      throw new Error(errText || 'Failed to save configuration');
    }
    
    window.showToast('Settings saved successfully', 'success');
    
    // Refresh user contexts list in switcher in case a new gateway enabled context is added
    await window.loadUsers();
    await window.loadSettings();
  } catch (err) {
    window.showToast('Failed to save settings: ' + err.message, 'error');
  }
};
