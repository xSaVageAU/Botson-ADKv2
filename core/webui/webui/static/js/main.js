// Main SPA Controller & Shared Workspace State

// Shared state between views (attached to global window scope)
window.activeView = 'dashboard';

// Chat state
window.activeAgent = null;
window.activeSessionId = null;
window.isNewSession = false;
window.activeInspectorTab = 'state';

// Builder state
window.allAgents = [];
window.allTools = { standard: [], agents: [] };
window.currentAgent = null;

// Initial bootstrap
window.addEventListener('DOMContentLoaded', async () => {
  await window.switchView('dashboard');
});

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
