// JavaScript Logic for Workspace Dashboard

const API_BASE = '/dashboard/api';

window.addEventListener('DOMContentLoaded', async () => {
  await loadStats();
});

async function loadStats() {
  const statAgents = document.getElementById('statAgents');
  const statSessions = document.getElementById('statSessions');
  const statEvents = document.getElementById('statEvents');
  const statDbPath = document.getElementById('statDbPath');

  const agentsTableBody = document.querySelector('#agentsTable tbody');
  const sessionsTableBody = document.querySelector('#sessionsTable tbody');

  try {
    const res = await fetch(`${API_BASE}/stats`);
    if (!res.ok) throw new Error('Failed to load dashboard metrics');
    const stats = await res.json();

    // Populate overall statistics
    statAgents.textContent = stats.totalAgents;
    statSessions.textContent = stats.totalSessions;
    statEvents.textContent = stats.totalEvents;
    statDbPath.textContent = stats.dbPath;

    // Render agents table
    if (stats.agents && stats.agents.length > 0) {
      agentsTableBody.innerHTML = '';
      stats.agents.forEach(agent => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
          <td class="agent-name-cell">${escapeHtml(agent.name)}</td>
          <td>${escapeHtml(agent.description || 'No description')}</td>
          <td>
            <span class="badge ${agent.isRoot ? 'badge-primary' : 'badge-secondary'}">
              ${agent.isRoot ? 'root' : 'agent'}
            </span>
          </td>
        `;
        agentsTableBody.appendChild(tr);
      });
    } else {
      agentsTableBody.innerHTML = '<tr><td colspan="3" class="loading-cell">No agents registered</td></tr>';
    }

    // Render sessions table
    if (stats.recentSessions && stats.recentSessions.length > 0) {
      sessionsTableBody.innerHTML = '';
      stats.recentSessions.forEach(session => {
        const tr = document.createElement('tr');
        const timeStr = new Date(session.lastUpdateTime * 1000).toLocaleString();
        
        tr.innerHTML = `
          <td class="session-title-cell" title="${escapeHtml(session.id)}">
            <a href="/chat/" style="color: inherit; text-decoration: none; font-weight: 500;">
              ${escapeHtml(session.displayName || session.id)}
            </a>
          </td>
          <td class="mono">${escapeHtml(session.agentName)}</td>
          <td class="mono" style="font-size: 11px; color: var(--text-secondary);">${timeStr}</td>
        `;
        sessionsTableBody.appendChild(tr);
      });
    } else {
      sessionsTableBody.innerHTML = '<tr><td colspan="3" class="loading-cell">No recent chat sessions</td></tr>';
    }

  } catch (err) {
    console.error('Dashboard statistics load error:', err);
    statAgents.textContent = 'Err';
    statSessions.textContent = 'Err';
    statEvents.textContent = 'Err';
    statDbPath.textContent = err.message;
    
    agentsTableBody.innerHTML = `<tr><td colspan="3" class="loading-cell" style="color: var(--green);">Failed to load registry: ${escapeHtml(err.message)}</td></tr>`;
    sessionsTableBody.innerHTML = `<tr><td colspan="3" class="loading-cell" style="color: var(--green);">Failed to load sessions: ${escapeHtml(err.message)}</td></tr>`;
  }
}

function escapeHtml(str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}
