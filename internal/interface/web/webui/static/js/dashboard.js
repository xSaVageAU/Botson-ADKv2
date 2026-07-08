// Dashboard View Controller

window.loadStats = async function() {
  const statAgents = document.getElementById('statAgents');
  const statSessions = document.getElementById('statSessions');
  const statEvents = document.getElementById('statEvents');
  const statDbPath = document.getElementById('statDbPath');

  const agentsTableBody = document.querySelector('#agentsTable tbody');
  const sessionsTableBody = document.querySelector('#sessionsTable tbody');

  try {
    const res = await fetch('/botson/api/stats');
    if (!res.ok) throw new Error('Failed to load dashboard metrics');
    const stats = await res.json();

    // Populate metrics
    if (statAgents) statAgents.textContent = stats.totalAgents;
    if (statSessions) statSessions.textContent = stats.totalSessions;
    if (statEvents) statEvents.textContent = stats.totalEvents;
    if (statDbPath) statDbPath.textContent = stats.dbPath || 'In-Memory';

    // Render agents table
    if (agentsTableBody) {
      if (stats.agents && stats.agents.length > 0) {
        agentsTableBody.innerHTML = '';
        stats.agents.forEach(agent => {
          const tr = document.createElement('tr');
          tr.innerHTML = `
            <td class="agent-name-cell">${window.escapeHtml(agent.name)}</td>
            <td>${window.escapeHtml(agent.description || 'No description')}</td>
            <td>
              <span class="badge ${agent.isRoot ? 'badge-primary' : 'badge-secondary'}">
                ${agent.isRoot ? 'root' : 'agent'}
              </span>
            </td>
            <td>
              <button class="btn btn-secondary" onclick="window.navigateToChat('${window.escapeHtml(agent.name)}')">Chat</button>
              <button class="btn btn-secondary" onclick="window.navigateToBuilder('${window.escapeHtml(agent.name)}')">Configure</button>
            </td>
          `;
          agentsTableBody.appendChild(tr);
        });
      } else {
        agentsTableBody.innerHTML = '<tr><td colspan="4" class="loading-cell">No agents registered</td></tr>';
      }
    }

    // Render sessions table
    if (sessionsTableBody) {
      if (stats.recentSessions && stats.recentSessions.length > 0) {
        sessionsTableBody.innerHTML = '';
        stats.recentSessions.forEach(session => {
          const tr = document.createElement('tr');
          const timeStr = new Date(session.lastUpdateTime * 1000).toLocaleString();
          
          tr.innerHTML = `
            <td class="session-title-cell" title="${window.escapeHtml(session.id)}">
              <a href="#" onclick="window.navigateToChat('${window.escapeHtml(session.agentName)}', '${window.escapeHtml(session.id)}'); return false;" style="color: inherit; text-decoration: none; font-weight: 500;">
                ${window.escapeHtml(session.displayName || session.id)}
              </a>
            </td>
            <td class="mono">${window.escapeHtml(session.agentName)}</td>
            <td class="mono" style="font-size: 11px; color: var(--text-faint);">${timeStr}</td>
          `;
          sessionsTableBody.appendChild(tr);
        });
      } else {
        sessionsTableBody.innerHTML = '<tr><td colspan="3" class="loading-cell">No recent chat sessions</td></tr>';
      }
    }

  } catch (err) {
    console.error('Dashboard load error:', err);
    if (statAgents) statAgents.textContent = 'Err';
    if (statSessions) statSessions.textContent = 'Err';
    if (statEvents) statEvents.textContent = 'Err';
    if (statDbPath) statDbPath.textContent = err.message;
    
    if (agentsTableBody) {
      agentsTableBody.innerHTML = `<tr><td colspan="4" class="loading-cell" style="color: var(--danger);">Failed to load registry: ${window.escapeHtml(err.message)}</td></tr>`;
    }
    if (sessionsTableBody) {
      sessionsTableBody.innerHTML = `<tr><td colspan="3" class="loading-cell" style="color: var(--danger);">Failed to load sessions: ${window.escapeHtml(err.message)}</td></tr>`;
    }
  }
};

// Navigation helper: route stats selection directly to Chat pane
window.navigateToChat = async function(agentName, sessionId = null) {
  window.activeAgent = agentName;
  window.activeSessionId = sessionId;
  await window.switchView('chat');
};

// Navigation helper: route stats selection directly to Config Builder form
window.navigateToBuilder = async function(agentName) {
  await window.switchView('builder');
  const agentObj = window.allAgents.find(a => a.name === agentName);
  if (agentObj) {
    window.selectAgent(agentObj);
  }
};
