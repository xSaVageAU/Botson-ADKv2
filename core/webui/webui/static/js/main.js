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

window.loadUsers = async function () {
  try {
    const res = await fetch('/botson/api/users');
    if (!res.ok) throw new Error('failed to load users');
    const users = await res.json();

    const label = document.getElementById('currentUserLabel');
    if (label) {
      label.textContent = window.currentUser;
    }

    const menu = document.getElementById('userDropdownMenu');
    if (menu) {
      menu.innerHTML = '';
      users.forEach(u => {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = `dropdown-item ${u === window.currentUser ? 'active' : ''}`;
        btn.textContent = u;
        btn.onclick = async (e) => {
          e.stopPropagation();
          menu.classList.remove('show');
          await window.changeCurrentUser(u);
        };
        menu.appendChild(btn);
      });
    }
  } catch (err) {
    console.error('Error fetching users:', err);
  }
};

window.toggleUserDropdown = function (event) {
  if (event) event.stopPropagation();
  const menu = document.getElementById('userDropdownMenu');
  if (menu) {
    menu.classList.toggle('show');
  }
};

// Global click-outside listener to close custom dropdown
document.addEventListener('click', () => {
  const menu = document.getElementById('userDropdownMenu');
  if (menu) {
    menu.classList.remove('show');
  }
});

window.changeCurrentUser = async function (newUser) {
  window.currentUser = newUser;

  const label = document.getElementById('currentUserLabel');
  if (label) {
    label.textContent = newUser;
  }

  // Update active states on item buttons
  document.querySelectorAll('#userDropdownMenu .dropdown-item').forEach(btn => {
    btn.classList.toggle('active', btn.textContent === newUser);
  });

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
window.switchView = async function (viewName) {
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
window.escapeHtml = function (str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
};

window.showToast = function (message, type = 'success') {
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
window.togglePasswordVisibility = function (id) {
  const input = document.getElementById(id);
  if (!input) return;
  input.type = input.type === 'password' ? 'text' : 'password';
};

window.toggleDiscordFields = function (checked) {
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

window.loadSettings = async function () {
  try {
    const res = await fetch('/botson/api/config');
    if (!res.ok) throw new Error('Failed to load settings');
    const cfg = await res.json();

    document.getElementById('geminiApiKeyInput').value = cfg.gemini_api_key || '';
    document.getElementById('discordEnabledInput').checked = !!(cfg.discord && cfg.discord.enabled);
    document.getElementById('discordTokenInput').value = (cfg.discord && cfg.discord.token) || '';
    document.getElementById('discordOwnerIdInput').value = (cfg.discord && cfg.discord.owner_id) || '';

    // Save whitelist locally to preserve on settings save
    window.currentWhitelist = (cfg.discord && cfg.discord.whitelist) || [];

    window.toggleDiscordFields(!!(cfg.discord && cfg.discord.enabled));

    // Populate Access Control lists
    await window.renderAccessControl(window.currentWhitelist);

    // Fetch stats to get all available agent names
    try {
      const statsRes = await fetch('/botson/api/stats');
      if (statsRes.ok) {
        const stats = await statsRes.json();
        const rootAgentSelect = document.getElementById('rootAgentSelect');
        if (rootAgentSelect) {
          rootAgentSelect.innerHTML = '';
          if (stats.agents) {
            stats.agents.forEach(ag => {
              const opt = document.createElement('option');
              opt.value = ag.name;
              opt.textContent = ag.name;
              if (ag.name === cfg.root_agent) {
                opt.selected = true;
              }
              rootAgentSelect.appendChild(opt);
            });
          }
        }
      }
    } catch (statsErr) {
      console.error('Failed to load agents list for root select:', statsErr);
    }
  } catch (err) {
    window.showToast('Failed to load configuration settings', 'error');
  }
};

window.renderAccessControl = async function (whitelist) {
  // 1. Render Active Whitelist List
  const wlList = document.getElementById('activeWhitelistList');
  if (wlList) {
    wlList.innerHTML = '';
    if (!whitelist || whitelist.length === 0) {
      wlList.innerHTML = `<div class="empty-table-state">No users whitelisted yet.</div>`;
    } else {
      whitelist.forEach(uid => {
        const div = document.createElement('div');
        div.className = 'access-item';
        div.innerHTML = `
          <span style="font-family: var(--font-mono); font-size: 13px; color: var(--text);">${window.escapeHtml(uid)}</span>
          <button type="button" class="btn-action-revoke" onclick="window.revokeUserAccess('${uid}')">Revoke Access</button>
        `;
        wlList.appendChild(div);
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

    const pendList = document.getElementById('pendingAuthsList');
    if (pendList) {
      pendList.innerHTML = '';
      if (!pending || pending.length === 0) {
        pendList.innerHTML = `<div class="empty-table-state">No pending authorization requests.</div>`;
      } else {
        pending.forEach(req => {
          const div = document.createElement('div');
          div.className = 'access-item';
          div.innerHTML = `
            <div class="user-info">
              <span class="username">${window.escapeHtml(req.username)}</span>
              <span class="userid">${window.escapeHtml(req.user_id)}</span>
            </div>
            <span class="auth-code-badge">${window.escapeHtml(req.code)}</span>
            <button type="button" class="btn-action-approve" onclick="window.approveUserAccess('${req.code}')">Approve</button>
          `;
          pendList.appendChild(div);
        });
      }
    }
  } catch (err) {
    console.error('Error fetching pending authorizations:', err);
  }
};

window.approveUserAccess = async function (code) {
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

window.revokeUserAccess = async function (userID) {
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

window.saveSettings = async function (event) {
  if (event) event.preventDefault();

  const payload = {
    model_name: "gemini-3.1-flash-lite", // Retain standard model default
    gemini_api_key: document.getElementById('geminiApiKeyInput').value.trim(),
    root_agent: document.getElementById('rootAgentSelect') ? document.getElementById('rootAgentSelect').value : "Agent Botson",
    discord: {
      enabled: document.getElementById('discordEnabledInput').checked,
      token: document.getElementById('discordTokenInput').value.trim(),
      owner_id: document.getElementById('discordOwnerIdInput').value.trim(),
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
