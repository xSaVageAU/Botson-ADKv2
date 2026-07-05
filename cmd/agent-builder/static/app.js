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
  listEl.innerHTML = '';

  allAgents.forEach(agent => {
    const li = document.createElement('li');
    li.className = `agent-item ${currentAgent && currentAgent.name === agent.name ? 'active' : ''}`;
    li.onclick = () => selectAgent(agent);

    li.innerHTML = `
      <div class="agent-info">
        <div class="agent-name">${agent.name}</div>
        <div class="agent-desc-short">${agent.description || 'No description'}</div>
      </div>
      <div style="display: flex; align-items: center; gap: 8px;">
        <span class="badge ${agent.read_only ? 'read-only' : ''}">${agent.read_only ? 'Default' : 'Custom'}</span>
        ${!agent.read_only ? `
          <button class="btn-delete" onclick="deleteAgent(event, '${agent.name}')">Delete</button>
        ` : ''}
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

  // Create checkbox lists for tools
  // Do not list yourself as a delegatable subagent
  const filteredSubagents = allTools.agents.filter(a => a !== agentData.name);

  let toolsHTML = '';
  
  // Standard Tools Section
  toolsHTML += '<div class="field-group" style="grid-column: 1 / -1;"><label>Standard Tools</label><div class="tools-grid">';
  allTools.standard.forEach(tool => {
    const checked = agentData.tools && agentData.tools.includes(tool) ? 'checked' : '';
    toolsHTML += `
      <label class="tool-checkbox">
        <input type="checkbox" name="agent_tools" value="${tool}" ${checked}>
        ${tool}
      </label>
    `;
  });
  toolsHTML += '</div></div>';

  // Sub-Agents delegation Section
  if (filteredSubagents.length > 0) {
    toolsHTML += '<div class="field-group" style="grid-column: 1 / -1;"><label>Sub-Agent Delegation</label><div class="tools-grid">';
    filteredSubagents.forEach(sub => {
      const checked = agentData.tools && agentData.tools.includes(sub) ? 'checked' : '';
      toolsHTML += `
        <label class="tool-checkbox">
          <input type="checkbox" name="agent_tools" value="${sub}" ${checked}>
          ${sub}
        </label>
      `;
    });
    toolsHTML += '</div></div>';
  }

  workspace.innerHTML = `
    <div class="editor-container">
      <div class="editor-header">
        <h3 class="editor-title">${isEdit ? `Edit Agent: ${agentData.name}` : 'Create New Agent'}</h3>
        <span class="badge ${agentData.read_only ? 'read-only' : ''}">${agentData.read_only ? 'Default Agent' : 'Custom Agent'}</span>
      </div>

      ${agentData.read_only ? `
        <div class="read-only-alert">
          <strong>Notice:</strong> This is a compiled-in default agent. Saving edits will create a custom override in your home directory, keeping the original default intact.
        </div>
      ` : ''}

      <div class="field-row">
        <div class="field-group">
          <label for="agentName">Agent Name (Unique ID)</label>
          <input type="text" id="agentName" value="${agentData.name}" placeholder="e.g. general_assistant" ${isEdit ? 'disabled' : ''}>
        </div>
        <div class="field-group">
          <label for="agentDesc">Description</label>
          <input type="text" id="agentDesc" value="${agentData.description}" placeholder="e.g. A general helper agent">
        </div>
      </div>

      <div class="toggle-row">
        <label class="toggle-item">
          <input type="checkbox" id="agentIsRoot" ${agentData.is_root ? 'checked' : ''}>
          <span>Is Root Agent (Default Launch)</span>
        </label>
        <label class="toggle-item">
          <input type="checkbox" id="agentIsPrivate" ${agentData.private ? 'checked' : ''}>
          <span>Private (Hide from Web UI selector)</span>
        </label>
      </div>

      ${toolsHTML}

      <div class="field-group">
        <label for="agentInstructions">Instructions (System Prompt - Markdown)</label>
        <textarea id="agentInstructions" placeholder="You are a polite assistant...">${agentData.instructions || ''}</textarea>
      </div>

      <div class="form-actions">
        <button class="btn btn-secondary" onclick="openNewAgentForm()">Cancel</button>
        <button class="btn btn-save" onclick="saveAgent()">Save Agent</button>
      </div>
    </div>
  `;
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

    showToast('Agent saved successfully!', 'success');
    
    // Reload tools list (since a new agent can now be a subagent tool)
    await loadTools();
    await loadAgents();

    // Select the newly saved agent
    const matched = allAgents.find(a => a.name === payload.name);
    if (matched) selectAgent(matched);

  } catch (err) {
    showToast(err.message, 'error');
  }
}

async function deleteAgent(event, name) {
  event.stopPropagation(); // Avoid triggering list select

  if (!confirm(`Are you sure you want to delete "${name}"? This cannot be undone.`)) {
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

    showToast('Agent deleted successfully', 'success');
    
    if (currentAgent && currentAgent.name === name) {
      currentAgent = null;
      document.getElementById('workspace').innerHTML = `
        <div class="welcome-container">
          <div class="welcome-card">
            <h2>Agent Builder Dashboard</h2>
            <p>Create and orchestrate agents. Add custom prompts, link tools, and wire up sub-agent delegation. Saving changes automatically updates your local <code>~/.botsonv2/agents/</code> directory.</p>
          </div>
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
