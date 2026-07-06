let allAgents = [];
let allTools = { standard: [], agents: [] };
let currentAgent = null;

// Load initial data
window.addEventListener('DOMContentLoaded', async () => {
  await loadTools();
  await loadAgents();
});

async function loadAgents() {
  try {
    const res = await fetch('/api/agents');
    if (!res.ok) throw new Error('Failed to load agents list');
    allAgents = await res.json();
    renderSidebar();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

async function loadTools() {
  try {
    const res = await fetch('/api/tools');
    if (!res.ok) throw new Error('Failed to load tools list');
    allTools = await res.json();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

function renderSidebar() {
  const listEl = document.getElementById('agentList');
  const countEl = document.getElementById('agentCount');
  listEl.innerHTML = '';
  countEl.textContent = allAgents.length;

  allAgents.forEach(agent => {
    const li = document.createElement('li');
    li.className = `node-row ${currentAgent && currentAgent.name === agent.name ? 'active' : ''}`;
    li.onclick = () => selectAgent(agent);

    const glyphClasses = [
      'node-glyph',
      agent.is_root ? 'is-root' : '',
      agent.private ? 'is-private' : ''
    ].filter(Boolean).join(' ');

    li.innerHTML = `
      <div class="node-row-top">
        <span class="${glyphClasses}" title="${agent.is_root ? 'root agent' : agent.private ? 'private' : ''}"></span>
        <span class="node-name">${escapeHtml(agent.name)}</span>
        <span class="node-tag ${agent.read_only ? '' : 'tag-custom'}">${agent.read_only ? 'default' : 'custom'}</span>
      </div>
      <div class="node-row-bottom">
        <div class="node-desc">${escapeHtml(agent.description || 'No description')}</div>
        ${!agent.read_only ? `<button class="btn-delete" onclick="deleteAgent(event, '${agent.name}')">Delete</button>` : ''}
      </div>
    `;
    listEl.appendChild(li);
  });
}

function selectAgent(agent) {
  currentAgent = agent;
  renderSidebar();
  renderForm(agent);
}

// Re-registers custom tool checks, wiring strip updates and heading name updates
function openNewAgentForm() {
  currentAgent = null;
  renderSidebar();
  renderForm({
    name: '',
    description: '',
    is_root: false,
    private: false,
    tools: [],
    instructions: '',
    read_only: false
  });
}

function renderForm(agentData) {
  const isEdit = agentData.name !== '';
  const workspace = document.getElementById('workspace');

  const filteredSubagents = allTools.agents.filter(a => a !== agentData.name);
  const toolCount = (agentData.tools || []).filter(t => allTools.standard.includes(t)).length;
  const subCount = (agentData.tools || []).filter(t => filteredSubagents.includes(t)).length;

  let groupsHTML = '';

  groupsHTML += `
    <div class="group">
      <span class="group-title">Standard tools</span>
      <div class="tools-container">
        ${allTools.standard.map(tool => {
          const checked = agentData.tools && agentData.tools.includes(tool) ? 'checked' : '';
          return `
            <label class="tool-item">
              <input type="checkbox" name="agent_tools" data-kind="tool" value="${escapeHtml(tool)}" ${checked}>
              ${escapeHtml(tool)}
            </label>
          `;
        }).join('') || `<span class="wire-label">no standard tools available</span>`}
      </div>
    </div>
  `;

  if (filteredSubagents.length > 0) {
    groupsHTML += `
      <div class="group">
        <span class="group-title">Sub-agent delegation</span>
        <div class="tools-container">
          ${filteredSubagents.map(sub => {
            const checked = agentData.tools && agentData.tools.includes(sub) ? 'checked' : '';
            return `
              <label class="tool-item">
                <input type="checkbox" name="agent_tools" data-kind="sub" value="${escapeHtml(sub)}" ${checked}>
                ${escapeHtml(sub)}
              </label>
            `;
          }).join('')}
        </div>
      </div>
    `;
  }

  workspace.innerHTML = `
    <div class="editor">
      <div class="editor-header">
        <div>
          <span class="eyebrow">${isEdit ? 'edit agent' : 'new agent'}</span>
          <h3 id="wireNameHeading">${isEdit ? escapeHtml(agentData.name) : 'Untitled agent'}</h3>
        </div>
        <span class="pill ${agentData.read_only ? 'pill-accent' : ''}">${agentData.read_only ? 'Default' : 'Custom'}</span>
      </div>

      ${agentData.read_only ? `
        <div class="notice">
          <span>&#9888;</span>
          <div><strong>Compiled-in default.</strong> Saving edits will create a custom override in your home directory rather than modifying the built-in agent.</div>
        </div>
      ` : ''}

      <div class="wiring-strip" id="wiringStrip">
        <div class="wire-node">
          <span class="wire-dot"></span>
          <span class="wire-name-label" id="wireNameLabel">${isEdit ? escapeHtml(agentData.name) : 'unnamed'}</span>
        </div>
        <div class="wire-trace"></div>
        <div class="wire-node">
          <span class="wire-count" id="toolCount">${toolCount}</span>
          <span class="wire-label">tools wired</span>
        </div>
        <div class="wire-trace"></div>
        <div class="wire-node">
          <span class="wire-count" id="subCount">${subCount}</span>
          <span class="wire-label">delegations</span>
        </div>
      </div>

      <div class="field-row">
        <div class="field">
          <label for="agentName">Agent name (unique ID)</label>
          <input type="text" id="agentName" value="${escapeAttr(agentData.name)}" placeholder="e.g. general_assistant" ${isEdit ? 'disabled' : ''} oninput="updateWireName()">
        </div>
        <div class="field">
          <label for="agentDesc">Description</label>
          <input type="text" id="agentDesc" value="${escapeAttr(agentData.description)}" placeholder="e.g. A general helper agent">
        </div>
      </div>

      <div class="switch-row">
        <label class="switch">
          <input type="checkbox" id="agentIsRoot" ${agentData.is_root ? 'checked' : ''}>
          Is root agent
        </label>
        <label class="switch">
          <input type="checkbox" id="agentIsPrivate" ${agentData.private ? 'checked' : ''}>
          Private (hide from selector)
        </label>
      </div>

      ${groupsHTML}

      <div class="field" style="margin-bottom: 0;">
        <label for="agentInstructions">Instructions (system prompt — Markdown)</label>
        <textarea id="agentInstructions" placeholder="You are a polite assistant...">${agentData.instructions || ''}</textarea>
      </div>

      <div class="editor-actions">
        <button class="btn" onclick="openNewAgentForm()">Cancel</button>
        <button class="btn btn-primary" onclick="saveAgent()">Save agent</button>
      </div>
    </div>
  `;

  document.querySelectorAll('input[name="agent_tools"]').forEach(cb => {
    cb.addEventListener('change', updateWiringCounts);
  });
}

function updateWireName() {
  const name = document.getElementById('agentName').value.trim();
  document.getElementById('wireNameLabel').textContent = name || 'unnamed';
  document.getElementById('wireNameHeading').textContent = name || 'Untitled agent';
}

function updateWiringCounts() {
  const toolBoxes = document.querySelectorAll('input[name="agent_tools"][data-kind="tool"]:checked');
  const subBoxes = document.querySelectorAll('input[name="agent_tools"][data-kind="sub"]:checked');
  document.getElementById('toolCount').textContent = toolBoxes.length;
  document.getElementById('subCount').textContent = subBoxes.length;
}

async function saveAgent() {
  const nameEl = document.getElementById('agentName');
  const descEl = document.getElementById('agentDesc');
  const isRootEl = document.getElementById('agentIsRoot');
  const isPrivateEl = document.getElementById('agentIsPrivate');
  const instEl = document.getElementById('agentInstructions');

  const selectedTools = [];
  document.querySelectorAll('input[name="agent_tools"]:checked').forEach(cb => {
    selectedTools.push(cb.value);
  });

  const payload = {
    name: nameEl.value,
    description: descEl.value,
    is_root: isRootEl.checked,
    private: isPrivateEl.checked,
    tools: selectedTools,
    instructions: instEl.value
  };

  try {
    const res = await fetch('/api/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });

    if (!res.ok) {
      const errMsg = await res.text();
      throw new Error(errMsg || 'Failed to save agent');
    }

    showToast('Agent saved', 'success');

    await loadTools();
    await loadAgents();

    const matched = allAgents.find(a => a.name === payload.name);
    if (matched) selectAgent(matched);

  } catch (err) {
    showToast(err.message, 'error');
  }
}

async function deleteAgent(event, name) {
  event.stopPropagation();

  if (!confirm(`Delete "${name}"? This cannot be undone.`)) {
    return;
  }

  try {
    const res = await fetch(`/api/agents/${name}`, {
      method: 'DELETE'
    });

    if (!res.ok) {
      const errMsg = await res.text();
      throw new Error(errMsg || 'Failed to delete agent');
    }

    showToast('Agent deleted', 'success');

    if (currentAgent && currentAgent.name === name) {
      currentAgent = null;
      document.getElementById('workspace').innerHTML = `
        <div class="welcome">
          <svg class="welcome-diagram" viewBox="0 0 320 120" width="320" height="120" role="presentation" aria-hidden="true">
            <line x1="40" y1="60" x2="140" y2="30" class="w-trace"></line>
            <line x1="40" y1="60" x2="140" y2="90" class="w-trace"></line>
            <line x1="140" y1="30" x2="260" y2="30" class="w-trace"></line>
            <line x1="140" y1="90" x2="260" y2="60" class="w-trace"></line>
            <line x1="140" y1="90" x2="260" y2="100" class="w-trace"></line>
            <circle cx="40" cy="60" r="7" class="w-node w-node-main"></circle>
            <circle cx="140" cy="30" r="5" class="w-node"></circle>
            <circle cx="140" cy="90" r="5" class="w-node"></circle>
            <circle cx="260" cy="30" r="4" class="w-node w-node-dim"></circle>
            <circle cx="260" cy="60" r="4" class="w-node w-node-dim"></circle>
            <circle cx="260" cy="100" r="4" class="w-node w-node-dim"></circle>
          </svg>
          <h2>Nothing selected yet</h2>
          <p>Pick an agent from the registry, or create one to start wiring up its prompt, tools, and sub-agent delegation. Changes save straight to your local <code>~/.botsonv2/agents/</code> directory.</p>
        </div>
      `;
    }

    await loadTools();
    await loadAgents();

  } catch (err) {
    showToast(err.message, 'error');
  }
}

function showToast(message, type = 'success') {
  const container = document.getElementById('toastContainer');
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.innerText = message;
  container.appendChild(toast);

  setTimeout(() => {
    toast.remove();
  }, 3000);
}

function escapeHtml(str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

function escapeAttr(str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}
